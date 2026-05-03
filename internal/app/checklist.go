package app

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var checklistTemplateIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func NewChecklistHandler(r2 *R2Client) http.Handler {
	return newChecklistHandler(r2)
}

func newChecklistHandler(store thinkJSONStore) http.Handler {
	if store == nil || !store.Enabled() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "checklist storage is not configured"})
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/checklist/index", handleChecklistIndex(store, "ko"))
	mux.HandleFunc("/api/checklist/templates/", handleChecklistTemplate(store, "ko"))
	mux.HandleFunc("/api/checklist/en/index", handleChecklistIndex(store, "en"))
	mux.HandleFunc("/api/checklist/en/templates/", handleChecklistTemplate(store, "en"))
	return mux
}

func handleChecklistIndex(store thinkJSONStore, lang string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		var index map[string]any
		if err := getChecklistJSON(ctx, store, checklistIndexKeys(lang), &index); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, index)
	}
}

func handleChecklistTemplate(store thinkJSONStore, lang string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		prefix := "/api/checklist/templates/"
		if lang == "en" {
			prefix = "/api/checklist/en/templates/"
		}
		templateID := strings.TrimPrefix(r.URL.Path, prefix)
		templateID = strings.TrimSuffix(templateID, ".json")
		if templateID == "" || !checklistTemplateIDPattern.MatchString(templateID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid checklist template id"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		var template map[string]any
		if err := getChecklistJSON(ctx, store, checklistTemplateKeys(templateID, lang), &template); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "checklist template not found"})
			return
		}
		writeJSON(w, http.StatusOK, template)
	}
}

func checklistIndexKeys(lang string) []string {
	if lang == "en" {
		return []string{
			"en/index.json",
			"checklist/en/index.json",
			"public/checklist/en/index.json",
			"openclaw-checklist/public/checklist/en/index.json",
			"dist/checklist/en/index.json",
			"openclaw-checklist/dist/checklist/en/index.json",
		}
	}
	return []string{
		"index.json",
		"checklist/index.json",
		"public/checklist/index.json",
		"openclaw-checklist/public/checklist/index.json",
		"dist/checklist/index.json",
		"openclaw-checklist/dist/checklist/index.json",
	}
}

func checklistTemplateKeys(templateID string, lang string) []string {
	filename := templateID + ".json"
	if lang == "en" {
		return []string{
			"en/templates/" + filename,
			"checklist/en/templates/" + filename,
			"public/checklist/en/templates/" + filename,
			"openclaw-checklist/public/checklist/en/templates/" + filename,
			"dist/checklist/en/templates/" + filename,
			"openclaw-checklist/dist/checklist/en/templates/" + filename,
		}
	}
	return []string{
		"templates/" + filename,
		"checklist/templates/" + filename,
		"public/checklist/templates/" + filename,
		"openclaw-checklist/public/checklist/templates/" + filename,
		"dist/checklist/templates/" + filename,
		"openclaw-checklist/dist/checklist/templates/" + filename,
	}
}

func getChecklistJSON(ctx context.Context, store thinkJSONStore, keys []string, v any) error {
	for _, key := range keys {
		if err := store.GetJSON(ctx, key, v); err != nil {
			continue
		}
		return nil
	}
	return fmt.Errorf("checklist object lookup failed; tried keys: %s", strings.Join(keys, ", "))
}
