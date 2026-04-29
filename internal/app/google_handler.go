package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func NewGoogleAPIHandler(google *GoogleService, auth *AuthService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/google/status", handleGoogleStatus(google, auth))
	mux.HandleFunc("/api/google/search-console/sites", handleGoogleSearchConsoleSites(google, auth))
	mux.HandleFunc("/api/google/search-console/site", handleGoogleAddSearchConsoleSite(google, auth))
	mux.HandleFunc("/api/google/search-console/sitemap", handleGoogleSubmitSitemap(google, auth))
	mux.HandleFunc("/api/google/search-console/url-inspection", handleGoogleInspectURL(google, auth))
	mux.HandleFunc("/api/google/search-console/search-analytics", handleGoogleSearchAnalytics(google, auth))
	mux.HandleFunc("/api/google/adsense/accounts", handleGoogleAdSenseAccounts(google, auth))
	mux.HandleFunc("/api/google/adsense/sites", handleGoogleAdSenseSites(google, auth))
	mux.HandleFunc("/api/google/adsense/report", handleGoogleAdSenseReport(google, auth))
	mux.HandleFunc("/api/google/analytics/run-report", handleGoogleAnalyticsRunReport(google, auth))
	return mux
}

func handleGoogleStatus(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIUser(w, r, auth) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": google.Enabled(),
			"endpoints": []string{
				"POST /api/google/search-console/sitemap",
				"GET /api/google/search-console/sites",
				"PUT /api/google/search-console/site",
				"POST /api/google/search-console/url-inspection",
				"POST /api/google/search-console/search-analytics",
				"GET /api/google/adsense/accounts",
				"GET /api/google/adsense/sites?account=accounts/{account}",
				"POST /api/google/adsense/report",
				"POST /api/google/analytics/run-report",
			},
		})
	}
}

func handleGoogleSearchConsoleSites(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodGet) {
			return
		}
		payload, err := google.SearchConsoleSites(r.Context())
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleAddSearchConsoleSite(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodPut) {
			return
		}
		var req struct {
			SiteURL string `json:"site_url"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.SiteURL == "" {
			writeAPIError(w, http.StatusBadRequest, "site_url is required")
			return
		}
		payload, err := google.SearchConsoleAddSite(r.Context(), req.SiteURL)
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleSubmitSitemap(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodPost) {
			return
		}
		var req struct {
			SiteURL    string `json:"site_url"`
			SitemapURL string `json:"sitemap_url"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.SiteURL == "" || req.SitemapURL == "" {
			writeAPIError(w, http.StatusBadRequest, "site_url and sitemap_url are required")
			return
		}
		payload, err := google.SearchConsoleSubmitSitemap(r.Context(), req.SiteURL, req.SitemapURL)
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleInspectURL(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodPost) {
			return
		}
		var req struct {
			SiteURL       string `json:"site_url"`
			InspectionURL string `json:"inspection_url"`
			LanguageCode  string `json:"language_code"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.SiteURL == "" || req.InspectionURL == "" {
			writeAPIError(w, http.StatusBadRequest, "site_url and inspection_url are required")
			return
		}
		payload, err := google.SearchConsoleInspectURL(r.Context(), inspectURLRequest{
			SiteURL:       req.SiteURL,
			InspectionURL: req.InspectionURL,
			LanguageCode:  req.LanguageCode,
		})
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleSearchAnalytics(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodPost) {
			return
		}
		var req struct {
			SiteURL string          `json:"site_url"`
			Query   json.RawMessage `json:"query"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.SiteURL == "" || len(req.Query) == 0 {
			writeAPIError(w, http.StatusBadRequest, "site_url and query are required")
			return
		}
		payload, err := google.SearchConsoleAnalytics(r.Context(), req.SiteURL, req.Query)
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleAdSenseAccounts(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodGet) {
			return
		}
		payload, err := google.AdSenseAccounts(r.Context())
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleAdSenseSites(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodGet) {
			return
		}
		account := r.URL.Query().Get("account")
		if account == "" {
			writeAPIError(w, http.StatusBadRequest, "account is required")
			return
		}
		payload, err := google.AdSenseSites(r.Context(), account)
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleAdSenseReport(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodPost) {
			return
		}
		var req adsenseReportRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.Account == "" || len(req.Metrics) == 0 {
			writeAPIError(w, http.StatusBadRequest, "account and metrics are required")
			return
		}
		payload, err := google.AdSenseReport(r.Context(), req)
		writeGoogleResult(w, payload, err)
	}
}

func handleGoogleAnalyticsRunReport(google *GoogleService, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAPIMethod(w, r, auth, http.MethodPost) {
			return
		}
		var req struct {
			PropertyID string          `json:"property_id"`
			Query      json.RawMessage `json:"query"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.PropertyID == "" || len(req.Query) == 0 {
			writeAPIError(w, http.StatusBadRequest, "property_id and query are required")
			return
		}
		payload, err := google.AnalyticsRunReport(r.Context(), req.PropertyID, req.Query)
		writeGoogleResult(w, payload, err)
	}
}

func requireAPIMethod(w http.ResponseWriter, r *http.Request, auth *AuthService, method string) bool {
	if r.Method != method {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	return requireAPIUser(w, r, auth)
}

func requireAPIUser(w http.ResponseWriter, r *http.Request, auth *AuthService) bool {
	if _, ok := auth.CurrentUserOrDev(r); !ok {
		writeAPIError(w, http.StatusUnauthorized, "login required")
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(v); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return false
	}
	return true
}

func writeGoogleResult(w http.ResponseWriter, payload json.RawMessage, err error) {
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "not configured") {
			status = http.StatusServiceUnavailable
		}
		writeAPIError(w, status, err.Error())
		return
	}
	writeRawJSON(w, http.StatusOK, payload)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
		"time":  time.Now().UTC().Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRawJSON(w http.ResponseWriter, status int, payload json.RawMessage) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
}
