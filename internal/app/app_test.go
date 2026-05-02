package app

import (
	"bytes"
	"context"
	"encoding/json"
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

	req := httptest.NewRequest(http.MethodPost, "/api/command", strings.NewReader(`{"tab":"builder","command":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
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
	if !strings.Contains(rec.Body.String(), `"reply":"done"`) {
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

	req := httptest.NewRequest(http.MethodPost, "/api/command", strings.NewReader(`{"tab":"trader","command":"buy signal"}`))
	req.Header.Set("Content-Type", "application/json")
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

	req := httptest.NewRequest(http.MethodPost, "/api/command", strings.NewReader(`{"tab":"asset-manager","command":"list assets"}`))
	req.Header.Set("Content-Type", "application/json")
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

func TestNaverLoginUsesFrontendURLForRedirectURI(t *testing.T) {
	auth := NewAuthService(AuthConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost:8080/auth/naver/callback",
		SessionKey:   "test-secret",
	})
	handler := NewHandler(&fakeSender{}, auth, NewGoogleService(GoogleConfig{}), NewKISClient("", "", "", "", false), NewUpbitClient("", ""), nil, nil, apiHandlerConfig{
		FrontendURL: "https://agent.choigonyok.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/login/naver", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "redirect_uri=https%3A%2F%2Fagent.choigonyok.com%2Fauth%2Fnaver%2Fcallback") {
		t.Fatalf("Location does not include frontend callback redirect_uri: %s", location)
	}
}

func TestGoogleStatusInDevMode(t *testing.T) {
	handler := NewHandler(&fakeSender{}, NewAuthService(AuthConfig{SessionKey: "test-secret", DevMode: true}), NewGoogleService(GoogleConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		RefreshToken: "refresh",
	}), NewKISClient("", "", "", "", false), NewUpbitClient("", ""), nil, nil, apiHandlerConfig{})

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

func TestValidThinkVotePath(t *testing.T) {
	valid := []string{
		"philosophy/ethics/trolley-problem",
		"ethics/trolley-problem",
		"relationship/love/question_01",
		"finance/money/choice-2",
	}
	for _, path := range valid {
		if !validThinkVotePath(path) {
			t.Fatalf("validThinkVotePath(%q) = false, want true", path)
		}
	}

	invalid := []string{
		"",
		"ethics",
		"ethics/",
		"/trolley-problem",
		"ethics/../secrets",
		"ethics/trolley.problem",
		"philosophy/ethics/trolley-problem/extra",
	}
	for _, path := range invalid {
		if validThinkVotePath(path) {
			t.Fatalf("validThinkVotePath(%q) = true, want false", path)
		}
	}
}

func TestThinkVotePostCreatesMissingVoteObject(t *testing.T) {
	store := &fakeThinkStore{objects: map[string][]byte{}}
	handler := handleThinkVotes(store)

	req := httptest.NewRequest(http.MethodPost, "/api/think/votes/philosophy/ethics/trolley-problem", strings.NewReader(`{"option":"b"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	key := "think/votes/philosophy/ethics/trolley-problem.json"
	raw, ok := store.objects[key]
	if !ok {
		t.Fatalf("vote object %q was not created", key)
	}

	var got votes
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("created vote object is invalid json: %v", err)
	}
	if got.A != 0 || got.B != 1 {
		t.Fatalf("votes = %+v, want {A:0 B:1}", got)
	}
}

func TestThinkDilemmasLegacyPathResolvesTopic(t *testing.T) {
	store := &fakeThinkStore{objects: map[string][]byte{
		"think/categories.json":                          []byte(`[{"id":"first-move","topic":"love"}]`),
		"think/dilemmas/love/first-move.json":            []byte(`[{"id":"confess-first"}]`),
		"think/votes/love/first-move/confess-first.json": []byte(`{"a":2,"b":1}`),
	}}
	handler := newThinkHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/think/dilemmas/first-move", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "confess-first") {
		t.Fatalf("body = %s, want dilemma payload", rec.Body.String())
	}
}

func TestThinkVoteLegacyPathResolvesTopic(t *testing.T) {
	store := &fakeThinkStore{objects: map[string][]byte{
		"think/categories.json":                          []byte(`[{"id":"first-move","topic":"love"}]`),
		"think/votes/love/first-move/confess-first.json": []byte(`{"a":2,"b":1}`),
	}}
	handler := handleThinkVotes(store)

	req := httptest.NewRequest(http.MethodGet, "/api/think/votes/first-move/confess-first", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got votes
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is invalid json: %v", err)
	}
	if got.A != 2 || got.B != 1 {
		t.Fatalf("votes = %+v, want {A:2 B:1}", got)
	}
}

func TestChecklistIndexReadsR2Object(t *testing.T) {
	store := &fakeThinkStore{objects: map[string][]byte{
		"checklist/index.json": []byte(`{"version":1,"templates":[{"id":"moving-checklist"}]}`),
	}}
	handler := newChecklistHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/checklist/index", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "moving-checklist") {
		t.Fatalf("body = %s, want checklist index payload", rec.Body.String())
	}
}

func TestChecklistTemplateReadsR2Object(t *testing.T) {
	store := &fakeThinkStore{objects: map[string][]byte{
		"checklist/templates/monthly-villa-move-in-checklist.json": []byte(`{"id":"monthly-villa-move-in-checklist","title":"월세 빌라 입주 체크리스트"}`),
	}}
	handler := newChecklistHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/checklist/templates/monthly-villa-move-in-checklist", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "월세 빌라 입주 체크리스트") {
		t.Fatalf("body = %s, want checklist template payload", rec.Body.String())
	}
}

func TestChecklistTemplateRejectsInvalidID(t *testing.T) {
	store := &fakeThinkStore{objects: map[string][]byte{}}
	handler := newChecklistHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/checklist/templates/bad.id", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

type fakeSender struct {
	reply   string
	err     error
	command string
}

type fakeThinkStore struct {
	objects map[string][]byte
}

func (f *fakeThinkStore) Enabled() bool {
	return true
}

func (f *fakeThinkStore) GetJSON(_ context.Context, key string, v any) error {
	data, ok := f.objects[key]
	if !ok {
		return os.ErrNotExist
	}
	return json.Unmarshal(data, v)
}

func (f *fakeThinkStore) PutJSON(_ context.Context, key string, v any) error {
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.objects[key] = data
	return nil
}

func newTestHandler(client commandSender, auth *AuthService) http.Handler {
	return NewHandler(client, auth, NewGoogleService(GoogleConfig{}), NewKISClient("", "", "", "", false), NewUpbitClient("", ""), nil, nil, apiHandlerConfig{})
}

func (f *fakeSender) SendCommand(_ context.Context, command string) (string, error) {
	f.command = command
	return f.reply, f.err
}
