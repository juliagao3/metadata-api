package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juliagao3/metadata-api/internal/storage"
)

type stubDatabase struct {
	ping              func(context.Context) error
	create            func(context.Context, string, string) (storage.Maintainer, error)
	get               func(context.Context, int64) (storage.Maintainer, error)
	update            func(context.Context, int64, string, string) (storage.Maintainer, error)
	remove            func(context.Context, int64) error
	list              func(context.Context) ([]storage.Maintainer, error)
	createApplication func(context.Context, storage.Application) (storage.Application, error)
	getApplication    func(context.Context, int64) (storage.Application, error)
	listApplications  func(context.Context) ([]storage.Application, error)
}

func (s stubDatabase) PingContext(ctx context.Context) error {
	return s.ping(ctx)
}

func (s stubDatabase) CreateMaintainer(ctx context.Context, name, email string) (storage.Maintainer, error) {
	return s.create(ctx, name, email)
}

func (s stubDatabase) GetMaintainer(ctx context.Context, id int64) (storage.Maintainer, error) {
	return s.get(ctx, id)
}

func (s stubDatabase) UpdateMaintainer(ctx context.Context, id int64, name, email string) (storage.Maintainer, error) {
	return s.update(ctx, id, name, email)
}

func (s stubDatabase) DeleteMaintainer(ctx context.Context, id int64) error {
	return s.remove(ctx, id)
}

func (s stubDatabase) ListMaintainers(ctx context.Context) ([]storage.Maintainer, error) {
	return s.list(ctx)
}

func (s stubDatabase) CreateApplication(ctx context.Context, input storage.Application) (storage.Application, error) {
	return s.createApplication(ctx, input)
}

func (s stubDatabase) GetApplication(ctx context.Context, id int64) (storage.Application, error) {
	return s.getApplication(ctx, id)
}

func (s stubDatabase) ListApplications(ctx context.Context) ([]storage.Application, error) {
	return s.listApplications(ctx)
}

func TestRouter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		pingErr    error
		wantStatus int
		wantCode   string
		wantPing   bool
	}{
		{name: "ready", method: "GET", path: "/readyz", wantStatus: 200, wantPing: true},
		{name: "head", method: "HEAD", path: "/readyz", wantStatus: 200, wantPing: true},
		{name: "unavailable", method: "GET", path: "/readyz", pingErr: errors.New("private database details"), wantStatus: 503, wantCode: "database_unavailable", wantPing: true},
		{name: "query", method: "GET", path: "/readyz?probe=true", wantStatus: 400, wantCode: "invalid_request"},
		{name: "empty query", method: "GET", path: "/readyz?", wantStatus: 400, wantCode: "invalid_request"},
		{name: "method", method: "POST", path: "/readyz", wantStatus: 405, wantCode: "method_not_allowed"},
		{name: "unknown path", method: "GET", path: "/missing", wantStatus: 404, wantCode: "not_found"},
		{name: "trailing slash", method: "GET", path: "/readyz/", wantStatus: 404, wantCode: "not_found"},
		{name: "head missing", method: "HEAD", path: "/missing", wantStatus: 404, wantCode: "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pingCalled := false
			db := stubDatabase{ping: func(ctx context.Context) error {
				pingCalled = true
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2*time.Second {
					t.Error("readiness check must have a bounded deadline")
				}
				return tt.pingErr
			}}
			response := httptest.NewRecorder()
			NewRouter(db).ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if pingCalled != tt.wantPing {
				t.Errorf("database called = %t, want %t", pingCalled, tt.wantPing)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if tt.wantStatus == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET, HEAD" {
				t.Error("405 response must advertise GET and HEAD")
			}
			if tt.method == http.MethodHead {
				if response.Body.Len() != 0 {
					t.Error("HEAD response must not have a body")
				}
				return
			}
			if tt.wantCode != "" {
				var body errorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != tt.wantCode || body.Error.Message == "" {
					t.Errorf("unexpected error response: %+v", body)
				}
				if strings.Contains(response.Body.String(), "private database details") {
					t.Error("response exposes internal database details")
				}
				return
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 1 || body["status"] != "ready" {
				t.Errorf("unexpected readiness response: %v", body)
			}
		})
	}
}

func TestReadinessPropagatesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	NewRouter(stubDatabase{ping: func(ctx context.Context) error {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Error("database check did not receive request cancellation")
		}
		return ctx.Err()
	}}).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}
