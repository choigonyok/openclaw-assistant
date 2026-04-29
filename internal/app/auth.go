package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	sessionCookieName = "openclaw_session"
	stateCookieName   = "openclaw_oauth_state"
)

type AuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	SessionKey   string
	AllowedIDs   []string
	DevMode      bool
}

type AuthService struct {
	clientID     string
	clientSecret string
	redirectURL  string
	sessionKey   []byte
	allowedIDs   map[string]struct{}
	devMode      bool
	httpClient   *http.Client
}

type User struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Email    string `json:"email,omitempty"`
}

func NewAuthService(cfg AuthConfig) *AuthService {
	allowedIDs := make(map[string]struct{}, len(cfg.AllowedIDs))
	for _, id := range cfg.AllowedIDs {
		allowedIDs[id] = struct{}{}
	}

	return &AuthService{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		redirectURL:  cfg.RedirectURL,
		sessionKey:   []byte(cfg.SessionKey),
		allowedIDs:   allowedIDs,
		devMode:      cfg.DevMode,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (a *AuthService) Enabled() bool {
	if a.devMode {
		return false
	}
	return a.clientID != "" && a.clientSecret != "" && a.redirectURL != ""
}

func (a *AuthService) CurrentUserOrDev(r *http.Request) (User, bool) {
	if a.devMode {
		return User{
			ID:       "dev",
			Name:     "Development User",
			Nickname: "DEV",
		}, true
	}
	return a.CurrentUser(r)
}

func (a *AuthService) LoginURL(state string) string {
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", a.clientID)
	values.Set("redirect_uri", a.redirectURL)
	values.Set("state", state)
	return "https://nid.naver.com/oauth2.0/authorize?" + values.Encode()
}

func (a *AuthService) ExchangeCode(ctx context.Context, code, state string) (string, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", a.clientID)
	values.Set("client_secret", a.clientSecret)
	values.Set("code", code)
	values.Set("state", state)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://nid.naver.com/oauth2.0/token?"+values.Encode(), nil)
	if err != nil {
		return "", err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("naver token endpoint returned %s", resp.Status)
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		if token.Error != "" {
			return "", fmt.Errorf("naver token error: %s", token.Error)
		}
		return "", fmt.Errorf("naver token response did not include access token")
	}
	return token.AccessToken, nil
}

func (a *AuthService) FetchUser(ctx context.Context, accessToken string) (User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openapi.naver.com/v1/nid/me", nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return User{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return User{}, fmt.Errorf("naver profile endpoint returned %s", resp.Status)
	}

	var profile profileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return User{}, err
	}
	if profile.Response.ID == "" {
		return User{}, fmt.Errorf("naver profile response did not include id")
	}

	user := User{
		ID:       profile.Response.ID,
		Name:     profile.Response.Name,
		Nickname: profile.Response.Nickname,
		Email:    profile.Response.Email,
	}
	if !a.userAllowed(user.ID) {
		return User{}, fmt.Errorf("naver user is not allowed")
	}
	return user, nil
}

func (a *AuthService) CurrentUser(r *http.Request) (User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return User{}, false
	}

	payload, ok := a.verify(cookie.Value)
	if !ok || payload.ExpiresAt.Before(time.Now()) {
		return User{}, false
	}
	return payload.User, true
}

func (a *AuthService) SetSession(w http.ResponseWriter, r *http.Request, user User) error {
	payload := sessionPayload{
		User:      user,
		ExpiresAt: time.Now().Add(24 * time.Hour * 30),
	}
	value, err := a.sign(payload)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  payload.ExpiresAt,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *AuthService) ClearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *AuthService) SetState(w http.ResponseWriter, r *http.Request, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *AuthService) PopState(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(stateCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
	return cookie.Value, true
}

func (a *AuthService) userAllowed(id string) bool {
	if len(a.allowedIDs) == 0 {
		return true
	}
	_, ok := a.allowedIDs[id]
	return ok
}

func (a *AuthService) sign(payload sessionPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func (a *AuthService) verify(value string) (sessionPayload, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return sessionPayload{}, false
	}

	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, expected) {
		return sessionPayload{}, false
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionPayload{}, false
	}

	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return sessionPayload{}, false
	}
	return payload, true
}

func newState() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}

type sessionPayload struct {
	User      User      `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

type profileResponse struct {
	Response struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
	} `json:"response"`
}
