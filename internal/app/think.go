package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type R2Client struct {
	s3     *s3.Client
	bucket string
}

func NewR2Client(accountID, accessKeyID, secretAccessKey, bucket string) (*R2Client, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("r2 config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	return &R2Client{s3: client, bucket: bucket}, nil
}

func (r *R2Client) Enabled() bool {
	return r != nil && r.s3 != nil && r.bucket != ""
}

func (r *R2Client) GetJSON(ctx context.Context, key string, v any) error {
	out, err := r.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("r2 get %s: %w", key, err)
	}
	defer out.Body.Close()
	return json.NewDecoder(out.Body).Decode(v)
}

func (r *R2Client) PutJSON(ctx context.Context, key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = r.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        io.NopCloser(bytes.NewReader(data)),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("r2 put %s: %w", key, err)
	}
	return nil
}

// ── API handlers ──────────────────────────────────────────────

func NewThinkHandler(r2 *R2Client) http.Handler {
	if !r2.Enabled() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "think storage is not configured"})
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/think/categories", handleThinkCategories(r2))
	mux.HandleFunc("/api/think/dilemmas/", handleThinkDilemmas(r2))
	mux.HandleFunc("/api/think/dilemma/", handleThinkDilemma(r2))
	mux.HandleFunc("/api/think/votes/", handleThinkVotes(r2))
	return mux
}

func handleThinkCategories(r2 *R2Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		var categories []map[string]any
		if err := r2.GetJSON(ctx, "think/categories.json", &categories); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, categories)
	}
}

func handleThinkDilemmas(r2 *R2Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		categoryID := strings.TrimPrefix(r.URL.Path, "/api/think/dilemmas/")
		if categoryID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category id required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		var dilemmas []map[string]any
		if err := r2.GetJSON(ctx, "think/dilemmas/"+categoryID+".json", &dilemmas); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "category not found"})
			return
		}
		writeJSON(w, http.StatusOK, dilemmas)
	}
}

func handleThinkDilemma(r2 *R2Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		dilemmaID := strings.TrimPrefix(r.URL.Path, "/api/think/dilemma/")
		if dilemmaID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dilemma id required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// dilemmaID 형식: "{categoryId}/{dilemmaId}"
		// 예: ethics/trolley-problem
		parts := strings.SplitN(dilemmaID, "/", 2)
		if len(parts) != 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid dilemma id format, use {categoryId}/{dilemmaId}"})
			return
		}
		categoryID, itemID := parts[0], parts[1]

		var dilemmas []map[string]any
		if err := r2.GetJSON(ctx, "think/dilemmas/"+categoryID+".json", &dilemmas); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "category not found"})
			return
		}
		for _, d := range dilemmas {
			if id, _ := d["id"].(string); id == itemID {
				writeJSON(w, http.StatusOK, d)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dilemma not found"})
	}
}

type votePayload struct {
	Option string `json:"option"` // "a" or "b"
}

type votes struct {
	A int `json:"a"`
	B int `json:"b"`
}

func handleThinkVotes(r2 *R2Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// /api/think/votes/{categoryId}/{dilemmaId}
		path := strings.TrimPrefix(r.URL.Path, "/api/think/votes/")
		if path == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dilemma path required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		key := "think/votes/" + path + ".json"

		switch r.Method {
		case http.MethodGet:
			var v votes
			if err := r2.GetJSON(ctx, key, &v); err != nil {
				// 아직 투표 없으면 0,0 반환
				writeJSON(w, http.StatusOK, votes{A: 0, B: 0})
				return
			}
			writeJSON(w, http.StatusOK, v)

		case http.MethodPost:
			var payload votePayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
				return
			}
			if payload.Option != "a" && payload.Option != "b" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "option must be 'a' or 'b'"})
				return
			}

			var v votes
			_ = r2.GetJSON(ctx, key, &v) // 없으면 0,0으로 시작

			if payload.Option == "a" {
				v.A++
			} else {
				v.B++
			}

			if err := r2.PutJSON(ctx, key, v); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, v)

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	}
}
