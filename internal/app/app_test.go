package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer

	if err := Run(context.Background(), []string{"version"}, &out); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := "openclaw-assistant dev\n"
	if got := out.String(); got != want {
		t.Fatalf("Run output = %q, want %q", got, want)
	}
}

func TestHandlerSendsCommand(t *testing.T) {
	client := &fakeSender{reply: "done"}
	auth := NewAuthService(AuthConfig{
		SessionKey: "test-secret",
	})
	handler := newTestHandler(client, auth)

	req := httptest.NewRequest(http.MethodPost, "/command", strings.NewReader("tab=builder&command=hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sessionValue, err := auth.sign(sessionPayload{
		User:      User{ID: "naver-user"},
		ExpiresAt: time.Now().AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("sign returned error: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionValue})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "done") {
		t.Fatalf("response body does not include reply: %q", rec.Body.String())
	}
	if got, want := client.command, "[Website Builder]\nhello"; got != want {
		t.Fatalf("sent command = %q, want %q", got, want)
	}
}

func TestHandlerAllowsCommandInDevMode(t *testing.T) {
	client := &fakeSender{reply: "done"}
	auth := NewAuthService(AuthConfig{
		SessionKey: "test-secret",
		DevMode:    true,
	})
	handler := newTestHandler(client, auth)

	req := httptest.NewRequest(http.MethodPost, "/command", strings.NewReader("tab=trader&command=buy signal"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := client.command, "[Trader]\nbuy signal"; got != want {
		t.Fatalf("sent command = %q, want %q", got, want)
	}
}

func TestHandlerSendsAssetManagerCommand(t *testing.T) {
	client := &fakeSender{reply: "done"}
	auth := NewAuthService(AuthConfig{
		SessionKey: "test-secret",
		DevMode:    true,
	})
	handler := newTestHandler(client, auth)

	req := httptest.NewRequest(http.MethodPost, "/command", strings.NewReader("tab=asset-manager&command=list assets"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := client.command, "[Asset Manager]\nlist assets"; got != want {
		t.Fatalf("sent command = %q, want %q", got, want)
	}
}

func TestGoogleStatusRequiresLogin(t *testing.T) {
	handler := newTestHandler(&fakeSender{}, NewAuthService(AuthConfig{SessionKey: "test-secret"}))

	req := httptest.NewRequest(http.MethodGet, "/api/google/status", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGoogleStatusInDevMode(t *testing.T) {
	handler := NewHandler(&fakeSender{}, NewAuthService(AuthConfig{SessionKey: "test-secret", DevMode: true}), NewGoogleService(GoogleConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		RefreshToken: "refresh",
	}), NewKISClient("", "", "", "", false), NewUpbitClient("", ""))

	req := httptest.NewRequest(http.MethodGet, "/api/google/status", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("response body does not show enabled google api: %q", rec.Body.String())
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := strings.Join([]string{
		"# local config",
		"OPENCLAW_TEST_EXISTING=from-file",
		`OPENCLAW_TEST_NEW="quoted-secret"`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	t.Setenv("OPENCLAW_TEST_EXISTING", "from-env")
	os.Unsetenv("OPENCLAW_TEST_NEW")
	t.Cleanup(func() {
		os.Unsetenv("OPENCLAW_TEST_NEW")
	})

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv returned error: %v", err)
	}

	if got, want := os.Getenv("OPENCLAW_TEST_EXISTING"), "from-env"; got != want {
		t.Fatalf("OPENCLAW_TEST_EXISTING = %q, want %q", got, want)
	}
	if got, want := os.Getenv("OPENCLAW_TEST_NEW"), "quoted-secret"; got != want {
		t.Fatalf("OPENCLAW_TEST_NEW = %q, want %q", got, want)
	}
}

func TestAddGoogleDate(t *testing.T) {
	values := url.Values{}
	if err := addGoogleDate(values, "startDate", "2026-04-29"); err != nil {
		t.Fatalf("addGoogleDate returned error: %v", err)
	}

	if got, want := values.Get("startDate.year"), "2026"; got != want {
		t.Fatalf("startDate.year = %q, want %q", got, want)
	}
	if got, want := values.Get("startDate.month"), "4"; got != want {
		t.Fatalf("startDate.month = %q, want %q", got, want)
	}
	if got, want := values.Get("startDate.day"), "29"; got != want {
		t.Fatalf("startDate.day = %q, want %q", got, want)
	}
}

type fakeSender struct {
	reply   string
	err     error
	command string
}

func newTestHandler(client commandSender, auth *AuthService) http.Handler {
	return NewHandler(client, auth, NewGoogleService(GoogleConfig{}), NewKISClient("", "", "", "", false), NewUpbitClient("", ""))
}

func (f *fakeSender) SendCommand(_ context.Context, command string) (string, error) {
	f.command = command
	return f.reply, f.err
}
