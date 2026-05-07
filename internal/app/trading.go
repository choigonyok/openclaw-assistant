package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TradingProxy forwards trading status requests to the openclaw-trading service.
//
// authToken (optional) is included as `Authorization: Bearer <token>` on all
// upstream requests. The trading service rejects requests without it when its
// own TRADING_AUTH_TOKEN env is set, so the two services share the same secret.
type TradingProxy struct {
	serviceURL string
	authToken  string
	http       *http.Client
}

func NewTradingProxy(serviceURL, authToken string) *TradingProxy {
	return &TradingProxy{
		serviceURL: serviceURL,
		authToken:  authToken,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *TradingProxy) Enabled() bool { return p.serviceURL != "" }

// handleTradingStatusAPI proxies GET /api/trading/status → trading service /status
func handleTradingStatusAPI(proxy *TradingProxy, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.CurrentUserOrDev(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if !proxy.Enabled() {
			writeJSON(w, http.StatusOK, map[string]string{
				"error": "퀀트봇 서비스가 설정되지 않았습니다. .env에 TRADING_SERVICE_URL을 설정하세요.",
			})
			return
		}
		body, status, err := proxy.get("/status")
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("trading service 연결 실패: %v", err)})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

// handleTradingControlAPI proxies POST /api/trading/control → trading service /control
func handleTradingControlAPI(proxy *TradingProxy, auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if _, ok := auth.CurrentUserOrDev(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if !proxy.Enabled() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "trading service not configured"})
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		body, status, err := proxy.post("/control", payload)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

func (p *TradingProxy) get(path string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, p.serviceURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	p.applyAuth(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func (p *TradingProxy) post(path string, payload any) ([]byte, int, error) {
	pr, pw := io.Pipe()
	go func() {
		_ = json.NewEncoder(pw).Encode(payload)
		_ = pw.Close()
	}()
	req, err := http.NewRequest(http.MethodPost, p.serviceURL+path, pr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	p.applyAuth(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func (p *TradingProxy) applyAuth(req *http.Request) {
	if p.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.authToken)
	}
}
