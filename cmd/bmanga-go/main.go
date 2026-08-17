package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/zzzcws/bmanga-core/internal/prototype"
)

const defaultLoginTarget = "/v2/"
const loginFailureLimit = 10
const defaultLoginAccountFailureLimit = 30
const defaultHTTPShutdownTimeout = 10 * time.Second

var loginFailureWindow = 10 * time.Minute
var loginFailureBlockDuration = 15 * time.Minute

func main() {
	host := flag.String("host", "127.0.0.1", "HTTP bind host")
	port := flag.Int("port", 8765, "HTTP bind port")
	dbPath := flag.String("db", filepath.Join("data", "bmanga-prototype.sqlite"), "SQLite database path")
	authUser := flag.String("auth-user", os.Getenv("BMANGA_AUTH_USER"), "auth user")
	authPassword := flag.String("auth-password", authPasswordFromRuntimeConfig(), "auth password")
	allowWildcardBind := flag.Bool("allow-wildcard-bind", false, "allow wildcard bind hosts such as 0.0.0.0 or ::")
	allowPublicBind := flag.Bool("allow-public-bind", false, "allow publicly routable bind hosts")
	flag.Parse()

	if *authPassword != "" && strings.TrimSpace(*authUser) == "" {
		*authUser = "bmanga"
	}
	if !isLoopbackBindHost(*host) && *authPassword == "" {
		log.Fatalf("refusing non-loopback bind host without --auth-password: use 127.0.0.1 or enable Basic Auth first")
	}
	if isWildcardBindHost(*host) && !*allowWildcardBind {
		log.Fatalf("refusing wildcard bind host without --allow-wildcard-bind: prefer 127.0.0.1, Chrome Remote Desktop, or a specific private LAN IP")
	}
	publicBind := !isLoopbackBindHost(*host) && !isWildcardBindHost(*host) && !isPrivateOrLocalBindHost(*host)
	if publicBind && !*allowPublicBind {
		log.Fatalf("refusing public bind host without --allow-public-bind: prefer 127.0.0.1, Chrome Remote Desktop, or a private LAN IP")
	}
	trustedProxies, err := trustedProxyPolicyFromEnv()
	if err != nil {
		log.Fatalf("invalid BMANGA_TRUSTED_PROXY_CIDRS: %v", err)
	}

	server, err := prototype.NewServer(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	server.SetAuthEnabled(*authPassword != "")
	defer server.Close()

	addr := net.JoinHostPort(*host, fmt.Sprintf("%d", *port))
	handler := server.Routes()
	if *authPassword != "" {
		sessions, err := newPersistentSessionStore(sessionStorePath(*dbPath))
		if err != nil {
			log.Fatalf("initialize persistent auth sessions: %v", err)
		}
		handler = sessionAuthWithStoreAndPolicy(handler, *authUser, *authPassword, sessions, trustedProxies)
	}
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: envDurationOrDefault("BMANGA_HTTP_READ_HEADER_TIMEOUT", 10*time.Second),
		ReadTimeout:       envDurationOrDefault("BMANGA_HTTP_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:      envDurationOrDefault("BMANGA_HTTP_WRITE_TIMEOUT", 5*time.Minute),
		IdleTimeout:       envDurationOrDefault("BMANGA_HTTP_IDLE_TIMEOUT", 2*time.Minute),
		MaxHeaderBytes:    1 << 20,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", addr, err)
	}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("bmanga Go prototype API: http://%s/", addr)
	if err := serveHTTP(shutdownContext, httpServer, listener, defaultHTTPShutdownTimeout); err != nil {
		log.Fatal(err)
	}
}

func serveHTTP(ctx context.Context, httpServer *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
		return fmt.Errorf("graceful HTTP shutdown: %w", err)
	}
	if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func authPasswordFromRuntimeConfig() string {
	if password, ok := readAuthPasswordFile(strings.TrimSpace(os.Getenv("BMANGA_AUTH_PASSWORD_FILE"))); ok {
		return password
	}
	return os.Getenv("BMANGA_AUTH_PASSWORD")
}

func readAuthPasswordFile(path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		return "", false
	}
	return password, true
}

func envDurationOrDefault(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envIntInRangeOrDefault(name string, fallback int, minimum int, maximum int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return fallback
	}
	return parsed
}

func isLoopbackBindHost(value string) bool {
	host := strings.Trim(strings.TrimSpace(value), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isWildcardBindHost(value string) bool {
	host := strings.Trim(strings.TrimSpace(value), "[]")
	if host == "0.0.0.0" || host == "::" || host == "*" || host == "+" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func isPrivateOrLocalBindHost(value string) bool {
	host := strings.Trim(strings.TrimSpace(value), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func sessionAuth(next http.Handler, user string, password string) http.Handler {
	return sessionAuthWithStore(next, user, password, newSessionStore())
}

func sessionAuthWithStore(next http.Handler, user string, password string, sessions *sessionStore) http.Handler {
	trustedProxies, err := trustedProxyPolicyFromEnv()
	if err != nil {
		log.Printf("ignoring invalid BMANGA_TRUSTED_PROXY_CIDRS: %v", err)
		trustedProxies = trustedProxyPolicy{}
	}
	return sessionAuthWithStoreAndPolicy(next, user, password, sessions, trustedProxies)
}

func sessionAuthWithStoreAndPolicy(next http.Handler, user string, password string, sessions *sessionStore, trustedProxies trustedProxyPolicy) http.Handler {
	user = strings.TrimSpace(user)
	if user == "" {
		user = "bmanga"
	}
	if sessions == nil {
		sessions = newSessionStore()
	}
	secret := os.Getenv("BMANGA_SESSION_SECRET")
	if strings.TrimSpace(secret) == "" {
		secret = user + "\n" + password
	}
	loginLimiter := newLoginAttemptLimiter(trustedProxies)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login":
			handleLogin(w, r, user, password, secret, sessions, loginLimiter, trustedProxies)
			return
		case r.URL.Path == "/logout":
			if err := sessions.revokeRequest(r); err != nil {
				log.Printf("persist auth session revocation: %v", err)
				if wantsJSON(r) {
					writeJSONLoginError(w, http.StatusServiceUnavailable, "暂时无法安全退出，请稍后重试。")
					return
				}
				http.Error(w, "暂时无法安全退出，请稍后重试。", http.StatusServiceUnavailable)
				return
			}
			clearSessionCookie(w, r, trustedProxies)
			redirectToLogin(w, r)
			return
		case validSessionCookie(r, secret, sessions):
			next.ServeHTTP(w, r)
			return
		case hasBasicAuth(r):
			if validBasicAuth(r, user, password) {
				loginLimiter.reset(r)
				next.ServeHTTP(w, r)
				return
			}
			if blocked, retryAfter := loginLimiter.blocked(r); blocked {
				writeLoginRateLimited(w, r, user, retryAfter)
				return
			}
			loginLimiter.recordFailure(r)
			if wantsJSON(r) {
				writeJSONUnauthorized(w)
				return
			}
			redirectToLogin(w, r)
			return
		default:
			if wantsJSON(r) {
				writeJSONUnauthorized(w)
				return
			}
			redirectToLogin(w, r)
			return
		}
	})
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	target := loginTargetForRequest(r)
	location := "/login"
	if target != "/" {
		location += "?next=" + url.QueryEscape(target)
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func loginTargetForRequest(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}
	path := strings.TrimSpace(r.URL.Path)
	lowerPath := strings.ToLower(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || lowerPath == "/login" || strings.HasPrefix(lowerPath, "/login/") || lowerPath == "/logout" {
		return "/"
	}
	target := r.URL.RequestURI()
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "/"
	}
	return target
}

func validBasicAuth(r *http.Request, user string, password string) bool {
	requestUser, requestPassword, ok := r.BasicAuth()
	if !ok {
		return false
	}
	userOK := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(requestUser)), []byte(user)) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(normalizeLoginPassword(requestPassword)), []byte(password)) == 1
	return userOK && passwordOK
}

func hasBasicAuth(r *http.Request) bool {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	return strings.HasPrefix(strings.ToLower(value), "basic ")
}

func handleLogin(w http.ResponseWriter, r *http.Request, user string, password string, secret string, sessions *sessionStore, loginLimiter *loginAttemptLimiter, trustedProxies trustedProxyPolicy) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeLoginPage(w, r, user, "")
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if blocked, retryAfter := loginLimiter.blocked(r); blocked {
			writeLoginRateLimited(w, r, user, retryAfter)
			return
		}
		if err := parseLoginForm(r); err != nil {
			if wantsJSON(r) {
				writeJSONLoginError(w, http.StatusBadRequest, "登录请求格式不对，请刷新后再试。")
				return
			}
			writeLoginPageStatus(w, r, user, "登录请求格式不对，请刷新后再试。", http.StatusBadRequest)
			return
		}
		requestUser := strings.TrimSpace(r.FormValue("username"))
		requestPassword := normalizeLoginPassword(r.FormValue("password"))
		userOK := subtle.ConstantTimeCompare([]byte(requestUser), []byte(user)) == 1
		passwordOK := subtle.ConstantTimeCompare([]byte(requestPassword), []byte(password)) == 1
		if !userOK || !passwordOK {
			loginLimiter.recordFailure(r)
			if wantsJSON(r) {
				writeJSONLoginError(w, http.StatusUnauthorized, "账号或密码不对。")
				return
			}
			writeLoginPageStatus(w, r, user, "账号或密码不对。", http.StatusUnauthorized)
			return
		}
		loginLimiter.reset(r)
		if err := setSessionCookie(w, r, secret, sessions, trustedProxies); err != nil {
			log.Printf("persist auth session: %v", err)
			if wantsJSON(r) {
				writeJSONLoginError(w, http.StatusServiceUnavailable, "暂时无法保存登录状态，请稍后重试。")
				return
			}
			writeLoginPageStatus(w, r, user, "暂时无法保存登录状态，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		next := normalizedLoginTarget(r.FormValue("next"))
		if wantsJSON(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, `{"ok":true,"next":`+strconv.Quote(next)+`}`)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, next, http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeLoginRateLimited(w http.ResponseWriter, r *http.Request, user string, retryAfter time.Duration) {
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second)/time.Second)))
	if wantsJSON(r) {
		writeJSONLoginError(w, http.StatusTooManyRequests, "登录尝试太频繁，请稍后再试。")
		return
	}
	writeLoginPageStatus(w, r, user, "登录尝试太频繁，请稍后再试。", http.StatusTooManyRequests)
}

type loginAttemptLimiter struct {
	mu             sync.Mutex
	entries        map[string]loginAttemptEntry
	account        loginAttemptEntry
	accountLimit   int
	trustedProxies trustedProxyPolicy
}

type loginAttemptEntry struct {
	firstFailure time.Time
	failures     int
	blockedUntil time.Time
	lastSeen     time.Time
}

func newLoginAttemptLimiter(trustedProxies trustedProxyPolicy) *loginAttemptLimiter {
	return &loginAttemptLimiter{
		entries:        map[string]loginAttemptEntry{},
		accountLimit:   envIntInRangeOrDefault("BMANGA_LOGIN_ACCOUNT_FAILURE_LIMIT", defaultLoginAccountFailureLimit, loginFailureLimit, 1000),
		trustedProxies: trustedProxies,
	}
}

func (l *loginAttemptLimiter) blocked(r *http.Request) (bool, time.Duration) {
	if l == nil {
		return false, 0
	}
	key := loginAttemptKey(r, l.trustedProxies)
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	blockedUntil := time.Time{}
	if entry, ok := l.entries[key]; ok && now.Before(entry.blockedUntil) {
		entry.lastSeen = now
		l.entries[key] = entry
		blockedUntil = entry.blockedUntil
	}
	if now.Before(l.account.blockedUntil) && l.account.blockedUntil.After(blockedUntil) {
		l.account.lastSeen = now
		blockedUntil = l.account.blockedUntil
	}
	if blockedUntil.IsZero() {
		return false, 0
	}
	return true, time.Until(blockedUntil)
}

func (l *loginAttemptLimiter) recordFailure(r *http.Request) {
	if l == nil {
		return
	}
	key := loginAttemptKey(r, l.trustedProxies)
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	l.entries[key] = recordLoginFailure(l.entries[key], now, loginFailureLimit)
	l.account = recordLoginFailure(l.account, now, l.accountLimit)
}

func (l *loginAttemptLimiter) reset(r *http.Request) {
	if l == nil {
		return
	}
	key := loginAttemptKey(r, l.trustedProxies)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
	l.account = loginAttemptEntry{}
}

func (l *loginAttemptLimiter) pruneLocked(now time.Time) {
	for key, entry := range l.entries {
		switch {
		case !entry.blockedUntil.IsZero() && now.After(entry.blockedUntil.Add(loginFailureWindow)):
			delete(l.entries, key)
		case entry.blockedUntil.IsZero() && now.Sub(entry.lastSeen) > loginFailureWindow:
			delete(l.entries, key)
		}
	}
	if loginAttemptEntryExpired(l.account, now) {
		l.account = loginAttemptEntry{}
	}
}

func recordLoginFailure(entry loginAttemptEntry, now time.Time, limit int) loginAttemptEntry {
	if entry.firstFailure.IsZero() || now.Sub(entry.firstFailure) > loginFailureWindow {
		entry = loginAttemptEntry{firstFailure: now}
	}
	entry.failures++
	entry.lastSeen = now
	if entry.failures >= limit {
		entry.blockedUntil = now.Add(loginFailureBlockDuration)
	}
	return entry
}

func loginAttemptEntryExpired(entry loginAttemptEntry, now time.Time) bool {
	if entry.firstFailure.IsZero() {
		return false
	}
	if !entry.blockedUntil.IsZero() {
		return now.After(entry.blockedUntil.Add(loginFailureWindow))
	}
	return now.Sub(entry.lastSeen) > loginFailureWindow
}

type trustedProxyPolicy struct {
	networks []*net.IPNet
}

func trustedProxyPolicyFromEnv() (trustedProxyPolicy, error) {
	return parseTrustedProxyCIDRs(os.Getenv("BMANGA_TRUSTED_PROXY_CIDRS"))
}

func parseTrustedProxyCIDRs(raw string) (trustedProxyPolicy, error) {
	policy := trustedProxyPolicy{}
	seen := map[string]bool{}
	for _, value := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	}) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip = ip.To4()
				bits = 32
			}
			value = (&net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}).String()
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return trustedProxyPolicy{}, fmt.Errorf("invalid CIDR %q", value)
		}
		if ipv4 := network.IP.To4(); ipv4 != nil {
			network.IP = ipv4
		}
		key := network.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		policy.networks = append(policy.networks, network)
	}
	return policy, nil
}

func (p trustedProxyPolicy) contains(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, network := range p.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func requestRemoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	remoteHost, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		remoteHost = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	return net.ParseIP(remoteHost)
}

func (p trustedProxyPolicy) trustsRequest(r *http.Request) bool {
	return p.contains(requestRemoteIP(r))
}

func loginAttemptKey(r *http.Request, trustedProxies trustedProxyPolicy) string {
	if host := trustedProxies.forwardedClientIP(r); host != "" {
		return host
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
}

func (p trustedProxyPolicy) forwardedClientIP(r *http.Request) string {
	if !p.trustsRequest(r) {
		return ""
	}
	forwardedFor := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	parts := strings.Split(forwardedFor, ",")
	for index := len(parts) - 1; index >= 0; index-- {
		if ip := parseForwardedIP(parts[index]); ip != nil && !p.contains(ip) {
			return ip.String()
		}
	}
	if ip := parseForwardedIP(r.Header.Get("X-Real-IP")); ip != nil && !p.contains(ip) {
		return ip.String()
	}
	return ""
}

func parseForwardedIP(value string) net.IP {
	host := strings.TrimSpace(value)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	return net.ParseIP(host)
}

func (p trustedProxyPolicy) forwardedHTTPS(r *http.Request) bool {
	if !p.trustsRequest(r) {
		return false
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
	if len(parts) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "https")
}

func parseLoginForm(r *http.Request) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		return r.ParseMultipartForm(1 << 20)
	}
	return r.ParseForm()
}

func normalizeLoginPassword(value string) string {
	var builder strings.Builder
	changed := false
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			changed = true
			continue
		}
		builder.WriteRune(r)
	}
	if !changed {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(builder.String())
}

func writeJSONLoginError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
}

func wantsJSON(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") ||
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") ||
		strings.EqualFold(r.Header.Get("X-Requested-With"), "XMLHttpRequest")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, secret string, sessions *sessionStore, trustedProxies trustedProxyPolicy) error {
	expires := time.Unix(time.Now().Add(30*24*time.Hour).Unix(), 0)
	nonce := randomSessionNonce()
	if err := sessions.add(nonce, expires); err != nil {
		return err
	}
	secure := cookieSecure(r, trustedProxies)
	http.SetCookie(w, &http.Cookie{
		Name:     "bmanga_session",
		Value:    signedSessionValue(secret, expires, nonce),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "bmanga_write_token",
		Value:    randomSessionNonce(),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request, trustedProxies trustedProxyPolicy) {
	secure := cookieSecure(r, trustedProxies)
	http.SetCookie(w, &http.Cookie{
		Name:     "bmanga_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "bmanga_write_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func validSessionCookie(r *http.Request, secret string, sessions *sessionStore) bool {
	cookie, err := r.Cookie("bmanga_session")
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 {
		return false
	}
	expiresUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > expiresUnix {
		return false
	}
	nonce := parts[1]
	expected := sessionSignature(secret, parts[0], nonce)
	return subtle.ConstantTimeCompare([]byte(parts[2]), []byte(expected)) == 1 && sessions.valid(nonce, time.Unix(expiresUnix, 0))
}

func signedSessionValue(secret string, expires time.Time, nonce string) string {
	expiresText := fmt.Sprintf("%d", expires.Unix())
	return expiresText + "." + nonce + "." + sessionSignature(secret, expiresText, nonce)
}

func sessionSignature(secret string, expiresText string, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("bmanga-session\n"))
	_, _ = mac.Write([]byte(expiresText))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(nonce))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomSessionNonce() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func cookieSecure(r *http.Request, trustedProxies trustedProxyPolicy) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BMANGA_COOKIE_SECURE"))) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	}
	return r.TLS != nil || trustedProxies.forwardedHTTPS(r)
}

func writeLoginPage(w http.ResponseWriter, r *http.Request, user string, message string) {
	writeLoginPageStatus(w, r, user, message, http.StatusOK)
}

func writeLoginPageStatus(w http.ResponseWriter, r *http.Request, user string, message string, status int) {
	next := loginPageTarget(r)
	nonce := randomSessionNonce()
	page := loginPageHTMLWithNonce(user, message, next, nonce)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+"'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_, _ = io.WriteString(w, page)
}

func loginPageTarget(r *http.Request) string {
	if r != nil && r.Form != nil {
		if target := strings.TrimSpace(r.Form.Get("next")); target != "" {
			return normalizedLoginTarget(target)
		}
	}
	if r == nil {
		return defaultLoginTarget
	}
	return normalizedLoginTarget(r.URL.Query().Get("next"))
}

func normalizedLoginTarget(value string) string {
	target := strings.TrimSpace(value)
	if target == "" || strings.Contains(target, "\\") || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return defaultLoginTarget
	}
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return defaultLoginTarget
	}
	if strings.Contains(parsed.Path, "\\") || strings.HasPrefix(parsed.Path, "//") {
		return defaultLoginTarget
	}
	lowerPath := strings.ToLower(parsed.Path)
	if strings.HasPrefix(lowerPath, "/login") || lowerPath == "/logout" {
		return defaultLoginTarget
	}
	return target
}

func loginPageHTML(user string, message string, next string) string {
	return loginPageHTMLWithNonce(user, message, next, "bmanga-test-nonce")
}

func writeJSONUnauthorized(w http.ResponseWriter) {
	body := []byte(`{"error":"authentication required"}`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(body)
}
