package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
}

type GoogleService struct {
	clientID     string
	clientSecret string
	refreshToken string
	httpClient   *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewGoogleService(cfg GoogleConfig) *GoogleService {
	return &GoogleService{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		refreshToken: cfg.RefreshToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (g *GoogleService) Enabled() bool {
	return g.clientID != "" && g.clientSecret != "" && g.refreshToken != ""
}

func (g *GoogleService) SearchConsoleSubmitSitemap(ctx context.Context, siteURL, sitemapURL string) (json.RawMessage, error) {
	endpoint := "https://www.googleapis.com/webmasters/v3/sites/" + url.PathEscape(siteURL) + "/sitemaps/" + url.PathEscape(sitemapURL)
	return g.doJSON(ctx, http.MethodPut, endpoint, nil)
}

func (g *GoogleService) SearchConsoleSites(ctx context.Context) (json.RawMessage, error) {
	return g.doJSON(ctx, http.MethodGet, "https://www.googleapis.com/webmasters/v3/sites", nil)
}

func (g *GoogleService) SearchConsoleAddSite(ctx context.Context, siteURL string) (json.RawMessage, error) {
	endpoint := "https://www.googleapis.com/webmasters/v3/sites/" + url.PathEscape(siteURL)
	return g.doJSON(ctx, http.MethodPut, endpoint, nil)
}

func (g *GoogleService) SearchConsoleInspectURL(ctx context.Context, req inspectURLRequest) (json.RawMessage, error) {
	if req.LanguageCode == "" {
		req.LanguageCode = "en-US"
	}
	return g.doJSON(ctx, http.MethodPost, "https://searchconsole.googleapis.com/v1/urlInspection/index:inspect", req)
}

func (g *GoogleService) SearchConsoleAnalytics(ctx context.Context, siteURL string, body json.RawMessage) (json.RawMessage, error) {
	endpoint := "https://www.googleapis.com/webmasters/v3/sites/" + url.PathEscape(siteURL) + "/searchAnalytics/query"
	return g.doRawJSON(ctx, http.MethodPost, endpoint, body)
}

func (g *GoogleService) AdSenseAccounts(ctx context.Context) (json.RawMessage, error) {
	return g.doJSON(ctx, http.MethodGet, "https://adsense.googleapis.com/v2/accounts", nil)
}

func (g *GoogleService) AdSenseSites(ctx context.Context, account string) (json.RawMessage, error) {
	account = strings.Trim(account, "/")
	if !strings.HasPrefix(account, "accounts/") {
		return nil, fmt.Errorf("account must use format accounts/{account}")
	}
	return g.doJSON(ctx, http.MethodGet, "https://adsense.googleapis.com/v2/"+account+"/sites", nil)
}

func (g *GoogleService) AdSenseReport(ctx context.Context, req adsenseReportRequest) (json.RawMessage, error) {
	account := strings.Trim(req.Account, "/")
	if !strings.HasPrefix(account, "accounts/") {
		return nil, fmt.Errorf("account must use format accounts/{account}")
	}

	values := url.Values{}
	for _, dimension := range req.Dimensions {
		values.Add("dimensions", dimension)
	}
	for _, metric := range req.Metrics {
		values.Add("metrics", metric)
	}
	for _, filter := range req.Filters {
		values.Add("filters", filter)
	}
	if req.DateRange != "" {
		values.Set("dateRange", req.DateRange)
	}
	if req.StartDate != "" {
		if err := addGoogleDate(values, "startDate", req.StartDate); err != nil {
			return nil, err
		}
	}
	if req.EndDate != "" {
		if err := addGoogleDate(values, "endDate", req.EndDate); err != nil {
			return nil, err
		}
	}

	endpoint := "https://adsense.googleapis.com/v2/" + account + "/reports:generate"
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return g.doJSON(ctx, http.MethodGet, endpoint, nil)
}

func (g *GoogleService) AnalyticsRunReport(ctx context.Context, propertyID string, body json.RawMessage) (json.RawMessage, error) {
	property := strings.Trim(propertyID, "/")
	if !strings.HasPrefix(property, "properties/") {
		property = "properties/" + property
	}
	endpoint := "https://analyticsdata.googleapis.com/v1beta/" + property + ":runReport"
	return g.doRawJSON(ctx, http.MethodPost, endpoint, body)
}

func (g *GoogleService) doJSON(ctx context.Context, method, endpoint string, body any) (json.RawMessage, error) {
	var raw io.Reader
	if body != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
		raw = &buf
	}
	return g.do(ctx, method, endpoint, raw)
}

func (g *GoogleService) doRawJSON(ctx context.Context, method, endpoint string, body json.RawMessage) (json.RawMessage, error) {
	return g.do(ctx, method, endpoint, bytes.NewReader(body))
}

func (g *GoogleService) do(ctx context.Context, method, endpoint string, body io.Reader) (json.RawMessage, error) {
	token, err := g.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google api returned %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(payload), nil
}

func (g *GoogleService) AccessToken(ctx context.Context) (string, error) {
	if !g.Enabled() {
		return "", fmt.Errorf("google api is not configured")
	}

	g.mu.Lock()
	if g.accessToken != "" && time.Now().Add(time.Minute).Before(g.tokenExpiry) {
		token := g.accessToken
		g.mu.Unlock()
		return token, nil
	}
	g.mu.Unlock()

	form := url.Values{}
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)
	form.Set("refresh_token", g.refreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("google token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}

	var token tokenRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("google token response did not include access_token")
	}

	g.mu.Lock()
	g.accessToken = token.AccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	g.mu.Unlock()

	return token.AccessToken, nil
}

func addGoogleDate(values url.Values, prefix, value string) error {
	parts := strings.Split(value, "-")
	if len(parts) != 3 {
		return fmt.Errorf("%s must use YYYY-MM-DD", prefix)
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("%s year: %w", prefix, err)
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("%s month: %w", prefix, err)
	}
	day, err := strconv.Atoi(parts[2])
	if err != nil {
		return fmt.Errorf("%s day: %w", prefix, err)
	}
	values.Set(prefix+".year", strconv.Itoa(year))
	values.Set(prefix+".month", strconv.Itoa(month))
	values.Set(prefix+".day", strconv.Itoa(day))
	return nil
}

type tokenRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type inspectURLRequest struct {
	SiteURL       string `json:"siteUrl"`
	InspectionURL string `json:"inspectionUrl"`
	LanguageCode  string `json:"languageCode,omitempty"`
}

type adsenseReportRequest struct {
	Account    string   `json:"account"`
	StartDate  string   `json:"start_date"`
	EndDate    string   `json:"end_date"`
	DateRange  string   `json:"date_range"`
	Dimensions []string `json:"dimensions"`
	Metrics    []string `json:"metrics"`
	Filters    []string `json:"filters"`
}
