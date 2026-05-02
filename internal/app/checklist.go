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
	mux.HandleFunc("/api/checklist/index", handleChecklistIndex(store))
	mux.HandleFunc("/api/checklist/templates/", handleChecklistTemplate(store))
	return mux
}

func handleChecklistIndex(store thinkJSONStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		var index map[string]any
		if err := getChecklistJSON(ctx, store, checklistIndexKeys(), &index); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, index)
	}
}

func handleChecklistTemplate(store thinkJSONStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		templateID := strings.TrimPrefix(r.URL.Path, "/api/checklist/templates/")
		templateID = strings.TrimSuffix(templateID, ".json")
		if templateID == "" || !checklistTemplateIDPattern.MatchString(templateID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid checklist template id"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		var template map[string]any
		if err := getChecklistJSON(ctx, store, checklistTemplateKeys(templateID), &template); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "checklist template not found"})
			return
		}
		writeJSON(w, http.StatusOK, template)
	}
}

func checklistIndexKeys() []string {
	return []string{
		"index.json",
		"checklist/index.json",
		"public/checklist/index.json",
		"openclaw-checklist/public/checklist/index.json",
		"dist/checklist/index.json",
		"openclaw-checklist/dist/checklist/index.json",
	}
}

func checklistTemplateKeys(templateID string) []string {
	filename := templateID + ".json"
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
