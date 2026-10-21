package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type databasePinger interface {
	PingContext(context.Context) error
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewRouter(db databasePinger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not supported for this path.")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			log.Printf("readiness database check failed: %v", err)
			writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable.")
			return
		}
		writeJSON(w, r, http.StatusOK, struct {
			Status string `json:"status"`
		}{Status: "ready"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "Requested resource does not exist.")
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "Query parameters are not supported.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, r, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
