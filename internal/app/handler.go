package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type commandSender interface {
	SendCommand(ctx context.Context, command string) (string, error)
}

type apiHandlerConfig struct {
	FrontendURL string
	CORSOrigins []string
}

func NewHandler(client commandSender, auth *AuthService, google *GoogleService, kis *KISClient, upbit *UpbitClient, r2 *R2Client, cf *CloudflareClient, cfg apiHandlerConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleAPIHome(cfg.FrontendURL))
	mux.HandleFunc("/api/session", handleSessionAPI(auth))
	mux.HandleFunc("/api/command", handleCommandAPI(client, auth))
	mux.HandleFunc("/command", handleCommandAPI(client, auth))
	mux.Handle("/api/google/", NewGoogleAPIHandler(google, auth))
	mux.HandleFunc("/login/naver", handleNaverLogin(auth, cfg.FrontendURL))
	mux.HandleFunc("/auth/naver/callback", handleNaverCallback(auth, cfg.FrontendURL))
	mux.HandleFunc("/logout", handleLogout(auth, cfg.FrontendURL))
	mux.HandleFunc("/api/health", handleHealthAPI(auth))
	mux.HandleFunc("/api/assets", handleAssetsAPI(kis, auth))
	mux.HandleFunc("/api/crypto", handleCryptoAPI(upbit, auth))
	mux.HandleFunc("/api/sites", handleSitesAPI(cf, auth))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/api/think/", NewThinkHandler(r2))
	return withRecover(withCORS(mux, cfg.CORSOrigins))
}

func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("[panic] %s %s: %v", r.Method, r.URL.Path, recovered)
				writeAPIError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type SiteInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CFStatus       string `json:"cf_status"`
	Plan           string `json:"plan"`
	Health         string `json:"health"`
	HTTPStatus     int    `json:"http_status"`
	ResponseMs     int64  `json:"response_ms"`
	RequestsToday  int64  `json:"requests_today"`
	PageViewsToday int64  `json:"page_views_today"`
	UniquesToday   int64  `json:"uniques_today"`
	BandwidthToday int64  `json:"bandwidth_today"`
	Requests7d     int64  `json:"requests_7d"`
	PageViews7d    int64  `json:"page_views_7d"`
	Uniques7d      int64  `json:"uniques_7d"`
	Bandwidth7d    int64  `json:"bandwidth_7d"`
	StatsError     string `json:"stats_error,omitempty"`
	IsSubdomain    bool   `json:"is_subdomain,omitempty"`
	ParentZone     string `json:"parent_zone,omitempty"`
	DNSType        string `json:"dns_type,omitempty"`
	DNSContent     string `json:"dns_content,omitempty"`
	DNSError       string `json:"dns_error,omitempty"`
}

func handleSitesAPI(cf *CloudflareClient, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.CurrentUserOrDev(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if cf == nil || !cf.Enabled() {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"error": "Cloudflare API가 설정되지 않았습니다. .env에 CF_API_TOKEN을 설정하세요.",
				"sites": []SiteInfo{},
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()

		zones, err := cf.ListZones(ctx)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"error": fmt.Sprintf("Cloudflare 영역 조회 실패: %s", err.Error()),
				"sites": []SiteInfo{},
			})
			return
		}

		// 1단계: 각 zone의 DNS 레코드를 병렬로 조회하여 서브도메인 목록 수집
		type zoneWithSubs struct {
			zone     CFZone
			subs     []CFDNSRecord
			dnsError string
		}
		zwsCh := make(chan zoneWithSubs, len(zones))
		for _, zone := range zones {
			go func(z CFZone) {
				records, err := cf.ListDNSRecords(ctx, z.ID)
				var dnsErr string
				if err != nil {
					dnsErr = err.Error()
				}
				var subs []CFDNSRecord
				for _, rec := range records {
					// 루트 도메인 자체(@) 레코드 제외, A/CNAME 서브도메인만 포함
					if rec.Name != z.Name {
						subs = append(subs, rec)
					}
				}
				zwsCh <- zoneWithSubs{zone: z, subs: subs, dnsError: dnsErr}
			}(zone)
		}

		zoneMap := make(map[string]zoneWithSubs, len(zones))
		for range zones {
			zws := <-zwsCh
			zoneMap[zws.zone.ID] = zws
		}

		// 2단계: 전체 작업 목록 구성 (zone + 서브도메인)
		type siteTask struct {
			isSubdomain bool
			zone        CFZone
			rec         CFDNSRecord // 서브도메인인 경우
			dnsError    string      // zone DNS 조회 실패 메시지 (루트 도메인에만 사용)
		}
		var tasks []siteTask
		for _, z := range zones {
			zws := zoneMap[z.ID]
			tasks = append(tasks, siteTask{zone: z, dnsError: zws.dnsError})
			for _, rec := range zws.subs {
				tasks = append(tasks, siteTask{isSubdomain: true, zone: z, rec: rec})
			}
		}

		// 3단계: 모든 사이트에 대해 헬스체크 + 통계 병렬 조회
		type indexed struct {
			site SiteInfo
			idx  int
		}
		ch := make(chan indexed, len(tasks))
		for i, task := range tasks {
			go func(idx int, t siteTask) {
				var s SiteInfo
				if !t.isSubdomain {
					s = SiteInfo{
						ID:       t.zone.ID,
						Name:     t.zone.Name,
						CFStatus: t.zone.Status,
						Plan:     t.zone.Plan.Name,
						DNSError: t.dnsError,
					}
					health := CheckSiteHealth(t.zone.Name)
					s.Health = health.Status
					s.HTTPStatus = health.HTTPStatus
					s.ResponseMs = health.ResponseMs

					stats, err := cf.GetZoneStats(ctx, t.zone.ID)
					if err != nil {
						s.StatsError = err.Error()
					} else if stats != nil {
						s.RequestsToday = stats.RequestsToday
						s.PageViewsToday = stats.PageViewsToday
						s.UniquesToday = stats.UniquesToday
						s.BandwidthToday = stats.BandwidthToday
						s.Requests7d = stats.Requests7d
						s.PageViews7d = stats.PageViews7d
						s.Uniques7d = stats.Uniques7d
						s.Bandwidth7d = stats.Bandwidth7d
					}
				} else {
					s = SiteInfo{
						ID:          t.rec.ID,
						Name:        t.rec.Name,
						CFStatus:    t.zone.Status,
						Plan:        t.zone.Plan.Name,
						IsSubdomain: true,
						ParentZone:  t.zone.Name,
						DNSType:     t.rec.Type,
						DNSContent:  t.rec.Content,
					}
					health := CheckSiteHealth(t.rec.Name)
					s.Health = health.Status
					s.HTTPStatus = health.HTTPStatus
					s.ResponseMs = health.ResponseMs
				}
				ch <- indexed{site: s, idx: idx}
			}(i, task)
		}

		sites := make([]SiteInfo, len(tasks))
		for range tasks {
			res := <-ch
			sites[res.idx] = res.site
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"sites": sites})
	}
}

func handleAPIHome(frontendURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if frontendURL != "" {
			http.Redirect(w, r, frontendURL, http.StatusTemporaryRedirect)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"service": Name})
	}
}

func handleSessionAPI(auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.CurrentUserOrDev(r)
		response := map[string]interface{}{
			"authenticated": ok,
			"auth_enabled":  auth.Enabled(),
			"login_url":     "/login/naver",
			"logout_url":    "/logout",
		}
		if ok {
			response["user"] = user
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func handleCommandAPI(client commandSender, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if _, ok := auth.CurrentUserOrDev(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		var payload struct {
			Tab     string `json:"tab"`
			Command string `json:"command"`
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
				return
			}
		} else {
			payload.Tab = r.FormValue("tab")
			payload.Command = r.FormValue("command")
		}

		command := strings.TrimSpace(payload.Command)
		if command == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "명령을 입력해주세요."})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
		defer cancel()

		reply, err := client.SendCommand(ctx, commandForTab(normalizeTab(payload.Tab), command))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"reply": reply})
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

func handleNaverLogin(auth *AuthService, frontendURL string) http.HandlerFunc {
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
		http.Redirect(w, r, auth.LoginURLForRedirect(state, callbackURLForRequest(r, frontendURL)), http.StatusFound)
	}
}

func handleNaverCallback(auth *AuthService, frontendURL string) http.HandlerFunc {
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
		redirectURL := frontendURL
		if redirectURL == "" {
			redirectURL = "/"
		}
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func handleLogout(auth *AuthService, frontendURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth.ClearSession(w, r)
		redirectURL := frontendURL
		if redirectURL == "" {
			redirectURL = "/"
		}
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func callbackURLForRequest(r *http.Request, frontendURL string) string {
	if frontendURL != "" {
		return strings.TrimRight(frontendURL, "/") + "/auth/naver/callback"
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host + "/auth/naver/callback"
}

func handleCryptoAPI(upbit *UpbitClient, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.CurrentUserOrDev(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if !upbit.Enabled() {
			writeJSON(w, http.StatusOK, map[string]string{
				"error": "업비트 API가 설정되지 않았습니다. .env에 UPBIT_ACCESS_KEY, UPBIT_SECRET_KEY를 설정하세요.",
			})
			return
		}
		result, err := upbit.GetAssets()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleAssetsAPI(kis *KISClient, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.CurrentUserOrDev(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if !kis.Enabled() {
			writeJSON(w, http.StatusOK, map[string]string{
				"error": "나무증권 API가 설정되지 않았습니다. .env에 KIS_APP_KEY, KIS_APP_SECRET, KIS_ACCOUNT_NO를 설정하세요.",
			})
			return
		}
		result, err := kis.GetBalance()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func withCORS(next http.Handler, origins []string) http.Handler {
	allowed := map[string]struct{}{}
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Vary", "Origin")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
