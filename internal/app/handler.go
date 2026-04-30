package app

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

type commandSender interface {
	SendCommand(ctx context.Context, command string) (string, error)
}

func NewHandler(client commandSender, auth *AuthService, google *GoogleService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleHome(auth))
	mux.HandleFunc("/command", handleCommand(client, auth))
	mux.Handle("/api/google/", NewGoogleAPIHandler(google, auth))
	mux.HandleFunc("/login/naver", handleNaverLogin(auth))
	mux.HandleFunc("/auth/naver/callback", handleNaverCallback(auth))
	mux.HandleFunc("/logout", handleLogout(auth))
	mux.HandleFunc("/api/health", handleHealthAPI(auth))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func handleHome(auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		user, ok := auth.CurrentUserOrDev(r)
		log.Printf("[home] user=%q ok=%v", user.ID, ok)
		if !ok {
			renderPage(w, pageData{AuthEnabled: auth.Enabled()})
			return
		}
		renderPage(w, pageData{AuthEnabled: auth.Enabled(), User: user})
	}
}

func handleCommand(client commandSender, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user, ok := auth.CurrentUserOrDev(r)
		if !ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		command := strings.TrimSpace(r.FormValue("command"))
		activeTab := normalizeTab(r.FormValue("tab"))
		data := pageData{AuthEnabled: auth.Enabled(), User: user, ActiveTab: activeTab, Command: command}
		if command == "" {
			data.Error = "명령을 입력해주세요."
			renderPage(w, data)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
		defer cancel()

		reply, err := client.SendCommand(ctx, commandForTab(activeTab, command))
		if err != nil {
			data.Error = err.Error()
			renderPage(w, data)
			return
		}

		data.Reply = reply
		renderPage(w, data)
	}
}

func normalizeTab(value string) string {
	switch value {
	case "trader", "builder", "asset-manager", "health":
		return value
	default:
		return "trader"
	}
}

func commandForTab(tab, command string) string {
	switch tab {
	case "builder":
		return "[Website Builder]\n" + command
	case "asset-manager":
		return "[Asset Manager]\n" + command
	default:
		return "[Trader]\n" + command
	}
}

func handleNaverLogin(auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.Enabled() {
			http.Error(w, "naver login is not configured", http.StatusServiceUnavailable)
			return
		}
		state, err := newState()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		auth.SetState(w, r, state)
		http.Redirect(w, r, auth.LoginURL(state), http.StatusFound)
	}
}

func handleNaverCallback(auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.Enabled() {
			http.Error(w, "naver login is not configured", http.StatusServiceUnavailable)
			return
		}
		queryState := r.URL.Query().Get("state")
		cookieState, ok := auth.PopState(w, r)
		if !ok || queryState == "" || queryState != cookieState {
			http.Error(w, "invalid oauth state", http.StatusBadRequest)
			return
		}
		if oauthError := r.URL.Query().Get("error"); oauthError != "" {
			http.Error(w, oauthError, http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing oauth code", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		accessToken, err := auth.ExchangeCode(ctx, code, queryState)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		user, err := auth.FetchUser(ctx, accessToken)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := auth.SetSession(w, r, user); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleLogout(auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth.ClearSession(w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

type pageData struct {
	AuthEnabled bool
	User        User
	ActiveTab   string
	Command     string
	Reply       string
	Error       string
}

func renderPage(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ko">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OpenClaw</title>
  <style>
    :root {
      --bg: #F5F6F8;
      --sidebar-bg: #FFFFFF;
      --card: #FFFFFF;
      --text: #191F28;
      --sub: #4E5968;
      --muted: #8B95A1;
      --line: #E5E8EB;
      --accent: #3182F6;
      --accent-bg: #EBF3FE;
      --accent-hover: #1B6EE4;
      --danger: #E53935;
      --danger-bg: #FFF5F5;
      --warning: #F59F00;
      --shadow-sm: 0 1px 2px rgba(0,0,0,.06), 0 2px 6px rgba(0,0,0,.04);
      --shadow: 0 2px 8px rgba(0,0,0,.06), 0 1px 3px rgba(0,0,0,.04);
    }
    [data-theme="dark"] {
      --bg: #111318;
      --sidebar-bg: #1A1E27;
      --card: #1E2330;
      --text: #F2F4F6;
      --sub: #B0B8C1;
      --muted: #6B7684;
      --line: #2B3245;
      --accent: #4A90F5;
      --accent-bg: #1A2840;
      --accent-hover: #6AA8FF;
      --danger: #FF6B6B;
      --danger-bg: #2A1818;
      --warning: #FFCC00;
      --shadow-sm: 0 1px 2px rgba(0,0,0,.2), 0 2px 6px rgba(0,0,0,.16);
      --shadow: 0 2px 8px rgba(0,0,0,.22), 0 1px 3px rgba(0,0,0,.16);
    }
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: var(--bg);
      color: var(--text);
      font-family: -apple-system, BlinkMacSystemFont, "Apple SD Gothic Neo", "Pretendard Variable", "Pretendard", "Noto Sans KR", sans-serif;
      line-height: 1.5;
      -webkit-font-smoothing: antialiased;
      min-height: 100vh;
    }

    /* ── 로그인 ── */
    .login-wrap {
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 24px;
      padding: 24px;
    }
    .login-card {
      background: var(--card);
      border-radius: 20px;
      box-shadow: 0 4px 24px rgba(0,0,0,.08), 0 1px 4px rgba(0,0,0,.04);
      padding: 48px 40px 40px;
      width: min(380px, 100%);
      text-align: center;
    }
    .brand-badge {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 56px;
      height: 56px;
      border-radius: 16px;
      background: var(--accent);
      color: #fff;
      font-size: 18px;
      font-weight: 900;
      letter-spacing: -0.5px;
      margin-bottom: 20px;
    }
    .login-card h1 {
      font-size: 22px;
      font-weight: 800;
      letter-spacing: -0.5px;
      margin-bottom: 6px;
    }
    .login-card .login-desc {
      font-size: 14px;
      color: var(--muted);
      margin-bottom: 32px;
    }
    .btn-naver {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      width: 100%;
      height: 52px;
      border-radius: 12px;
      background: #03C75A;
      color: #fff;
      font-size: 15px;
      font-weight: 700;
      text-decoration: none;
      transition: background .15s;
      border: none;
      cursor: pointer;
    }
    .btn-naver:hover { background: #02B34E; }

    /* ── 앱 셸 ── */
    .shell { display: flex; min-height: 100vh; }
    .sidebar {
      width: 240px;
      flex: 0 0 240px;
      background: var(--sidebar-bg);
      border-right: 1px solid var(--line);
      display: flex;
      flex-direction: column;
      position: sticky;
      top: 0;
      height: 100vh;
      overflow: hidden;
      transition: width .2s ease, flex-basis .2s ease;
    }
    body.sb-collapsed .sidebar { width: 64px; flex-basis: 64px; }

    .sb-header {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 18px 16px;
      border-bottom: 1px solid var(--line);
      min-height: 66px;
      overflow: hidden;
    }
    .brand-icon {
      width: 36px;
      height: 36px;
      flex: 0 0 36px;
      border-radius: 10px;
      background: var(--accent);
      color: #fff;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      font-size: 13px;
      font-weight: 900;
      letter-spacing: -0.5px;
    }
    .brand-info { overflow: hidden; min-width: 0; }
    .brand-name {
      font-size: 15px;
      font-weight: 800;
      color: var(--text);
      white-space: nowrap;
      letter-spacing: -0.3px;
    }
    .brand-tagline {
      font-size: 11px;
      color: var(--muted);
      white-space: nowrap;
      margin-top: 1px;
    }

    .sb-nav {
      flex: 1;
      padding: 10px 8px;
      display: flex;
      flex-direction: column;
      gap: 2px;
      overflow-y: auto;
    }
    .nav-item {
      display: flex;
      align-items: center;
      gap: 10px;
      height: 44px;
      padding: 0 10px;
      border-radius: 10px;
      border: none;
      background: transparent;
      color: var(--muted);
      font: inherit;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      text-align: left;
      transition: background .1s, color .1s;
      white-space: nowrap;
      overflow: hidden;
      letter-spacing: -0.1px;
    }
    .nav-item:hover { background: var(--line); color: var(--sub); }
    .nav-item.active { background: var(--accent-bg); color: var(--accent); }
    .nav-icon { font-size: 16px; flex: 0 0 22px; text-align: center; }
    .nav-label { overflow: hidden; text-overflow: ellipsis; }

    .sb-footer {
      padding: 10px 8px 14px;
      border-top: 1px solid var(--line);
      display: flex;
      align-items: center;
      gap: 8px;
      overflow: hidden;
    }
    .user-dot {
      width: 32px;
      height: 32px;
      flex: 0 0 32px;
      border-radius: 50%;
      background: var(--accent-bg);
      display: inline-flex;
      align-items: center;
      justify-content: center;
      font-size: 14px;
    }
    .user-meta { flex: 1; min-width: 0; overflow: hidden; }
    .user-name {
      font-size: 13px;
      font-weight: 700;
      color: var(--text);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .user-id-label { font-size: 11px; color: var(--muted); white-space: nowrap; }
    .icon-btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 30px;
      height: 30px;
      flex: 0 0 30px;
      border-radius: 8px;
      background: transparent;
      border: 1px solid var(--line);
      color: var(--muted);
      cursor: pointer;
      font-size: 13px;
      transition: background .1s;
    }
    .icon-btn:hover { background: var(--line); color: var(--sub); }

    body.sb-collapsed .brand-info,
    body.sb-collapsed .nav-label,
    body.sb-collapsed .user-meta { display: none; }
    body.sb-collapsed .sb-footer { justify-content: center; flex-wrap: wrap; gap: 6px; }
    body.sb-collapsed .sb-header { justify-content: center; padding: 15px; }

    /* ── 메인 ── */
    .main-area { flex: 1; min-width: 0; display: flex; flex-direction: column; }
    .topbar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 16px 28px;
      background: var(--card);
      border-bottom: 1px solid var(--line);
      position: sticky;
      top: 0;
      z-index: 10;
    }
    .page-title {
      font-size: 18px;
      font-weight: 800;
      letter-spacing: -0.5px;
      color: var(--text);
    }
    .page-sub { font-size: 12px; color: var(--muted); margin-top: 1px; }
    .topbar-right { display: flex; align-items: center; gap: 8px; }
    .topbar-user { font-size: 13px; color: var(--sub); white-space: nowrap; }
    .btn-logout {
      display: inline-flex;
      align-items: center;
      height: 30px;
      padding: 0 12px;
      border-radius: 8px;
      border: 1px solid var(--line);
      background: transparent;
      color: var(--muted);
      font: inherit;
      font-size: 13px;
      font-weight: 600;
      text-decoration: none;
      cursor: pointer;
      transition: background .1s;
    }
    .btn-logout:hover { background: var(--line); color: var(--sub); }

    .content {
      flex: 1;
      padding: 28px 32px;
      width: min(820px, 100%);
      margin: 0 auto;
    }

    /* ── 커맨드 ── */
    .cmd-card {
      background: var(--card);
      border-radius: 16px;
      box-shadow: var(--shadow-sm);
      padding: 22px;
    }
    .cmd-label {
      font-size: 12px;
      font-weight: 700;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0.06em;
      margin-bottom: 10px;
    }
    .cmd-input {
      width: 100%;
      min-height: 160px;
      resize: vertical;
      border: 1.5px solid var(--line);
      border-radius: 10px;
      padding: 14px 16px;
      background: var(--bg);
      color: var(--text);
      font: inherit;
      font-size: 15px;
      line-height: 1.6;
      outline: none;
      transition: border-color .15s;
    }
    .cmd-input:focus { border-color: var(--accent); }
    .cmd-input::placeholder { color: var(--muted); }
    .cmd-actions { display: flex; justify-content: flex-end; margin-top: 12px; }
    .btn-send {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      height: 44px;
      padding: 0 22px;
      border-radius: 10px;
      background: var(--accent);
      color: #fff;
      border: none;
      font: inherit;
      font-size: 15px;
      font-weight: 700;
      cursor: pointer;
      letter-spacing: -0.2px;
      transition: background .15s;
    }
    .btn-send:hover { background: var(--accent-hover); }

    .result-card {
      background: var(--card);
      border-radius: 16px;
      box-shadow: var(--shadow-sm);
      padding: 22px;
      margin-top: 14px;
      font-size: 15px;
      line-height: 1.7;
      white-space: pre-wrap;
      color: var(--text);
    }
    .result-card.is-error {
      background: var(--danger-bg);
      color: var(--danger);
    }

    /* ── 헬스 패널 ── */
    #health-panel { display: none; }
    .health-grid {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 14px;
    }
    .health-card {
      background: var(--card);
      border-radius: 16px;
      box-shadow: var(--shadow-sm);
      padding: 22px 20px;
    }
    .hc-emoji { font-size: 22px; margin-bottom: 14px; }
    .hc-label {
      font-size: 11px;
      font-weight: 700;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0.08em;
      margin-bottom: 8px;
    }
    .hc-value {
      font-size: 44px;
      font-weight: 800;
      color: var(--text);
      line-height: 1;
      letter-spacing: -2px;
      margin-bottom: 18px;
    }
    .hc-value .hc-unit {
      font-size: 18px;
      font-weight: 700;
      letter-spacing: -0.5px;
      color: var(--sub);
    }
    .hc-track {
      height: 5px;
      background: var(--line);
      border-radius: 99px;
      overflow: hidden;
      margin-bottom: 10px;
    }
    .hc-fill {
      height: 100%;
      border-radius: 99px;
      background: var(--accent);
      width: 0%;
      transition: width .5s cubic-bezier(.4,0,.2,1), background .3s;
    }
    .hc-fill.warn { background: var(--warning); }
    .hc-fill.crit { background: var(--danger); }
    .hc-detail { font-size: 12px; color: var(--muted); font-weight: 600; }
    .health-ts {
      font-size: 11px;
      color: var(--muted);
      text-align: right;
      margin-top: 14px;
      letter-spacing: 0.01em;
    }

    /* ── 반응형 ── */
    @media (max-width: 860px) {
      .health-grid { grid-template-columns: 1fr; }
    }
    @media (max-width: 760px) {
      .sidebar { width: 64px; flex-basis: 64px; }
      .brand-info, .nav-label, .user-meta { display: none; }
      .sb-footer { justify-content: center; flex-wrap: wrap; }
      .sb-header { justify-content: center; padding: 15px; }
      .content { padding: 20px 16px; }
      .topbar { padding: 12px 16px; }
    }
    @media (max-width: 480px) { .topbar-user { display: none; } }
  </style>
</head>
<body>
{{if .User.ID}}
<div class="shell">
  <aside class="sidebar">
    <div class="sb-header">
      <span class="brand-icon">OC</span>
      <div class="brand-info">
        <div class="brand-name">OpenClaw</div>
        <div class="brand-tagline">Assistant Console</div>
      </div>
    </div>

    <nav class="sb-nav">
      <button class="nav-item" type="button" data-tab="trader">
        <span class="nav-icon">◇</span>
        <span class="nav-label">Trader</span>
      </button>
      <button class="nav-item" type="button" data-tab="builder">
        <span class="nav-icon">▣</span>
        <span class="nav-label">Website Builder</span>
      </button>
      <button class="nav-item" type="button" data-tab="asset-manager">
        <span class="nav-icon">▤</span>
        <span class="nav-label">Asset Manager</span>
      </button>
      <button class="nav-item" type="button" data-tab="health">
        <span class="nav-icon">◉</span>
        <span class="nav-label">OpenClaw Health</span>
      </button>
    </nav>

    <div class="sb-footer">
      <span class="user-dot">👤</span>
      <div class="user-meta">
        <div class="user-name">{{if .User.Nickname}}{{.User.Nickname}}{{else}}{{.User.ID}}{{end}}</div>
        <div class="user-id-label">{{.User.ID}}</div>
      </div>
      <button class="icon-btn" id="themeBtn" title="테마">◐</button>
      <button class="icon-btn" id="sbBtn" title="사이드바">☰</button>
    </div>
  </aside>

  <div class="main-area">
    <div class="topbar">
      <div>
        <div class="page-title" id="pageTitle">Trader</div>
        <div class="page-sub" id="pageSub">Trader workspace</div>
      </div>
      <div class="topbar-right">
        <span class="topbar-user">{{if .User.Nickname}}{{.User.Nickname}}{{end}}</span>
        <a class="btn-logout" href="/logout">로그아웃</a>
      </div>
    </div>

    <div class="content">
      <div id="cmd-section">
        <form class="cmd-card" method="post" action="/command">
          <input type="hidden" id="activeTab" name="tab" value="{{if .ActiveTab}}{{.ActiveTab}}{{else}}trader{{end}}">
          <div class="cmd-label">명령</div>
          <textarea class="cmd-input" id="command" name="command" placeholder="OpenClaw에게 시킬 일을 입력하세요">{{.Command}}</textarea>
          <div class="cmd-actions">
            <button class="btn-send" type="submit">보내기 →</button>
          </div>
        </form>
        {{if .Error}}
          <div class="result-card is-error">{{.Error}}</div>
        {{end}}
        {{if .Reply}}
          <div class="result-card">{{.Reply}}</div>
        {{end}}
      </div>

      <section id="health-panel">
        <div class="health-grid">
          <div class="health-card">
            <div class="hc-emoji">⚡</div>
            <div class="hc-label">CPU</div>
            <div class="hc-value"><span id="cpu-pct">—</span><span class="hc-unit" id="cpu-unit"></span></div>
            <div class="hc-track"><div class="hc-fill" id="cpu-fill"></div></div>
            <div class="hc-detail" id="cpu-detail"></div>
          </div>
          <div class="health-card">
            <div class="hc-emoji">🧠</div>
            <div class="hc-label">Memory</div>
            <div class="hc-value"><span id="mem-pct">—</span><span class="hc-unit" id="mem-unit"></span></div>
            <div class="hc-track"><div class="hc-fill" id="mem-fill"></div></div>
            <div class="hc-detail" id="mem-detail"></div>
          </div>
          <div class="health-card">
            <div class="hc-emoji">💾</div>
            <div class="hc-label">Storage</div>
            <div class="hc-value"><span id="disk-pct">—</span><span class="hc-unit" id="disk-unit"></span></div>
            <div class="hc-track"><div class="hc-fill" id="disk-fill"></div></div>
            <div class="hc-detail" id="disk-detail"></div>
          </div>
        </div>
        <div class="health-ts" id="health-ts"></div>
      </section>
    </div>
  </div>
</div>
{{else}}
<div class="login-wrap">
  <div class="login-card">
    <div class="brand-badge">OC</div>
    <h1>OpenClaw</h1>
    <p class="login-desc">계속하려면 네이버 계정으로 로그인하세요.</p>
    {{if .AuthEnabled}}
      <a class="btn-naver" href="/login/naver">네이버로 로그인</a>
    {{else}}
      <p style="color:var(--danger);font-size:13px;margin-top:8px;">네이버 로그인이 설정되지 않았습니다.</p>
    {{end}}
  </div>
  <button class="icon-btn" id="themeBtn" style="border-color:var(--line);">◐</button>
</div>
{{end}}
<script>
(function () {
  var root = document.documentElement;
  var body = document.body;
  var tabs = {
    "trader":         { title: "Trader",           sub: "Trader workspace" },
    "builder":        { title: "Website Builder",  sub: "Website Builder workspace" },
    "asset-manager":  { title: "Asset Manager",    sub: "Asset Manager workspace" },
    "health":         { title: "OpenClaw Health",  sub: "Mac Mini 실시간 모니터링" }
  };
  var healthTimer = null;

  function setTheme(t) {
    root.setAttribute("data-theme", t);
    localStorage.setItem("oc-theme", t);
  }
  function setSidebar(col) {
    body.classList.toggle("sb-collapsed", col);
    localStorage.setItem("oc-sb", col ? "1" : "0");
  }

  function updateCard(pctId, unitId, fillId, detailId, pct, detail) {
    var pEl = document.getElementById(pctId);
    var uEl = document.getElementById(unitId);
    var fEl = document.getElementById(fillId);
    var dEl = document.getElementById(detailId);
    if (!pEl) return;
    var num = pct.toFixed(1);
    pEl.textContent = num;
    if (uEl) uEl.textContent = "%";
    fEl.style.width = Math.min(pct, 100).toFixed(1) + "%";
    fEl.className = "hc-fill" + (pct >= 90 ? " crit" : pct >= 75 ? " warn" : "");
    if (dEl) dEl.textContent = detail;
  }

  function fetchHealth() {
    fetch("/api/health")
      .then(function (r) { return r.json(); })
      .then(function (d) {
        updateCard("cpu-pct",  "cpu-unit",  "cpu-fill",  "cpu-detail",  d.cpu_percent, "");
        updateCard("mem-pct",  "mem-unit",  "mem-fill",  "mem-detail",  d.mem_percent,
          d.mem_used_gb.toFixed(1) + " GB / " + d.mem_total_gb.toFixed(1) + " GB");
        updateCard("disk-pct", "disk-unit", "disk-fill", "disk-detail", d.disk_percent,
          d.disk_used_gb.toFixed(1) + " GB / " + d.disk_total_gb.toFixed(1) + " GB");
        var ts = document.getElementById("health-ts");
        if (ts) ts.textContent = "마지막 업데이트  " + new Date().toLocaleTimeString("ko-KR");
      })
      .catch(function () {});
  }

  function setTab(tab) {
    if (!tabs[tab]) tab = "trader";
    var isHealth = tab === "health";
    var inp = document.getElementById("activeTab");
    var cmd = document.getElementById("cmd-section");
    var hp  = document.getElementById("health-panel");
    var pt  = document.getElementById("pageTitle");
    var ps  = document.getElementById("pageSub");
    if (inp) inp.value = tab;
    if (pt)  pt.textContent = tabs[tab].title;
    if (ps)  ps.textContent = tabs[tab].sub;
    if (cmd) cmd.style.display = isHealth ? "none" : "";
    if (hp)  hp.style.display  = isHealth ? ""     : "none";
    if (isHealth) {
      if (!healthTimer) { fetchHealth(); healthTimer = setInterval(fetchHealth, 2000); }
    } else {
      if (healthTimer) { clearInterval(healthTimer); healthTimer = null; }
    }
    document.querySelectorAll("[data-tab]").forEach(function (el) {
      el.classList.toggle("active", el.getAttribute("data-tab") === tab);
    });
    localStorage.setItem("oc-tab", tab);
  }

  var savedTheme = localStorage.getItem("oc-theme") || "light";
  setTheme(savedTheme === "dark" ? "dark" : "light");
  setSidebar(localStorage.getItem("oc-sb") === "1");
  var initInp = document.getElementById("activeTab");
  setTab((initInp && initInp.value) || localStorage.getItem("oc-tab") || "trader");

  document.querySelectorAll("[data-tab]").forEach(function (el) {
    el.addEventListener("click", function () { setTab(el.getAttribute("data-tab")); });
  });
  var tb = document.getElementById("themeBtn");
  if (tb) tb.addEventListener("click", function () {
    setTheme(root.getAttribute("data-theme") === "dark" ? "light" : "dark");
  });
  var sb = document.getElementById("sbBtn");
  if (sb) sb.addEventListener("click", function () {
    setSidebar(!body.classList.contains("sb-collapsed"));
  });
})();
</script>
</body>
</html>`))
