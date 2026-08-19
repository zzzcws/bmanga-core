package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestLoginPageV2LocaleContract(t *testing.T) {
	page := loginPageHTMLWithNonce("bmanga", "账号或密码不对。", "/v2/", "test-nonce")
	required := []string{
		`<html lang="zh-CN">`,
		`<title>登录 · bmanga 私人漫画馆</title>`,
		`var LOCALE_STORAGE_KEY = "bmanga.uiLocale.v1";`,
		`var DEFAULT_LOCALE = "zh-CN";`,
		`var GENERIC_LOGIN_ERROR = "loginFailed";`,
		`"zh-CN": {`,
		`en: {`,
		`ja: {`,
		`data-locale="zh-CN"`,
		`data-locale="en"`,
		`data-locale="ja"`,
		`<span lang="zh-CN">中文</span>`,
		`<span lang="en">English</span>`,
		`<span lang="ja">日本語</span>`,
		`data-i18n="loginTitle"`,
		`data-i18n-placeholder="usernamePlaceholder"`,
		`data-i18n-placeholder="accessPlaceholder"`,
		`data-i18n-aria-label="languageSwitcherAria"`,
		`data-i18n-aria-label="brandAria"`,
		`document.documentElement.lang = currentLocale;`,
		`document.title = translate("documentTitle");`,
		`normalizeLocale(window.localStorage.getItem(LOCALE_STORAGE_KEY))`,
		`window.localStorage.setItem(LOCALE_STORAGE_KEY, locale)`,
		`applyLocale(readStoredLocale());`,
		`if (event.key === LOCALE_STORAGE_KEY || event.key === null) applyLocale(event.newValue);`,
		`var normalized = typeof value === "string" ? value.trim() : "";`,
		`return Object.prototype.hasOwnProperty.call(messages, normalized) ? normalized : DEFAULT_LOCALE;`,
		`return errorKey ? translate(errorKey) : translate("loginFailed");`,
		`loginFailure.loginErrorSource = loginErrorMessage(data);`,
		`errorInvalidCredentials: "The username or password is incorrect."`,
		`errorInvalidCredentials: "ユーザー名またはパスワードが正しくありません。"`,
	}
	for _, needle := range required {
		if !strings.Contains(page, needle) {
			t.Fatalf("login page missing locale contract: %s", needle)
		}
	}

	for _, forbidden := range []string{
		`navigator.language`,
		`navigator.languages`,
		`bmanga.v2.locale.v1`,
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("login page contains forbidden automatic or legacy locale behavior: %s", forbidden)
		}
	}

	translationHook := regexp.MustCompile(`data-i18n(?:-placeholder|-aria-label)?="([A-Za-z][A-Za-z0-9]+)"`)
	for _, match := range translationHook.FindAllStringSubmatch(page, -1) {
		key := match[1]
		if got := strings.Count(page, key+`: `); got != 3 {
			t.Fatalf("locale key %q has %d dictionary entries, want one per locale", key, got)
		}
	}
}

func TestLoginPageV2LocaleKeepsAuthenticationWireContract(t *testing.T) {
	page := loginPageHTMLWithNonce("bmanga", "", "/v2/", "test-nonce")
	required := []string{
		`<form id="login-form" class="login-form" method="post"`,
		`name="next" value="/v2/"`,
		`name="username" type="text"`,
		`name="password" type="password"`,
		`body: new URLSearchParams(new FormData(form))`,
		`credentials: "same-origin"`,
		`"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"`,
		`"X-Requested-With": "XMLHttpRequest"`,
		`window.location.replace(data.next || "/v2/");`,
	}
	for _, needle := range required {
		if !strings.Contains(page, needle) {
			t.Fatalf("login page changed authentication wire contract: %s", needle)
		}
	}
}
