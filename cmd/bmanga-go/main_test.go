package main

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestServeHTTPShutsDownCleanlyWhenContextIsCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveHTTP(ctx, httpServer, listener, time.Second)
	}()

	client := &http.Client{
		Timeout: time.Second,
		Transport: &http.Transport{
			Proxy:             nil,
			DisableKeepAlives: true,
		},
	}
	response, err := client.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatalf("request before shutdown: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveHTTP returned an error during normal shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveHTTP did not finish after cancellation")
	}
}

func TestAuthPasswordFromRuntimeConfigPrefersFile(t *testing.T) {
	t.Setenv("BMANGA_AUTH_PASSWORD", "old-password")
	path := filepath.Join(t.TempDir(), "auth-password.txt")
	if err := os.WriteFile(path, []byte("new-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMANGA_AUTH_PASSWORD_FILE", path)

	if got := authPasswordFromRuntimeConfig(); got != "new-password" {
		t.Fatalf("password = %q, want file value", got)
	}
}

func TestAuthPasswordFromRuntimeConfigFallsBackToEnv(t *testing.T) {
	t.Setenv("BMANGA_AUTH_PASSWORD", "env-password")
	t.Setenv("BMANGA_AUTH_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing.txt"))

	if got := authPasswordFromRuntimeConfig(); got != "env-password" {
		t.Fatalf("password = %q, want env value", got)
	}
}

func TestSessionAuthShowsCustomLoginInsteadOfBrowserChallenge(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	apiReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	apiRec := httptest.NewRecorder()
	handler.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusUnauthorized {
		t.Fatalf("api status = %d, want 401", apiRec.Code)
	}
	if got := apiRec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("unexpected browser auth challenge: %q", got)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusSeeOther {
		t.Fatalf("page status = %d, want redirect", pageRec.Code)
	}
	if got := pageRec.Header().Get("Location"); got != "/login" {
		t.Fatalf("redirect = %q, want /login", got)
	}
}

func TestSessionAuthPreservesV2TargetThroughLogin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	target := "/v2/?tab=discover&q=%E7%82%8E%E6%8B%B3"
	pageReq := httptest.NewRequest(http.MethodGet, target, nil)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusSeeOther {
		t.Fatalf("page status = %d, want redirect", pageRec.Code)
	}
	location, err := url.Parse(pageRec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Path != "/login" {
		t.Fatalf("redirect path = %q, want /login", location.Path)
	}
	if got := location.Query().Get("next"); got != target {
		t.Fatalf("redirect next = %q, want %q", got, target)
	}

	loginPageReq := httptest.NewRequest(http.MethodGet, location.String(), nil)
	loginPageRec := httptest.NewRecorder()
	handler.ServeHTTP(loginPageRec, loginPageReq)
	if !strings.Contains(loginPageRec.Body.String(), `name="next" value="/v2/?tab=discover&amp;q=%E7%82%8E%E6%8B%B3"`) {
		t.Fatalf("login page did not preserve V2 target: %q", loginPageRec.Body.String())
	}
}

func TestSessionAuthRejectsProtocolRelativeLoginTarget(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://bmanga.test//evil.example/path", nil)
	if got := loginTargetForRequest(req); got != "/" {
		t.Fatalf("login target = %q, want root", got)
	}
}

func TestLoginPageV2ResponsiveAndAccessibleContract(t *testing.T) {
	page := loginPageHTML("bmanga", "", "/")
	required := []string{
		`<meta name="viewport" content="width=device-width, initial-scale=1" />`,
		`-webkit-text-size-adjust: 100%;`,
		`text-size-adjust: 100%;`,
		`--paper: #f3eee4;`,
		`--forest: #21382f;`,
		`--vermilion: #a74332;`,
		`font: 16px/1.2 var(--sans);`,
		`data-login-design="paper-ink-v2"`,
		`PRIVATE MANGA LIBRARY`,
		`进入私人书房`,
		`data-login-form`,
		`autocomplete="username"`,
		`autocomplete="current-password"`,
		`data-password-field`,
		`data-password-toggle`,
		`aria-controls="login-password"`,
		`data-password-meta`,
		`role="alert" aria-live="assertive"`,
		`form.setAttribute("aria-busy", "true");`,
		`@media (max-width: 760px)`,
		`env(safe-area-inset-bottom)`,
		`@media (prefers-reduced-motion: reduce)`,
		`translate(count === 1 ? "accessCountOne" : "accessCount").replace("{count}", String(count))`,
		`new URLSearchParams(new FormData(form))`,
		`application/x-www-form-urlencoded;charset=UTF-8`,
		`clearLoginError();`,
		`window.location.replace(data.next || "/v2/");`,
		`data-password-toggle-label>显示</span>`,
	}
	for _, needle := range required {
		if !strings.Contains(page, needle) {
			t.Fatalf("login page missing V2 responsive/accessibility contract: %s", needle)
		}
	}
	for _, obsolete := range []string{`--login-bg-image`, `backdrop-filter`, `brand-stats`, `data.diagnostic`, `服务端收到密码 `} {
		if strings.Contains(page, obsolete) {
			t.Fatalf("login page retained obsolete visual contract: %s", obsolete)
		}
	}
}

func TestLoginPageSecurityHeadersMatchInlineNonce(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	match := regexp.MustCompile(`<style nonce="([A-Za-z0-9_-]+)">`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("login page is missing a CSP style nonce")
	}
	nonce := match[1]
	if !strings.Contains(body, `<script nonce="`+nonce+`">`) {
		t.Fatalf("script nonce does not match style nonce")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, needle := range []string{
		`default-src 'none'`,
		`style-src 'nonce-` + nonce + `'`,
		`script-src 'nonce-` + nonce + `'`,
		`connect-src 'self'`,
		`form-action 'self'`,
		`base-uri 'none'`,
		`frame-ancestors 'none'`,
	} {
		if !strings.Contains(csp, needle) {
			t.Fatalf("CSP missing %q: %q", needle, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("CSP must not allow unsafe-inline: %q", csp)
	}
	for header, want := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestHTMLLoginFailurePreservesDeepTargetForRetry(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")
	target := "/v2/?tab=discover&q=%E7%82%8E%E6%8B%B3"

	failureForm := url.Values{
		"username": {"bmanga"},
		"password": {"wrong-secret"},
		"next":     {target},
	}
	failureReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(failureForm.Encode()))
	failureReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	failureRec := httptest.NewRecorder()
	handler.ServeHTTP(failureRec, failureReq)
	if failureRec.Code != http.StatusUnauthorized {
		t.Fatalf("failure status = %d, want 401", failureRec.Code)
	}
	if !strings.Contains(failureRec.Body.String(), `name="next" value="/v2/?tab=discover&amp;q=%E7%82%8E%E6%8B%B3"`) {
		t.Fatalf("failed login did not preserve deep target")
	}

	successForm := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {target},
	}
	successReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(successForm.Encode()))
	successReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	successRec := httptest.NewRecorder()
	handler.ServeHTTP(successRec, successReq)
	if successRec.Code != http.StatusSeeOther {
		t.Fatalf("success status = %d, want 303", successRec.Code)
	}
	if got := successRec.Header().Get("Location"); got != target {
		t.Fatalf("success location = %q, want %q", got, target)
	}
}

func TestHTMLLoginRateLimitPreservesDeepTargetForRetry(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")
	target := "/v2/?tab=discover&q=%E7%82%8E%E6%8B%B3"
	action := "/login?next=" + url.QueryEscape(target)
	form := url.Values{
		"username": {"bmanga"},
		"password": {"wrong-secret"},
		"next":     {target},
	}

	for attempt := 0; attempt < loginFailureLimit; attempt++ {
		req := httptest.NewRequest(http.MethodPost, action, strings.NewReader(form.Encode()))
		req.RemoteAddr = "192.0.2.24:443"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", attempt+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, action, strings.NewReader(form.Encode()))
	req.RemoteAddr = "192.0.2.24:443"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited status = %d, want 429", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/login?next=%2Fv2%2F%3Ftab%3Ddiscover%26q%3D%25E7%2582%258E%25E6%258B%25B3"`) {
		t.Fatalf("rate-limited login form action did not preserve deep target")
	}
	if !strings.Contains(body, `name="next" value="/v2/?tab=discover&amp;q=%E7%82%8E%E6%8B%B3"`) {
		t.Fatalf("rate-limited login page did not preserve hidden deep target")
	}
}

func TestLoginDefaultsToCanonicalV2Target(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	loginPageHandler := sessionAuth(next, "bmanga", "secret")
	pageReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	pageRec := httptest.NewRecorder()
	loginPageHandler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("login page status = %d, want 200", pageRec.Code)
	}
	if !strings.Contains(pageRec.Body.String(), `name="next" value="/v2/"`) {
		t.Fatalf("login page missing V2 default target")
	}

	for _, accept := range []string{"", "application/json"} {
		t.Run("accept="+accept, func(t *testing.T) {
			handler := sessionAuth(next, "bmanga", "secret")
			form := url.Values{"username": {"bmanga"}, "password": {"secret"}}
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if accept == "" {
				if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != defaultLoginTarget {
					t.Fatalf("HTML login = status %d location %q, want 303 %q", rec.Code, rec.Header().Get("Location"), defaultLoginTarget)
				}
				return
			}
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"next":"/v2/"`) {
				t.Fatalf("JSON login = status %d body %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestNormalizedLoginTargetPreservesLocalDestinationsAndRejectsUnsafeValues(t *testing.T) {
	cases := map[string]string{
		"":                        "/v2/",
		"/v2/library?kind=doujin": "/v2/library?kind=doujin",
		"/classic/?view=review":   "/classic/?view=review",
		"/?view=review":           "/?view=review",
		"//evil.example/path":     "/v2/",
		`/\evil.example/path`:     "/v2/",
		"/%2f%2fevil.example":     "/v2/",
		"/%5c%5cevil.example":     "/v2/",
		"https://evil.example/":   "/v2/",
		"/login?next=/classic/":   "/v2/",
		"/logout":                 "/v2/",
	}
	for input, want := range cases {
		if got := normalizedLoginTarget(input); got != want {
			t.Errorf("normalizedLoginTarget(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSessionAuthLoginParsesMultipartFormData(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("username", "bmanga"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("password", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("next", "/"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/login", &body)
	loginReq.Header.Set("Content-Type", writer.FormDataContentType())
	loginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	loginReq.Header.Set("Accept", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%q", loginRec.Code, loginRec.Body.String())
	}
	if findCookie(loginRec.Result().Cookies(), "bmanga_session") == nil {
		t.Fatal("multipart login did not set session cookie")
	}
}

func TestSessionAuthRejectsOversizedAnonymousLoginBody(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("oversized login request reached the protected handler")
	})
	handler := sessionAuth(next, "bmanga", "secret")
	body := "username=bmanga&password=" + strings.Repeat("x", (1<<20)+1)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized login status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	if findCookie(rec.Result().Cookies(), "bmanga_session") != nil {
		t.Fatal("oversized login unexpectedly set a session cookie")
	}
}

func TestSessionAuthLoginJSONForReplaceNavigation(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/?view=shelf"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	loginReq.Header.Set("Accept", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginRec.Code)
	}
	if got := loginRec.Header().Get("Location"); got != "" {
		t.Fatalf("json login should not redirect, got Location %q", got)
	}
	if !strings.Contains(loginRec.Body.String(), `"ok":true`) || !strings.Contains(loginRec.Body.String(), `"next":"/?view=shelf"`) {
		t.Fatalf("json login body = %q", loginRec.Body.String())
	}
	if findCookie(loginRec.Result().Cookies(), "bmanga_session") == nil {
		t.Fatal("json login did not set session cookie")
	}
	if findCookie(loginRec.Result().Cookies(), "bmanga_write_token") == nil {
		t.Fatal("json login did not set write token cookie")
	}
}

func TestSessionAuthLoginTrimsPastedPasswordWhitespace(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	form := url.Values{
		"username": {" bmanga "},
		"password": {" secret \n"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	loginReq.Header.Set("Accept", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginRec.Code)
	}
	if findCookie(loginRec.Result().Cookies(), "bmanga_session") == nil {
		t.Fatal("trimmed login did not set session cookie")
	}
}

func TestSessionAuthLoginIgnoresInvisiblePasswordCharacters(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	form := url.Values{
		"username": {"bmanga"},
		"password": {"se\u200bcret"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	loginReq.Header.Set("Accept", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginRec.Code)
	}
}

func TestLoginFailureReturnsGenericErrorAndWritesNoDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "login-failures.jsonl")
	t.Setenv("BMANGA_LOGIN_FAILURE_DIAGNOSTICS_PATH", path)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	form := url.Values{
		"username": {"bmanga"},
		"password": {"wrong-secret"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want 401", loginRec.Code)
	}
	body := loginRec.Body.String()
	if strings.Contains(body, "wrong-secret") || strings.Contains(body, "secret") {
		t.Fatalf("json error must not contain password text: %q", body)
	}
	if strings.Contains(body, `"diagnostic"`) || strings.Contains(body, `"user_ok"`) || strings.Contains(body, `"password_"`) {
		t.Fatalf("json error must not disclose login diagnostics: %q", body)
	}
	if !strings.Contains(body, `"error":"账号或密码不对。"`) {
		t.Fatalf("json error must remain generic: %q", body)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("login failure diagnostics file must not be created; stat error = %v", err)
	}
}

func TestSessionAuthRateLimitsRepeatedLoginFailures(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	for i := 0; i < loginFailureLimit; i++ {
		rec := postLogin(handler, "bmanga", "wrong-secret")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, rec.Code)
		}
	}

	rec := postLogin(handler, "bmanga", "wrong-secret")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatal("rate limited response missing Retry-After")
	}
	if !strings.Contains(rec.Body.String(), "登录尝试太频繁") {
		t.Fatalf("rate limited body = %q", rec.Body.String())
	}
}

func TestSessionAuthSuccessfulLoginClearsFailureLimit(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	for i := 0; i < loginFailureLimit-1; i++ {
		rec := postLogin(handler, "bmanga", "wrong-secret")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := postLogin(handler, "bmanga", "secret"); rec.Code != http.StatusOK {
		t.Fatalf("successful login status = %d, want 200", rec.Code)
	}
	if rec := postLogin(handler, "bmanga", "wrong-secret"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-reset failure status = %d, want 401", rec.Code)
	}
}

func TestSessionAuthRateLimitIgnoresForwardedClientIPByDefault(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	for i := 0; i < loginFailureLimit; i++ {
		headers := map[string]string{"X-Forwarded-For": fmt.Sprintf("203.0.113.%d", i+1)}
		rec := postLoginFrom(handler, "bmanga", "wrong-secret", "192.0.2.6:443", headers)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := postLoginFrom(handler, "bmanga", "wrong-secret", "192.0.2.6:443", map[string]string{"X-Forwarded-For": "203.0.113.250"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed forwarded client status = %d, want 429", rec.Code)
	}
}

func TestSessionAuthRateLimitUsesForwardedClientIPFromConfiguredProxy(t *testing.T) {
	t.Setenv("BMANGA_TRUSTED_PROXY_CIDRS", "192.0.2.6/32")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	headers := map[string]string{"X-Forwarded-For": "203.0.113.10"}
	for i := 0; i < loginFailureLimit; i++ {
		rec := postLoginFrom(handler, "bmanga", "wrong-secret", "192.0.2.6:443", headers)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := postLoginFrom(handler, "bmanga", "wrong-secret", "192.0.2.6:443", headers); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("same forwarded client status = %d, want 429", rec.Code)
	}
	otherHeaders := map[string]string{"X-Forwarded-For": "203.0.113.11"}
	if rec := postLoginFrom(handler, "bmanga", "wrong-secret", "192.0.2.6:443", otherHeaders); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other forwarded client status = %d, want 401", rec.Code)
	}
}

func TestSessionAuthAccountRateLimitCannotBeBypassedByRotatingForwardedClientIP(t *testing.T) {
	t.Setenv("BMANGA_TRUSTED_PROXY_CIDRS", "192.0.2.6/32")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	for i := 0; i < defaultLoginAccountFailureLimit; i++ {
		headers := map[string]string{"X-Forwarded-For": fmt.Sprintf("203.0.%d.%d", i/250, i%250+1)}
		rec := postLoginFrom(handler, "bmanga", "wrong-secret", "192.0.2.6:443", headers)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := postLoginFrom(handler, "bmanga", "wrong-secret", "192.0.2.6:443", map[string]string{"X-Forwarded-For": "198.51.100.200"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("account-global status = %d, want 429", rec.Code)
	}
}

func postLogin(handler http.Handler, username string, password string) *httptest.ResponseRecorder {
	return postLoginFrom(handler, username, password, "", nil)
}

func postLoginFrom(handler http.Handler, username string, password string, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	form := url.Values{
		"username": {username},
		"password": {password},
		"next":     {"/"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json")
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestSessionAuthLoginCookieAllowsPage(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")

	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want redirect", loginRec.Code)
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a cookie")
	}
	if findCookie(cookies, "bmanga_write_token") == nil {
		t.Fatal("login did not set write token cookie")
	}
	sessionCookie := findCookie(cookies, "bmanga_session")
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.AddCookie(sessionCookie)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("page status = %d, want 200", pageRec.Code)
	}
	if strings.TrimSpace(pageRec.Body.String()) != "ok" {
		t.Fatalf("page body = %q", pageRec.Body.String())
	}
}

func TestSessionAuthCookieSurvivesServerRestart(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	storePath := filepath.Join(t.TempDir(), sessionStoreFileName)
	firstStore, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	firstHandler := sessionAuthWithStore(next, "bmanga", "secret", firstStore)

	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	firstHandler.ServeHTTP(loginRec, loginReq)
	sessionCookie := findCookie(loginRec.Result().Cookies(), "bmanga_session")
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}
	parts := strings.Split(sessionCookie.Value, ".")
	if len(parts) != 3 {
		t.Fatalf("session cookie has %d parts, want 3", len(parts))
	}
	storedBody, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(storedBody), parts[1]) {
		t.Fatal("persistent session store must not contain the raw nonce")
	}

	restartedStore, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	restartedHandler := sessionAuthWithStore(next, "bmanga", "secret", restartedStore)
	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.AddCookie(sessionCookie)
	pageRec := httptest.NewRecorder()
	restartedHandler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("page status after restart = %d, want 200", pageRec.Code)
	}
}

func TestSessionAuthLogoutRevocationSurvivesServerRestart(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	storePath := filepath.Join(t.TempDir(), sessionStoreFileName)
	store, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	handler := sessionAuthWithStore(next, "bmanga", "secret", store)
	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	sessionCookie := findCookie(loginRec.Result().Cookies(), "bmanga_session")
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}

	logoutReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want redirect", logoutRec.Code)
	}

	restartedStore, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	restartedHandler := sessionAuthWithStore(next, "bmanga", "secret", restartedStore)
	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.AddCookie(sessionCookie)
	pageRec := httptest.NewRecorder()
	restartedHandler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusSeeOther {
		t.Fatalf("revoked cookie status after restart = %d, want redirect", pageRec.Code)
	}
}

func TestSessionAuthLogoutFailsClosedWhenRevocationCannotPersist(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, sessionStoreFileName)
	store, err := newPersistentSessionStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	handler := sessionAuthWithStore(next, "bmanga", "secret", store)
	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	sessionCookie := findCookie(loginRec.Result().Cookies(), "bmanga_session")
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}

	blockedParent := filepath.Join(tempDir, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(blockedParent, sessionStoreFileName)
	logoutReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("logout status = %d, want 503", logoutRec.Code)
	}
	if findCookie(logoutRec.Result().Cookies(), "bmanga_session") != nil {
		t.Fatal("failed logout must not clear the browser session cookie")
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.AddCookie(sessionCookie)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("session status after failed logout = %d, want 200", pageRec.Code)
	}
}

func TestSessionAuthCookieHasNonceAndSecureBehindForwardedHTTPS(t *testing.T) {
	t.Setenv("BMANGA_COOKIE_SECURE", "auto")
	t.Setenv("BMANGA_TRUSTED_PROXY_CIDRS", "192.0.2.1/32")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")
	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/"},
	}
	login := func() *http.Cookie {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		cookies := rec.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("login did not set a cookie")
		}
		session := findCookie(cookies, "bmanga_session")
		writeToken := findCookie(cookies, "bmanga_write_token")
		if session == nil || writeToken == nil {
			t.Fatal("login did not set session and write token cookies")
		}
		if writeToken.HttpOnly {
			t.Fatal("write token cookie must be readable by the web UI")
		}
		if !writeToken.Secure {
			t.Fatal("write token cookie should be Secure behind X-Forwarded-Proto=https")
		}
		return session
	}
	first := login()
	second := login()
	if first.Value == second.Value {
		t.Fatal("two login cookies should differ because sessions include a random nonce")
	}
	if !first.Secure {
		t.Fatal("cookie should be Secure behind X-Forwarded-Proto=https")
	}
}

func TestSessionAuthCookieIgnoresForwardedHTTPSFromUntrustedClient(t *testing.T) {
	t.Setenv("BMANGA_COOKIE_SECURE", "auto")
	t.Setenv("BMANGA_TRUSTED_PROXY_CIDRS", "")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")
	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = "192.0.2.23:443"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	session := findCookie(rec.Result().Cookies(), "bmanga_session")
	writeToken := findCookie(rec.Result().Cookies(), "bmanga_write_token")
	if session == nil || writeToken == nil {
		t.Fatal("login did not set session and write token cookies")
	}
	if session.Secure || writeToken.Secure {
		t.Fatal("untrusted X-Forwarded-Proto must not mark cookies Secure")
	}
}

func TestTrustedProxyPolicyParsesExactIPsAndWalksForwardedChainFromRight(t *testing.T) {
	policy, err := parseTrustedProxyCIDRs("192.0.2.6, 198.51.100.0/24")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.6:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.99, 192.0.2.25, 198.51.100.7")
	if got := policy.forwardedClientIP(req); got != "192.0.2.25" {
		t.Fatalf("forwarded client = %q, want 192.0.2.25", got)
	}
	if _, err := parseTrustedProxyCIDRs("192.0.2.0/not-a-prefix"); err == nil {
		t.Fatal("invalid proxy CIDR should fail closed")
	}
}

func TestSessionAuthLogoutRevokesCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := sessionAuth(next, "bmanga", "secret")
	form := url.Values{
		"username": {"bmanga"},
		"password": {"secret"},
		"next":     {"/"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a cookie")
	}
	sessionCookie := findCookie(cookies, "bmanga_session")
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}

	logoutReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want redirect", logoutRec.Code)
	}
	clearedWriteToken := findCookie(logoutRec.Result().Cookies(), "bmanga_write_token")
	if clearedWriteToken == nil || clearedWriteToken.MaxAge >= 0 {
		t.Fatal("logout did not clear write token cookie")
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.AddCookie(sessionCookie)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusSeeOther {
		t.Fatalf("page status after logout = %d, want redirect", pageRec.Code)
	}
}
