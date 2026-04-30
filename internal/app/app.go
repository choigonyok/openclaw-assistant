package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const Name = "openclaw-assistant"

func Run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "version" {
		_, err := fmt.Fprintf(out, "%s dev\n", Name)
		return err
	}

	if err := LoadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	cfg := ConfigFromEnv()
	google := NewGoogleService(GoogleConfig{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RefreshToken: cfg.GoogleRefreshToken,
	})
	auth := NewAuthService(AuthConfig{
		ClientID:     cfg.NaverClientID,
		ClientSecret: cfg.NaverClientSecret,
		RedirectURL:  cfg.NaverRedirectURL,
		SessionKey:   cfg.SessionSecret,
		AllowedIDs:   cfg.NaverAllowedIDs,
		DevMode:      cfg.Dev,
	})
	kis := NewKISClient(cfg.KISAppKey, cfg.KISAppSecret, cfg.KISAccountNo, cfg.KISAccountProduct, cfg.KISMock)
	upbit := NewUpbitClient(cfg.UpbitAccessKey, cfg.UpbitSecretKey)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           NewHandler(NewOpenClawClient(cfg.OpenClawBaseURL, cfg.OpenClawToken), auth, google, kis, upbit),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(out, "%s listening on http://localhost:%s\n", Name, cfg.Port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return scanner.Err()
}

type Config struct {
	Port               string
	OpenClawBaseURL    string
	OpenClawToken      string
	NaverClientID      string
	NaverClientSecret  string
	NaverRedirectURL   string
	SessionSecret      string
	NaverAllowedIDs    []string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRefreshToken string
	Dev                bool
	KISAppKey          string
	KISAppSecret       string
	KISAccountNo       string
	KISAccountProduct  string
	KISMock            bool
	UpbitAccessKey     string
	UpbitSecretKey     string
}

func ConfigFromEnv() Config {
	return Config{
		Port:               envOrDefault("PORT", "8080"),
		OpenClawBaseURL:    envOrDefault("OPENCLAW_BASE_URL", "http://localhost:18789"),
		OpenClawToken:      os.Getenv("OPENCLAW_TOKEN"),
		NaverClientID:      os.Getenv("NAVER_CLIENT_ID"),
		NaverClientSecret:  os.Getenv("NAVER_CLIENT_SECRET"),
		NaverRedirectURL:   envOrDefault("NAVER_REDIRECT_URL", "https://agent.choigonyok.com/auth/naver/callback"),
		SessionSecret:      envOrDefault("SESSION_SECRET", "dev-session-secret-change-me"),
		NaverAllowedIDs:    splitCSV(os.Getenv("NAVER_ALLOWED_IDS")),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRefreshToken: os.Getenv("GOOGLE_REFRESH_TOKEN"),
		Dev:                envBool("DEV"),
		KISAppKey:          os.Getenv("KIS_APP_KEY"),
		KISAppSecret:       os.Getenv("KIS_APP_SECRET"),
		KISAccountNo:       os.Getenv("KIS_ACCOUNT_NO"),
		KISAccountProduct:  envOrDefault("KIS_ACCOUNT_PRODUCT", "01"),
		KISMock:            envBool("KIS_MOCK"),
		UpbitAccessKey:     os.Getenv("UPBIT_ACCESS_KEY"),
		UpbitSecretKey:     os.Getenv("UPBIT_SECRET_KEY"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func splitCSV(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}
