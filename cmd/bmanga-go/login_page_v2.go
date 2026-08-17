package main

import (
	"html"
	"net/url"
	"strings"
)

func loginPageHTMLWithNonce(user string, message string, next string, nonce string) string {
	safeNext := normalizedLoginTarget(next)
	formAction := "/login?next=" + url.QueryEscape(safeNext)
	messageHTML := `<div class="login-error" data-login-error role="alert" aria-live="assertive" aria-atomic="true" tabindex="-1" hidden></div>`
	if strings.TrimSpace(message) != "" {
		messageHTML = `<div class="login-error" data-login-error role="alert" aria-live="assertive" aria-atomic="true" tabindex="-1">` + html.EscapeString(message) + `</div>`
	}
	nonce = html.EscapeString(nonce)
	return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="theme-color" content="#21382f" />
  <title>登录 · bmanga 私人漫画馆</title>
  <style nonce="` + nonce + `">
    :root {
      color-scheme: light;
      --paper: #f3eee4;
      --paper-deep: #e9e1d3;
      --paper-soft: #faf7f0;
      --paper-bright: #fffaf1;
      --ink: #1d231f;
      --ink-soft: #60645d;
      --ink-faint: #676b64;
      --ink-ornament: #929187;
      --forest: #21382f;
      --forest-soft: #355448;
      --forest-pale: #dfe5dd;
      --vermilion: #a74332;
      --vermilion-dark: #863327;
      --danger: #9e3c30;
      --line: rgba(29, 35, 31, 0.14);
      --line-strong: rgba(29, 35, 31, 0.24);
      --line-light: rgba(255, 250, 241, 0.2);
      --focus: rgba(167, 67, 50, 0.22);
      --serif: "Noto Serif SC", "Noto Serif CJK SC", "Source Han Serif SC", "Songti SC", STSong, SimSun, serif;
      --sans: Inter, "Noto Sans SC", "SF Pro Text", "PingFang SC", "Microsoft YaHei", system-ui, sans-serif;
      --ease: cubic-bezier(0.22, 0.76, 0.24, 1);
    }

    *,
    *::before,
    *::after {
      box-sizing: border-box;
    }

    html {
      min-width: 320px;
      min-height: 100%;
      background: var(--paper);
      -webkit-text-size-adjust: 100%;
      text-size-adjust: 100%;
    }

    body {
      min-width: 320px;
      min-height: 100vh;
      min-height: 100dvh;
      margin: 0;
      overflow-x: hidden;
      background:
        linear-gradient(90deg, rgba(29, 35, 31, 0.026) 1px, transparent 1px) 0 0 / 88px 100%,
        radial-gradient(circle at 10% 12%, rgba(167, 67, 50, 0.08), transparent 28%),
        radial-gradient(circle at 91% 86%, rgba(33, 56, 47, 0.09), transparent 31%),
        var(--paper);
      color: var(--ink);
      font-family: var(--sans);
      font-size: 14px;
      line-height: 1.5;
      -webkit-font-smoothing: antialiased;
      text-rendering: optimizeLegibility;
    }

    button,
    input {
      color: inherit;
      font: inherit;
    }

    button {
      cursor: pointer;
      -webkit-tap-highlight-color: transparent;
    }

    button:disabled {
      cursor: wait;
    }

    ::selection {
      background: rgba(167, 67, 50, 0.18);
      color: var(--ink);
    }

    :focus-visible {
      outline: 2px solid var(--vermilion);
      outline-offset: 3px;
    }

    .skip-link {
      position: fixed;
      top: 12px;
      left: 12px;
      z-index: 20;
      min-height: 44px;
      padding: 11px 16px;
      border: 1px solid var(--vermilion-dark);
      background: var(--paper-bright);
      color: var(--ink);
      font-weight: 720;
      text-decoration: none;
      transform: translateY(calc(-100% - 20px));
      transition: transform 180ms var(--ease);
    }

    .skip-link:focus {
      transform: translateY(0);
    }

    .page {
      position: relative;
      display: grid;
      place-items: center;
      min-height: 100vh;
      min-height: 100dvh;
      padding: clamp(28px, 5vw, 72px);
    }

    .login-shell {
      position: relative;
      z-index: 1;
      display: grid;
      grid-template-columns: minmax(0, 1.18fr) minmax(390px, 0.82fr);
      width: min(1120px, 100%);
      min-height: min(680px, calc(100dvh - 96px));
      overflow: hidden;
      border: 1px solid var(--line-strong);
      border-radius: 2px;
      background: var(--paper-soft);
      box-shadow: 0 18px 42px rgba(52, 46, 38, 0.07);
      animation: page-enter 240ms var(--ease) both;
    }

    .login-shell::before {
      content: "";
      position: absolute;
      z-index: 4;
      top: 0;
      left: 0;
      width: 68px;
      height: 3px;
      background: var(--vermilion);
      pointer-events: none;
    }

    .login-editorial {
      position: relative;
      display: flex;
      min-width: 0;
      min-height: 620px;
      flex-direction: column;
      justify-content: space-between;
      padding: clamp(34px, 5vw, 58px);
      overflow: hidden;
      border-right: 1px solid var(--line-light);
      background: var(--forest);
      color: var(--paper-soft);
      isolation: isolate;
    }

    .login-editorial::before {
      content: "";
      position: absolute;
      z-index: -1;
      inset: 0;
      background:
        linear-gradient(90deg, rgba(250, 247, 240, 0.025) 1px, transparent 1px) 0 0 / 76px 100%,
        linear-gradient(180deg, transparent 0 68%, rgba(250, 247, 240, 0.025) 68% 68.2%, transparent 68.2%);
      pointer-events: none;
    }

    .login-editorial::after {
      content: "";
      position: absolute;
      z-index: -1;
      top: 0;
      right: 38px;
      width: 1px;
      height: 114px;
      background: var(--vermilion);
      opacity: 0.9;
    }

    .editorial-topline {
      position: relative;
      z-index: 2;
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 24px;
    }

    .brand-lockup {
      display: flex;
      align-items: flex-start;
      gap: 13px;
      min-width: 0;
    }

    .brand-copy {
      min-width: 0;
    }

    .brand-mark {
      position: relative;
      width: 15px;
      height: 43px;
      flex: 0 0 15px;
      margin-top: 1px;
    }

    .brand-mark::before,
    .brand-mark::after {
      content: "";
      position: absolute;
      width: 7px;
      height: 34px;
    }

    .brand-mark::before {
      top: 0;
      left: 0;
      background: var(--vermilion);
    }

    .brand-mark::after {
      top: 9px;
      left: 7px;
      background: var(--forest-pale);
    }

    .brand-copy strong {
      display: block;
      font-family: Georgia, "Times New Roman", var(--serif);
      font-size: 28px;
      font-weight: 600;
      line-height: 1;
      letter-spacing: -0.7px;
    }

    .brand-copy small {
      display: block;
      margin-top: 9px;
      color: rgba(250, 247, 240, 0.62);
      font-size: 9px;
      font-weight: 720;
      line-height: 1.2;
      letter-spacing: 2.2px;
      text-transform: uppercase;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .folio {
      color: rgba(250, 247, 240, 0.48);
      font-family: Georgia, "Times New Roman", serif;
      font-size: 11px;
      letter-spacing: 1.8px;
      white-space: nowrap;
    }

    .editorial-story {
      position: relative;
      z-index: 2;
      max-width: 570px;
      margin: clamp(64px, 10vh, 116px) 0 60px;
    }

    .kicker,
    .panel-eyebrow {
      margin: 0;
      font-size: 9px;
      font-weight: 740;
      letter-spacing: 2.6px;
      text-transform: uppercase;
    }

    .kicker {
      color: rgba(250, 247, 240, 0.58);
    }

    .editorial-story h1 {
      max-width: 570px;
      margin: 21px 0 22px;
      font-family: var(--serif);
      font-size: clamp(46px, 4.3vw, 60px);
      font-weight: 560;
      line-height: 1.13;
      letter-spacing: -1.8px;
    }

    .editorial-story h1 em {
      position: relative;
      color: var(--paper-bright);
      font-style: normal;
    }

    .editorial-story h1 em::after {
      content: "";
      position: absolute;
      left: 0;
      bottom: -7px;
      width: 64px;
      height: 3px;
      background: var(--vermilion);
    }

    .editorial-story > p:last-child {
      max-width: 440px;
      margin: 0;
      color: rgba(250, 247, 240, 0.7);
      font-size: 14px;
      line-height: 1.85;
    }

    .editorial-ledger {
      position: relative;
      z-index: 2;
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      border-top: 1px solid var(--line-light);
      border-bottom: 1px solid var(--line-light);
    }

    .ledger-item {
      min-width: 0;
      padding: 16px 16px 15px 0;
    }

    .ledger-item + .ledger-item {
      padding-left: 16px;
      border-left: 1px solid var(--line-light);
    }

    .ledger-item strong,
    .ledger-item small {
      display: block;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .ledger-item strong {
      font-family: Georgia, "Times New Roman", serif;
      font-size: 11px;
      font-weight: 600;
      letter-spacing: 1.7px;
    }

    .ledger-item small {
      margin-top: 5px;
      color: rgba(250, 247, 240, 0.5);
      font-size: 10px;
    }

    .editorial-orbit {
      position: absolute;
      z-index: 0;
      top: 86px;
      right: -158px;
      width: 430px;
      height: 430px;
      border: 1px solid rgba(250, 247, 240, 0.08);
      border-radius: 50%;
      pointer-events: none;
    }

    .editorial-orbit::before,
    .editorial-orbit::after {
      content: "";
      position: absolute;
      border: 1px solid rgba(250, 247, 240, 0.065);
      border-radius: 50%;
    }

    .editorial-orbit::before {
      inset: 70px;
    }

    .editorial-orbit::after {
      inset: 138px;
      background: rgba(250, 247, 240, 0.025);
    }

    .issue-mark {
      position: absolute;
      z-index: 0;
      right: 18px;
      bottom: 42px;
      color: rgba(250, 247, 240, 0.045);
      font-family: Georgia, "Times New Roman", serif;
      font-size: clamp(120px, 14vw, 210px);
      line-height: 0.75;
      letter-spacing: -8px;
      pointer-events: none;
      user-select: none;
    }

    .login-panel {
      position: relative;
      display: flex;
      min-width: 0;
      flex-direction: column;
      justify-content: center;
      padding: clamp(38px, 5vw, 64px);
      background:
        linear-gradient(90deg, rgba(29, 35, 31, 0.022) 1px, transparent 1px) 0 0 / 72px 100%,
        var(--paper-soft);
    }

    .login-panel::after {
      content: "";
      position: absolute;
      right: 0;
      bottom: 0;
      width: 82px;
      height: 3px;
      background: var(--forest);
    }

    .panel-topline {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 18px;
      margin-bottom: clamp(42px, 7vh, 72px);
    }

    .panel-eyebrow {
      color: var(--ink-faint);
    }

    .private-state {
      display: inline-flex;
      align-items: center;
      gap: 7px;
      min-height: 28px;
      padding: 5px 9px;
      border: 1px solid var(--line);
      color: var(--ink-soft);
      font-size: 9px;
      font-weight: 760;
      letter-spacing: 1.4px;
      text-transform: uppercase;
      white-space: nowrap;
    }

    .private-state::before {
      content: "";
      width: 5px;
      height: 5px;
      background: var(--forest);
      border-radius: 50%;
    }

    .login-intro h2 {
      margin: 0;
      font-family: var(--serif);
      font-size: clamp(34px, 3.5vw, 46px);
      font-weight: 560;
      line-height: 1.12;
      letter-spacing: -1px;
    }

    .login-intro p {
      margin: 13px 0 0;
      color: var(--ink-soft);
      font-size: 13px;
      line-height: 1.75;
    }

    .login-error {
      margin: 24px 0 0;
      padding: 12px 14px;
      border: 1px solid rgba(158, 60, 48, 0.28);
      border-left: 3px solid var(--danger);
      background: rgba(158, 60, 48, 0.045);
      color: var(--danger);
      font-size: 13px;
      line-height: 1.6;
    }

    .login-error[hidden] {
      display: none;
    }

    .login-form {
      display: grid;
      gap: 21px;
      margin-top: 31px;
    }

    .field-group {
      min-width: 0;
    }

    .field-label {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: 14px;
      margin: 0 0 8px;
      color: var(--ink);
      font-size: 12px;
      font-weight: 720;
    }

    .field-label small {
      color: var(--ink-ornament);
      font-family: Georgia, "Times New Roman", serif;
      font-size: 9px;
      font-weight: 600;
      letter-spacing: 1.6px;
    }

    .input-frame,
    .password-frame {
      position: relative;
      display: flex;
      min-width: 0;
      align-items: stretch;
      border: 1px solid var(--line-strong);
      border-radius: 2px;
      background: var(--paper-bright);
      transition: border-color 180ms var(--ease), box-shadow 180ms var(--ease), background 180ms var(--ease);
    }

    .input-frame:focus-within,
    .password-frame:focus-within {
      border-color: var(--forest-soft);
      background: #fffdf8;
      box-shadow: 0 0 0 3px rgba(33, 56, 47, 0.08);
    }

    .input-frame input,
    .password-frame input {
      width: 100%;
      min-width: 0;
      height: 50px;
      padding: 0 14px;
      border: 0;
      outline: 0;
      background: transparent;
      color: var(--ink);
      font: 16px/1.2 var(--sans);
    }

    .input-frame input::placeholder,
    .password-frame input::placeholder {
      color: var(--ink-ornament);
    }

    .password-toggle {
      min-width: 68px;
      min-height: 50px;
      padding: 0 12px;
      border: 0;
      border-left: 1px solid var(--line);
      background: transparent;
      color: var(--forest-soft);
      font-size: 12px;
      font-weight: 760;
      transition: color 180ms var(--ease), background 180ms var(--ease);
    }

    .password-toggle:hover {
      background: rgba(33, 56, 47, 0.055);
      color: var(--forest);
    }

    .field-meta {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 14px;
      min-height: 18px;
      margin-top: 7px;
      color: var(--ink-faint);
      font-size: 10px;
    }

    .login-submit {
      position: relative;
      display: grid;
      grid-template-columns: minmax(0, 1fr) 52px;
      align-items: stretch;
      width: 100%;
      min-height: 52px;
      margin-top: 4px;
      padding: 0;
      overflow: hidden;
      border: 1px solid var(--vermilion-dark);
      border-radius: 2px;
      background: var(--vermilion);
      color: var(--paper-bright);
      font-weight: 760;
      transition: background 180ms var(--ease), transform 180ms var(--ease), box-shadow 180ms var(--ease);
    }

    .login-submit:hover:not(:disabled) {
      background: var(--vermilion-dark);
      transform: translateY(-1px);
    }

    .login-submit:active:not(:disabled) {
      transform: translateY(0);
    }

    .login-submit:disabled {
      opacity: 0.72;
    }

    .submit-label {
      display: grid;
      place-items: center;
      padding-left: 52px;
    }

    .submit-arrow {
      display: grid;
      place-items: center;
      border-left: 1px solid rgba(255, 250, 241, 0.25);
      font-family: Georgia, "Times New Roman", serif;
      font-size: 20px;
      font-weight: 400;
    }

    .privacy-note {
      display: grid;
      grid-template-columns: 28px minmax(0, 1fr);
      gap: 12px;
      align-items: start;
      margin-top: 28px;
      padding-top: 18px;
      border-top: 1px solid var(--line);
    }

    .privacy-seal {
      position: relative;
      display: grid;
      place-items: center;
      width: 28px;
      height: 28px;
      border: 1px solid var(--line-strong);
      border-radius: 50%;
    }

    .privacy-seal::before {
      content: "";
      width: 6px;
      height: 6px;
      background: var(--vermilion);
      border-radius: 50%;
    }

    .privacy-note p {
      margin: 0;
      color: var(--ink-faint);
      font-size: 10px;
      line-height: 1.65;
    }

    .privacy-note strong {
      display: block;
      margin-bottom: 2px;
      color: var(--ink-soft);
      font-size: 11px;
    }

    .panel-foot {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      margin-top: 36px;
      color: var(--ink-ornament);
      font-family: Georgia, "Times New Roman", serif;
      font-size: 8px;
      letter-spacing: 1.35px;
      text-transform: uppercase;
    }

    @keyframes page-enter {
      from {
        opacity: 0;
        transform: translateY(8px);
      }
      to {
        opacity: 1;
        transform: translateY(0);
      }
    }

    @media (max-width: 900px) {
      .page {
        padding: 24px;
      }

      .login-shell {
        grid-template-columns: minmax(0, 0.95fr) minmax(360px, 1.05fr);
        min-height: calc(100dvh - 48px);
      }

      .login-editorial {
        padding: 36px 32px;
      }

      .folio {
        display: none;
      }

      .editorial-story h1 {
        font-size: clamp(42px, 6vw, 56px);
      }

      .ledger-item {
        padding-right: 10px;
      }

      .ledger-item + .ledger-item {
        padding-left: 10px;
      }

      .login-panel {
        padding: 40px 34px;
      }
    }

    @media (max-width: 760px) {
      .login-shell::before {
        width: 54px;
      }

      .page {
        display: block;
        min-height: 100dvh;
        padding: 0;
      }

      .login-shell {
        grid-template-columns: minmax(0, 1fr);
        width: 100%;
        min-height: 100dvh;
        border: 0;
        border-radius: 0;
        box-shadow: none;
      }

      .login-editorial {
        min-height: 280px;
        padding:
          max(26px, env(safe-area-inset-top))
          max(22px, calc(22px + env(safe-area-inset-right)))
          26px
          max(22px, calc(22px + env(safe-area-inset-left)));
        border-right: 0;
        border-bottom: 1px solid var(--line-light);
      }

      .login-editorial::after {
        right: 24px;
        height: 68px;
      }

      .brand-copy strong {
        font-size: 25px;
      }

      .editorial-story {
        margin: 34px 0 28px;
      }

      .editorial-story h1 {
        margin: 14px 0 12px;
        font-size: clamp(34px, 9.6vw, 46px);
        line-height: 1.16;
        letter-spacing: -1px;
      }

      .editorial-story h1 br {
        display: none;
      }

      .editorial-story h1 em::after {
        bottom: -4px;
        width: 44px;
        height: 2px;
      }

      .editorial-story > p:last-child {
        max-width: 520px;
        font-size: 12px;
        line-height: 1.7;
      }

      .editorial-ledger {
        display: none;
      }

      .editorial-orbit {
        top: -88px;
        right: -142px;
        width: 330px;
        height: 330px;
      }

      .issue-mark {
        right: 6px;
        bottom: 24px;
        font-size: 118px;
      }

      .login-panel {
        min-height: calc(100dvh - 280px);
        justify-content: flex-start;
        padding:
          34px
          max(22px, calc(22px + env(safe-area-inset-right)))
          max(28px, calc(28px + env(safe-area-inset-bottom)))
          max(22px, calc(22px + env(safe-area-inset-left)));
      }

      .panel-topline {
        margin-bottom: 36px;
      }

      .login-intro h2 {
        font-size: 36px;
      }

      .panel-foot {
        margin-top: 30px;
      }
    }

    @media (max-width: 374px) {
      .login-editorial {
        min-height: 248px;
        padding-inline: 18px;
      }

      .brand-copy small {
        max-width: 190px;
        overflow: hidden;
        text-overflow: ellipsis;
      }

      .editorial-story {
        margin-top: 27px;
      }

      .editorial-story h1 {
        font-size: 32px;
      }

      .editorial-story > p:last-child {
        display: none;
      }

      .login-panel {
        min-height: calc(100dvh - 248px);
        padding: 28px 18px max(24px, env(safe-area-inset-bottom));
      }

      .panel-topline {
        margin-bottom: 30px;
      }

      .login-intro h2 {
        font-size: 32px;
      }

      .login-form {
        gap: 18px;
        margin-top: 26px;
      }
    }

    @media (min-width: 761px) and (max-height: 760px) {
      .page {
        padding-block: 24px;
      }

      .login-shell {
        min-height: calc(100dvh - 48px);
      }

      .login-editorial {
        min-height: 580px;
      }

      .editorial-story {
        margin-block: 44px;
      }

      .panel-topline {
        margin-bottom: 38px;
      }
    }

    @media (prefers-reduced-motion: reduce) {
      *,
      *::before,
      *::after {
        scroll-behavior: auto !important;
        animation-duration: 0.01ms !important;
        animation-iteration-count: 1 !important;
        transition-duration: 0.01ms !important;
      }
    }

    @media (forced-colors: active) {
      .login-shell,
      .login-editorial,
      .login-panel,
      .input-frame,
      .password-frame,
      .login-submit,
      .private-state {
        forced-color-adjust: auto;
      }

      .brand-mark::before,
      .brand-mark::after,
      .private-state::before,
      .privacy-seal::before {
        background: currentColor;
      }
    }
  </style>
</head>
<body>
  <a class="skip-link" href="#login-form">跳到登录表单</a>
  <div class="page">
    <main class="login-shell" data-login-design="paper-ink-v2">
      <section class="login-editorial" aria-labelledby="brand-title">
        <div class="editorial-topline">
          <div class="brand-lockup" aria-label="bmanga 私人漫画馆">
            <span class="brand-mark" aria-hidden="true"></span>
            <span class="brand-copy">
              <strong>bmanga</strong>
              <small>PRIVATE MANGA LIBRARY</small>
            </span>
          </div>
          <span class="folio">EDITION · V2</span>
        </div>
        <div class="editorial-story">
          <p class="kicker">PRIVATE READING · PERSONAL ARCHIVE</p>
          <h1 id="brand-title">纸墨之间，<br />回到<em>自己的书架。</em></h1>
          <p>阅读、整理与进度，安静地留在你的私人空间。每次回来，都从上一次停下的地方继续。</p>
        </div>
        <div class="editorial-ledger" aria-hidden="true">
          <span class="ledger-item"><strong>READ</strong><small>沉浸阅读</small></span>
          <span class="ledger-item"><strong>KEEP</strong><small>私人收藏</small></span>
          <span class="ledger-item"><strong>RESUME</strong><small>跨设备续读</small></span>
        </div>
        <span class="editorial-orbit" aria-hidden="true"></span>
        <span class="issue-mark" aria-hidden="true">01</span>
      </section>

      <section class="login-panel" aria-labelledby="login-title">
        <div class="panel-topline">
          <p class="panel-eyebrow">LIBRARY ACCESS</p>
          <span class="private-state">PROTECTED</span>
        </div>
        <div class="login-intro">
          <h2 id="login-title">进入私人书房</h2>
          <p>输入用户名和访问密码，继续上一次阅读。</p>
        </div>
        ` + messageHTML + `
        <form id="login-form" class="login-form" method="post" action="` + html.EscapeString(formAction) + `" data-login-form aria-describedby="login-privacy" tabindex="-1">
          <input type="hidden" name="next" value="` + html.EscapeString(safeNext) + `" />
          <div class="field-group">
            <label class="field-label" for="login-username"><span>用户名</span><small>USER</small></label>
            <span class="input-frame">
              <input id="login-username" name="username" type="text" value="` + html.EscapeString(user) + `" autocomplete="username" autocapitalize="none" spellcheck="false" required />
            </span>
          </div>
          <div class="field-group">
            <label class="field-label" for="login-password"><span>访问密码</span><small>PASSWORD</small></label>
            <span class="password-frame">
              <input id="login-password" name="password" type="password" autocomplete="current-password" autocapitalize="none" autocorrect="off" spellcheck="false" required autofocus data-password-field aria-describedby="password-meta" />
              <button class="password-toggle" type="button" data-password-toggle aria-controls="login-password" aria-label="显示密码" aria-pressed="false"><span data-password-toggle-label>显示</span></button>
            </span>
            <span class="field-meta"><span>区分大小写</span><span id="password-meta" data-password-meta>0 位</span></span>
          </div>
          <button class="login-submit" type="submit">
            <span class="submit-label" data-submit-label>进入书库</span>
            <span class="submit-arrow" aria-hidden="true">→</span>
          </button>
        </form>
        <div class="privacy-note" id="login-privacy">
          <span class="privacy-seal" aria-hidden="true"></span>
          <p><strong>受保护的私人访问</strong>此浏览器会保存登录会话；退出或会话失效后需要重新验证。</p>
        </div>
        <div class="panel-foot" aria-hidden="true">
          <span>BMANGA / LOCAL-FIRST</span>
          <span>V2 READING ROOM</span>
        </div>
      </section>
    </main>
  </div>
  <script nonce="` + nonce + `">
    (function () {
      var form = document.querySelector("[data-login-form]");
      var errorSlot = document.querySelector("[data-login-error]");
      var usernameField = form ? form.querySelector("input[name='username']") : null;
      var passwordField = document.querySelector("[data-password-field]");
      var passwordToggle = document.querySelector("[data-password-toggle]");
      var passwordToggleLabel = document.querySelector("[data-password-toggle-label]");
      var passwordMeta = document.querySelector("[data-password-meta]");
      var submitLabel = document.querySelector("[data-submit-label]");
      function clearLoginError() {
        if (!errorSlot) return;
        errorSlot.hidden = true;
        errorSlot.textContent = "";
      }
      function loginErrorMessage(data) {
		return (data && data.error) || "登录失败，请再试一次。";
      }
      function updatePasswordMeta() {
        if (!passwordField || !passwordMeta) return;
        passwordMeta.textContent = Array.from(passwordField.value || "").length + " 位";
      }
      if (usernameField) {
        usernameField.addEventListener("input", clearLoginError);
        usernameField.addEventListener("change", clearLoginError);
      }
      if (passwordField) {
        passwordField.addEventListener("input", function () {
          clearLoginError();
          updatePasswordMeta();
        });
        passwordField.addEventListener("change", function () {
          clearLoginError();
          updatePasswordMeta();
        });
        updatePasswordMeta();
      }
      if (passwordField && passwordToggle) {
        passwordToggle.addEventListener("click", function () {
          clearLoginError();
          var visible = passwordField.type === "text";
          passwordField.type = visible ? "password" : "text";
          if (passwordToggleLabel) passwordToggleLabel.textContent = visible ? "显示" : "隐藏";
          passwordToggle.setAttribute("aria-label", visible ? "显示密码" : "隐藏密码");
          passwordToggle.setAttribute("aria-pressed", visible ? "false" : "true");
          passwordField.focus();
          updatePasswordMeta();
        });
      }
      if (!form || !window.fetch || !window.location || !window.location.replace) return;
      form.addEventListener("submit", async function (event) {
        event.preventDefault();
        var button = form.querySelector("button[type='submit']");
        var originalLabel = submitLabel ? submitLabel.textContent : "";
        form.setAttribute("aria-busy", "true");
        if (button) button.disabled = true;
        if (submitLabel) submitLabel.textContent = "正在验证";
        clearLoginError();
        try {
          var response = await fetch(form.action, {
            method: "POST",
            body: new URLSearchParams(new FormData(form)),
            credentials: "same-origin",
            headers: {
              "Accept": "application/json",
              "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
              "X-Requested-With": "XMLHttpRequest"
            }
          });
          var data = {};
          try {
            data = await response.json();
          } catch (_) {}
          if (!response.ok || !data.ok) {
            throw new Error(loginErrorMessage(data));
          }
          window.location.replace(data.next || "/v2/");
        } catch (error) {
          if (errorSlot) {
            errorSlot.textContent = error.message || String(error);
            errorSlot.hidden = false;
            errorSlot.focus();
          } else {
            alert(error.message || String(error));
          }
          form.setAttribute("aria-busy", "false");
          if (button) button.disabled = false;
          if (submitLabel) submitLabel.textContent = originalLabel;
        }
      });
    })();
  </script>
</body>
</html>`
}
