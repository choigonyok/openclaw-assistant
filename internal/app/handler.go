package app

import (
	"context"
	"html/template"
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
	case "trader", "builder", "asset-manager":
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
	if err := pageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ko">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OpenClaw Assistant</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --sidebar: #111827;
      --sidebar-text: #eef2f7;
      --sidebar-muted: #aab4c3;
      --panel: #ffffff;
      --text: #1c2430;
      --muted: #657080;
      --line: #d8dee7;
      --accent: #087f8c;
      --accent-strong: #06636d;
      --danger: #b42318;
      --button-text: #ffffff;
    }
    [data-theme="dark"] {
      color-scheme: dark;
      --bg: #15181d;
      --sidebar: #0b0f14;
      --sidebar-text: #f3f6f9;
      --sidebar-muted: #8b97a8;
      --panel: #20252c;
      --text: #edf1f5;
      --muted: #a2adbb;
      --line: #38414d;
      --accent: #2ea9b6;
      --accent-strong: #45bdc8;
      --danger: #ff8d83;
      --button-text: #081116;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }

    /* ── 로그인 페이지 ── */
    .login-page {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      gap: 24px;
    }
    .login-card {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 40px 48px;
      text-align: center;
      width: min(360px, calc(100% - 32px));
    }
    .login-card .brand-mark {
      display: inline-grid;
      place-items: center;
      width: 48px;
      height: 48px;
      border-radius: 10px;
      background: var(--accent);
      color: var(--button-text);
      font-weight: 900;
      font-size: 18px;
      margin-bottom: 16px;
    }
    .login-card h1 {
      margin: 0 0 4px;
      font-size: 22px;
    }
    .login-card p {
      margin: 0 0 28px;
      color: var(--muted);
      font-size: 14px;
    }
    .login-button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 44px;
      width: 100%;
      border-radius: 8px;
      background: #03c75a;
      color: #ffffff;
      font-weight: 800;
      font-size: 15px;
      padding: 0 16px;
      text-decoration: none;
    }
    .login-button:hover { background: #02b351; }
    .login-notice {
      color: var(--muted);
      font-size: 13px;
    }

    /* ── 패널 ── */
    .shell {
      display: grid;
      grid-template-columns: 260px minmax(0, 1fr);
      min-height: 100vh;
      transition: grid-template-columns .18s ease;
    }
    body.sidebar-collapsed .shell {
      grid-template-columns: 72px minmax(0, 1fr);
    }
    .sidebar {
      position: sticky;
      top: 0;
      height: 100vh;
      overflow: hidden;
      background: var(--sidebar);
      color: var(--sidebar-text);
      border-right: 1px solid rgba(255, 255, 255, .08);
    }
    .sidebar-inner {
      display: flex;
      flex-direction: column;
      height: 100%;
      padding: 16px 12px;
      gap: 18px;
    }
    .brand-row, .sidebar-footer {
      display: flex;
      align-items: center;
      gap: 10px;
    }
    .brand-mark {
      display: inline-grid;
      place-items: center;
      width: 40px;
      height: 40px;
      flex: 0 0 40px;
      border-radius: 8px;
      background: var(--accent);
      color: var(--button-text);
      font-weight: 900;
    }
    .brand-text { min-width: 0; }
    .brand-title {
      font-size: 16px;
      font-weight: 800;
      line-height: 1.2;
      white-space: nowrap;
    }
    .brand-subtitle {
      color: var(--sidebar-muted);
      font-size: 12px;
      margin-top: 2px;
      white-space: nowrap;
    }
    .icon-button {
      display: inline-grid;
      place-items: center;
      width: 40px;
      height: 40px;
      flex: 0 0 40px;
      border: 1px solid rgba(255, 255, 255, .14);
      border-radius: 8px;
      background: transparent;
      color: inherit;
      cursor: pointer;
      font-size: 18px;
      padding: 0;
    }
    .icon-button:hover { background: rgba(255, 255, 255, .08); }
    .nav-tabs { display: grid; gap: 8px; }
    .tab-button {
      display: flex;
      align-items: center;
      gap: 10px;
      min-height: 44px;
      width: 100%;
      border: 0;
      border-radius: 8px;
      background: transparent;
      color: var(--sidebar-muted);
      cursor: pointer;
      font: inherit;
      font-weight: 700;
      padding: 0 12px;
      text-align: left;
    }
    .tab-button:hover, .tab-button.is-active {
      background: rgba(255, 255, 255, .1);
      color: var(--sidebar-text);
    }
    .tab-icon {
      display: inline-grid;
      place-items: center;
      width: 24px;
      flex: 0 0 24px;
      font-size: 18px;
    }
    .tab-label {
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .sidebar-footer {
      margin-top: auto;
      justify-content: space-between;
    }
    body.sidebar-collapsed .brand-text,
    body.sidebar-collapsed .tab-label,
    body.sidebar-collapsed .sidebar-footer .status-label { display: none; }
    body.sidebar-collapsed .brand-row,
    body.sidebar-collapsed .sidebar-footer { justify-content: center; }
    main {
      width: min(980px, calc(100% - 32px));
      margin: 0 auto;
      padding: 40px 0;
    }
    header {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 20px;
    }
    h1 { margin: 0; font-size: 28px; line-height: 1.2; }
    .status { color: var(--muted); font-size: 14px; white-space: nowrap; }
    form, .result {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 18px;
    }
    label { display: block; margin-bottom: 10px; font-weight: 700; }
    textarea {
      width: 100%;
      min-height: 180px;
      resize: vertical;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 14px;
      background: var(--panel);
      color: var(--text);
      font: inherit;
      line-height: 1.5;
    }
    textarea:focus {
      outline: 3px solid rgba(8, 127, 140, .16);
      border-color: var(--accent);
    }
    .actions { display: flex; justify-content: flex-end; margin-top: 12px; }
    button {
      border: 0;
      border-radius: 6px;
      background: var(--accent);
      color: var(--button-text);
      cursor: pointer;
      font: inherit;
      font-weight: 700;
      padding: 10px 16px;
    }
    button:hover { background: var(--accent-strong); }
    a { color: var(--accent); }
    .workspace-meta { color: var(--muted); font-size: 14px; margin: -8px 0 18px; }
    .result { margin-top: 18px; white-space: pre-wrap; line-height: 1.55; }
    .error { border-color: rgba(180, 35, 24, .28); color: var(--danger); }
    @media (max-width: 640px) {
      main { width: min(100% - 24px, 920px); padding: 24px 0; }
      header { display: block; }
      h1 { font-size: 24px; margin-bottom: 6px; }
      .status { white-space: normal; }
    }
    @media (max-width: 760px) {
      .shell { grid-template-columns: 72px minmax(0, 1fr); }
      body:not(.sidebar-expanded) .brand-text,
      body:not(.sidebar-expanded) .tab-label,
      body:not(.sidebar-expanded) .sidebar-footer .status-label { display: none; }
      body.sidebar-expanded .shell { grid-template-columns: 240px minmax(0, 1fr); }
      main { width: min(100% - 24px, 920px); }
    }
  </style>
</head>
<body>
{{if .User.ID}}
  <div class="shell">
    <aside class="sidebar" aria-label="작업 메뉴">
      <div class="sidebar-inner">
        <div class="brand-row">
          <span class="brand-mark">OC</span>
          <div class="brand-text">
            <div class="brand-title">OpenClaw</div>
            <div class="brand-subtitle">Assistant Console</div>
          </div>
        </div>

        <nav class="nav-tabs" aria-label="작업 탭">
          <button class="tab-button" type="button" data-tab-target="trader" aria-pressed="true">
            <span class="tab-icon" aria-hidden="true">◇</span>
            <span class="tab-label">Trader</span>
          </button>
          <button class="tab-button" type="button" data-tab-target="builder" aria-pressed="false">
            <span class="tab-icon" aria-hidden="true">▣</span>
            <span class="tab-label">Website Builder</span>
          </button>
          <button class="tab-button" type="button" data-tab-target="asset-manager" aria-pressed="false">
            <span class="tab-icon" aria-hidden="true">▤</span>
            <span class="tab-label">Asset Manager</span>
          </button>
        </nav>

        <div class="sidebar-footer">
          <span class="status-label">패널</span>
          <button class="icon-button" type="button" id="sidebarToggle" aria-label="사이드 패널 열고 닫기">☰</button>
        </div>
      </div>
    </aside>

    <main>
      <header>
        <h1 id="pageTitle">OpenClaw Assistant</h1>
        <div class="status">
          {{if .User.Nickname}}{{.User.Nickname}}{{else}}{{.User.ID}}{{end}} · {{.User.ID}} ·
          <button class="icon-button" type="button" id="themeToggle" aria-label="다크 모드 라이트 모드 전환">◐</button>
          <a href="/logout">로그아웃</a>
        </div>
      </header>

      <p class="workspace-meta" id="workspaceMeta">Trader workspace</p>
      <form method="post" action="/command">
        <input type="hidden" id="activeTab" name="tab" value="{{if .ActiveTab}}{{.ActiveTab}}{{else}}trader{{end}}">
        <label for="command">명령</label>
        <textarea id="command" name="command" placeholder="OpenClaw에게 시킬 일을 입력하세요">{{.Command}}</textarea>
        <div class="actions">
          <button type="submit">보내기</button>
        </div>
      </form>

      {{if .Error}}
        <section class="result error">{{.Error}}</section>
      {{end}}
      {{if .Reply}}
        <section class="result">{{.Reply}}</section>
      {{end}}
    </main>
  </div>
{{else}}
  <div class="login-page">
    <div class="login-card">
      <div class="brand-mark">OC</div>
      <h1>OpenClaw Assistant</h1>
      <p>계속하려면 네이버 계정으로 로그인하세요.</p>
      {{if .AuthEnabled}}
        <a class="login-button" href="/login/naver">네이버로 로그인</a>
      {{else}}
        <p style="color:var(--danger);font-size:13px;">네이버 로그인이 설정되지 않았습니다.<br>NAVER_CLIENT_ID와 NAVER_CLIENT_SECRET을 설정하세요.</p>
      {{end}}
    </div>
    <span class="login-notice">
      <button type="button" id="themeToggle" style="border:1px solid var(--line);background:var(--panel);color:var(--text);border-radius:6px;padding:6px 10px;cursor:pointer;">◐</button>
    </span>
  </div>
{{end}}
  <script>
    (function () {
      var root = document.documentElement;
      var body = document.body;
      var themeToggle = document.getElementById("themeToggle");
      var sidebarToggle = document.getElementById("sidebarToggle");
      var activeTabInput = document.getElementById("activeTab");
      var pageTitle = document.getElementById("pageTitle");
      var workspaceMeta = document.getElementById("workspaceMeta");
      var labels = {
        trader: { title: "Trader", meta: "Trader workspace" },
        builder: { title: "Website Builder", meta: "Website Builder workspace" },
        "asset-manager": { title: "Asset Manager", meta: "Asset Manager workspace" }
      };

      function setTheme(theme) {
        root.setAttribute("data-theme", theme);
        localStorage.setItem("openclaw-theme", theme);
      }

      function setSidebar(collapsed) {
        body.classList.toggle("sidebar-collapsed", collapsed);
        body.classList.toggle("sidebar-expanded", !collapsed);
        localStorage.setItem("openclaw-sidebar-collapsed", collapsed ? "true" : "false");
      }

      function setTab(tab) {
        if (!labels[tab]) tab = "trader";
        if (activeTabInput) activeTabInput.value = tab;
        if (pageTitle) pageTitle.textContent = labels[tab].title;
        if (workspaceMeta) workspaceMeta.textContent = labels[tab].meta;
        document.querySelectorAll("[data-tab-target]").forEach(function (button) {
          var active = button.getAttribute("data-tab-target") === tab;
          button.classList.toggle("is-active", active);
          button.setAttribute("aria-pressed", active ? "true" : "false");
        });
        localStorage.setItem("openclaw-active-tab", tab);
      }

      var savedTheme = localStorage.getItem("openclaw-theme") || "light";
      setTheme(savedTheme === "dark" ? "dark" : "light");
      setSidebar(localStorage.getItem("openclaw-sidebar-collapsed") === "true");
      setTab((activeTabInput && activeTabInput.value) || localStorage.getItem("openclaw-active-tab") || "trader");

      if (themeToggle) {
        themeToggle.addEventListener("click", function () {
          setTheme(root.getAttribute("data-theme") === "dark" ? "light" : "dark");
        });
      }
      if (sidebarToggle) {
        sidebarToggle.addEventListener("click", function () {
          setSidebar(!body.classList.contains("sidebar-collapsed"));
        });
      }
      document.querySelectorAll("[data-tab-target]").forEach(function (button) {
        button.addEventListener("click", function () {
          setTab(button.getAttribute("data-tab-target"));
        });
      });
    })();
  </script>
</body>
</html>`))
