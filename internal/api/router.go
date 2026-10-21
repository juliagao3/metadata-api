package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/juliagao3/metadata-api/internal/storage"
)

type database interface {
	PingContext(context.Context) error
	CreateMaintainer(context.Context, string, string) (storage.Maintainer, error)
	GetMaintainer(context.Context, int64) (storage.Maintainer, error)
	UpdateMaintainer(context.Context, int64, string, string) (storage.Maintainer, error)
	DeleteMaintainer(context.Context, int64) error
	ListMaintainers(context.Context) ([]storage.Maintainer, error)
	CreateApplication(context.Context, storage.Application) (storage.Application, error)
}

func NewRouter(db database) http.Handler {
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
	mux.HandleFunc("/api/v1/maintainers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			createMaintainer(w, r, db)
		case http.MethodGet, http.MethodHead:
			listMaintainers(w, r, db)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not supported for this path.")
		}
	})
	mux.HandleFunc("/api/v1/maintainers/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			getMaintainer(w, r, db)
		case http.MethodPut:
			updateMaintainer(w, r, db)
		case http.MethodDelete:
			deleteMaintainer(w, r, db)
		default:
			w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not supported for this path.")
		}
	})
	mux.HandleFunc("/api/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not supported for this path.")
			return
		}
		createApplication(w, r, db)
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
