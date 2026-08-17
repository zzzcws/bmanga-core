//go:build unix

package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const sigtermProcessTestTimeout = 10 * time.Second

type sigtermTestProcess struct {
	cmd      *exec.Cmd
	addr     string
	wait     <-chan error
	logFile  *os.File
	logPath  string
	finished bool
}

type pendingHTTPExchange struct {
	conn    net.Conn
	reader  *bufio.Reader
	request *http.Request
}

func TestProcessSIGTERMDataIntegrity(t *testing.T) {
	binary := buildSIGTERMTestBinary(t)

	t.Run("SQLite write drains before exit", func(t *testing.T) {
		testSQLiteWriteSurvivesSIGTERM(t, binary)
	})
	t.Run("session write drains before exit", func(t *testing.T) {
		testSessionWriteSurvivesSIGTERM(t, binary)
	})
}

func testSQLiteWriteSurvivesSIGTERM(t *testing.T, binary string) {
	t.Helper()
	testDir := t.TempDir()
	dbPath := filepath.Join(testDir, "bmanga.sqlite")
	server := startSIGTERMTestProcess(t, binary, dbPath, nil)
	server.waitUntilReady(t, "", "")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := db.Conn(context.Background())
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	locked := false
	defer func() {
		if locked {
			_, _ = holder.ExecContext(context.Background(), "ROLLBACK")
		}
		_ = holder.Close()
		_ = db.Close()
	}()
	if _, err := holder.ExecContext(context.Background(), "PRAGMA busy_timeout=5000"); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("hold SQLite write transaction: %v", err)
	}
	locked = true

	body := []byte(`{"state":{"bmangaView":"works","bmangaSearch":"sigterm-write","updated_at":"2026-08-17T00:00:00Z"}}`)
	exchange := beginExpectContinueExchange(t, server.addr, http.MethodPost, "/api/browse-state", len(body), map[string]string{
		"Content-Type":   "application/json",
		"X-Bmanga-Write": "same-origin",
	})
	defer exchange.close()
	exchange.sendBody(t, body)
	exchange.requirePending(t, 150*time.Millisecond)

	server.signalSIGTERM(t)
	waitForListenerClosure(t, server.addr)
	if _, err := holder.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatalf("release SQLite write transaction: %v", err)
	}
	locked = false
	_ = holder.Close()
	_ = db.Close()

	response, responseBody := exchange.readFinalResponse(t)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("browse-state status after SIGTERM = %d, want 200; body=%s", response.StatusCode, responseBody)
	}
	server.waitForCleanExit(t)

	restarted := startSIGTERMTestProcess(t, binary, dbPath, nil)
	restarted.waitUntilReady(t, "", "")
	assertBrowseStateSurvivedRestart(t, restarted.addr)
	restarted.signalSIGTERM(t)
	restarted.waitForCleanExit(t)
	assertSQLiteIntegrity(t, dbPath)
}

func testSessionWriteSurvivesSIGTERM(t *testing.T, binary string) {
	t.Helper()
	testDir := t.TempDir()
	dbPath := filepath.Join(testDir, "bmanga.sqlite")
	sessionPath := filepath.Join(testDir, sessionStoreFileName)
	const authUser = "bmanga"
	authValue := strings.Join([]string{"fixture", "login", "value"}, "-")
	signingValue := strings.Join([]string{"fixture", "session", "signing", "value"}, "-")
	environment := []string{
		"BMANGA_AUTH_USER=" + authUser,
		"BMANGA_AUTH_PASSWORD=" + authValue,
		"BMANGA_SESSION_SECRET=" + signingValue,
		"BMANGA_SESSION_STORE_FILE=" + sessionPath,
		"BMANGA_COOKIE_SECURE=0",
	}
	server := startSIGTERMTestProcess(t, binary, dbPath, environment)
	server.waitUntilReady(t, authUser, authValue)

	form := url.Values{"username": {authUser}, "password": {authValue}, "next": {"/v2/"}}
	body := []byte(form.Encode())
	exchange := beginExpectContinueExchange(t, server.addr, http.MethodPost, "/login", len(body), map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/x-www-form-urlencoded",
	})
	defer exchange.close()

	server.signalSIGTERM(t)
	waitForListenerClosure(t, server.addr)
	exchange.sendBody(t, body)
	response, responseBody := exchange.readFinalResponse(t)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status after SIGTERM = %d, want 200; body=%s", response.StatusCode, responseBody)
	}
	sessionCookie := responseCookie(response, "bmanga_session")
	if sessionCookie == nil {
		t.Fatal("login completed during shutdown without a session cookie")
	}
	server.waitForCleanExit(t)
	assertPersistedSession(t, sessionPath, sessionCookie)

	restarted := startSIGTERMTestProcess(t, binary, dbPath, environment)
	restarted.waitUntilReady(t, authUser, authValue)
	request, err := http.NewRequest(http.MethodGet, "http://"+restarted.addr+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(sessionCookie)
	responseAfterRestart, err := sigtermHTTPClient().Do(request)
	if err != nil {
		t.Fatalf("use persisted session after restart: %v", err)
	}
	_, _ = io.Copy(io.Discard, responseAfterRestart.Body)
	_ = responseAfterRestart.Body.Close()
	if responseAfterRestart.StatusCode != http.StatusOK {
		t.Fatalf("persisted session status after restart = %d, want 200", responseAfterRestart.StatusCode)
	}
	restarted.signalSIGTERM(t)
	restarted.waitForCleanExit(t)
	assertPersistedSession(t, sessionPath, sessionCookie)
	assertSQLiteIntegrity(t, dbPath)
}

func buildSIGTERMTestBinary(t *testing.T) string {
	t.Helper()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(workingDir, "..", ".."))
	binary := filepath.Join(t.TempDir(), "bmanga-sigterm-test")
	command := exec.Command("go", "build", "-buildvcs=false", "-mod=readonly", "-o", binary, "./cmd/bmanga-go")
	command.Dir = repoRoot
	command.Env = append(cleanSIGTERMTestEnvironment(), "GOWORK=off", "GOFLAGS=")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build bmanga process test binary: %v\n%s", err, output)
	}
	return binary
}

func startSIGTERMTestProcess(t *testing.T, binary string, dbPath string, extraEnvironment []string) *sigtermTestProcess {
	t.Helper()
	addr := reserveLoopbackAddress(t)
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-host", host, "-port", strconv.Itoa(port), "-db", dbPath)
	command.Env = append(cleanSIGTERMTestEnvironment(), extraEnvironment...)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start bmanga process: %v", err)
	}
	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()
	process := &sigtermTestProcess{
		cmd:     command,
		addr:    addr,
		wait:    wait,
		logFile: logFile,
		logPath: logPath,
	}
	t.Cleanup(func() {
		if !process.finished {
			_ = process.cmd.Process.Kill()
			select {
			case <-process.wait:
			case <-time.After(sigtermProcessTestTimeout):
			}
			process.finished = true
		}
		_ = process.logFile.Close()
	})
	return process
}

func (process *sigtermTestProcess) waitUntilReady(t *testing.T, user string, password string) {
	t.Helper()
	deadline := time.Now().Add(sigtermProcessTestTimeout)
	client := sigtermHTTPClient()
	defer client.CloseIdleConnections()
	for time.Now().Before(deadline) {
		select {
		case err := <-process.wait:
			process.finished = true
			t.Fatalf("bmanga exited before readiness: %v\n%s", err, process.logs())
		default:
		}
		request, err := http.NewRequest(http.MethodGet, "http://"+process.addr+"/api/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		if password != "" {
			request.SetBasicAuth(user, password)
		}
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("bmanga did not become ready at %s\n%s", process.addr, process.logs())
}

func (process *sigtermTestProcess) signalSIGTERM(t *testing.T) {
	t.Helper()
	if process.finished {
		t.Fatal("cannot signal an exited bmanga process")
	}
	if err := process.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v\n%s", err, process.logs())
	}
}

func (process *sigtermTestProcess) waitForCleanExit(t *testing.T) {
	t.Helper()
	if process.finished {
		t.Fatal("bmanga process was already reaped")
	}
	select {
	case err := <-process.wait:
		process.finished = true
		if err != nil {
			t.Fatalf("bmanga did not exit cleanly after SIGTERM: %v\n%s", err, process.logs())
		}
	case <-time.After(sigtermProcessTestTimeout + 2*time.Second):
		t.Fatalf("bmanga did not exit after SIGTERM\n%s", process.logs())
	}
}

func (process *sigtermTestProcess) logs() string {
	_ = process.logFile.Sync()
	body, err := os.ReadFile(process.logPath)
	if err != nil {
		return "read process log: " + err.Error()
	}
	return string(body)
}

func reserveLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func beginExpectContinueExchange(t *testing.T, addr string, method string, path string, contentLength int, headers map[string]string) *pendingHTTPExchange {
	t.Helper()
	connection, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("open pending request: %v", err)
	}
	request := &http.Request{Method: method}
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(connection, "%s %s HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\nExpect: 100-continue\r\nConnection: close\r\n", method, path, addr, contentLength); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	for name, value := range headers {
		if _, err := fmt.Fprintf(connection, "%s: %s\r\n", name, value); err != nil {
			_ = connection.Close()
			t.Fatal(err)
		}
	}
	if _, err := io.WriteString(connection, "\r\n"); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	interim, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = connection.Close()
		t.Fatalf("read 100-continue response: %v", err)
	}
	_ = interim.Body.Close()
	if interim.StatusCode != http.StatusContinue {
		_ = connection.Close()
		t.Fatalf("interim status = %d, want 100", interim.StatusCode)
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	return &pendingHTTPExchange{conn: connection, reader: reader, request: request}
}

func (exchange *pendingHTTPExchange) sendBody(t *testing.T, body []byte) {
	t.Helper()
	if err := exchange.conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.conn.Write(body); err != nil {
		t.Fatalf("write pending request body: %v", err)
	}
	if err := exchange.conn.SetWriteDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func (exchange *pendingHTTPExchange) requirePending(t *testing.T, duration time.Duration) {
	t.Helper()
	if err := exchange.conn.SetReadDeadline(time.Now().Add(duration)); err != nil {
		t.Fatal(err)
	}
	_, err := exchange.reader.Peek(1)
	if err == nil {
		t.Fatal("write request completed before SIGTERM was sent")
	}
	if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
		t.Fatalf("pending write request ended unexpectedly: %v", err)
	}
	if err := exchange.conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func (exchange *pendingHTTPExchange) readFinalResponse(t *testing.T) (*http.Response, string) {
	t.Helper()
	if err := exchange.conn.SetReadDeadline(time.Now().Add(sigtermProcessTestTimeout)); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(exchange.reader, exchange.request)
	if err != nil {
		t.Fatalf("read final response: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read final response body: %v", err)
	}
	return response, string(body)
}

func (exchange *pendingHTTPExchange) close() {
	_ = exchange.conn.Close()
}

func waitForListenerClosure(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	consecutiveRefusals := 0
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp4", addr, 50*time.Millisecond)
		if err != nil {
			if errors.Is(err, syscall.ECONNREFUSED) {
				consecutiveRefusals++
				if consecutiveRefusals >= 3 {
					return
				}
			} else {
				consecutiveRefusals = 0
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		consecutiveRefusals = 0
		_ = connection.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listener %s remained open after SIGTERM", addr)
}

func assertBrowseStateSurvivedRestart(t *testing.T, addr string) {
	t.Helper()
	response, err := sigtermHTTPClient().Get("http://" + addr + "/api/browse-state")
	if err != nil {
		t.Fatalf("read browse state after restart: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("browse-state status after restart = %d, want 200; body=%s", response.StatusCode, body)
	}
	var payload struct {
		State map[string]any `json:"state"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode browse state after restart: %v", err)
	}
	if payload.State["bmangaSearch"] != "sigterm-write" {
		t.Fatalf("browse state after restart = %#v", payload.State)
	}
}

func assertPersistedSession(t *testing.T, path string, cookie *http.Cookie) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted session: %v", err)
	}
	var persisted persistedSessionStore
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatalf("decode persisted session: %v", err)
	}
	if persisted.Version != sessionStoreFormatVersion {
		t.Fatalf("session store version = %d, want %d", persisted.Version, sessionStoreFormatVersion)
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 {
		t.Fatalf("session cookie has %d parts, want 3", len(parts))
	}
	expiresUnix, ok := persisted.Sessions[sessionStoreKey(parts[1])]
	if !ok || expiresUnix <= time.Now().Unix() {
		t.Fatalf("persisted session is missing or expired: %#v", persisted.Sessions)
	}
	temporaryFiles, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".auth-sessions-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("session write left temporary files: %v", temporaryFiles)
	}
}

func assertSQLiteIntegrity(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, pragma := range []string{"quick_check", "integrity_check"} {
		var result string
		if err := db.QueryRow("PRAGMA " + pragma).Scan(&result); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if result != "ok" {
			t.Fatalf("PRAGMA %s = %q, want ok", pragma, result)
		}
	}
}

func responseCookie(response *http.Response, name string) *http.Cookie {
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func sigtermHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			Proxy:             nil,
			DisableKeepAlives: true,
		},
	}
}

func cleanSIGTERMTestEnvironment() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(strings.ToUpper(name), "BMANGA_") || strings.EqualFold(name, "GOWORK") || strings.EqualFold(name, "GOFLAGS") {
			continue
		}
		environment = append(environment, item)
	}
	return environment
}
