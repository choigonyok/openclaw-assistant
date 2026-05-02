package app

import (
	"context"
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
		if err := getChecklistJSON(ctx, store, []string{"index.json", "checklist/index.json"}, &index); err != nil {
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
		keys := []string{
			"templates/" + templateID + ".json",
			"checklist/templates/" + templateID + ".json",
		}
		if err := getChecklistJSON(ctx, store, keys, &template); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "checklist template not found"})
			return
		}
		writeJSON(w, http.StatusOK, template)
	}
}

func getChecklistJSON(ctx context.Context, store thinkJSONStore, keys []string, v any) error {
	var lastErr error
	for _, key := range keys {
		if err := store.GetJSON(ctx, key, v); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}
