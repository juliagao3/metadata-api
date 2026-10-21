package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/juliagao3/metadata-api/internal/storage"
)

func TestGetMaintainer(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		path       string
		method     string
		storageErr error
		wantID     int64
		wantStatus int
		wantCode   string
		wantGet    bool
	}{
		{name: "found", path: "/api/v1/maintainers/7", wantID: 7, wantStatus: 200, wantGet: true},
		{name: "leading zeros", path: "/api/v1/maintainers/0007", wantID: 7, wantStatus: 200, wantGet: true},
		{name: "largest ID", path: "/api/v1/maintainers/9223372036854775807", wantID: 9223372036854775807, wantStatus: 200, wantGet: true},
		{name: "missing", path: "/api/v1/maintainers/7", wantID: 7, storageErr: fmt.Errorf("get: %w", storage.ErrNotFound), wantStatus: 404, wantCode: "not_found", wantGet: true},
		{name: "busy", path: "/api/v1/maintainers/7", wantID: 7, storageErr: fmt.Errorf("get: %w", storage.ErrUnavailable), wantStatus: 503, wantCode: "database_unavailable", wantGet: true},
		{name: "storage failure", path: "/api/v1/maintainers/7", wantID: 7, storageErr: errors.New("private database details"), wantStatus: 500, wantCode: "internal_error", wantGet: true},
		{name: "head", method: "HEAD", path: "/api/v1/maintainers/7", wantID: 7, wantStatus: 200, wantGet: true},
		{name: "head missing", method: "HEAD", path: "/api/v1/maintainers/7", wantID: 7, storageErr: storage.ErrNotFound, wantStatus: 404, wantGet: true},
		{name: "zero", path: "/api/v1/maintainers/0", wantStatus: 400, wantCode: "invalid_request"},
		{name: "negative", path: "/api/v1/maintainers/-1", wantStatus: 400, wantCode: "invalid_request"},
		{name: "plus sign", path: "/api/v1/maintainers/+1", wantStatus: 400, wantCode: "invalid_request"},
		{name: "nondecimal", path: "/api/v1/maintainers/abc", wantStatus: 400, wantCode: "invalid_request"},
		{name: "overflow", path: "/api/v1/maintainers/9223372036854775808", wantStatus: 400, wantCode: "invalid_request"},
		{name: "whitespace", path: "/api/v1/maintainers/7%20", wantStatus: 400, wantCode: "invalid_request"},
		{name: "non-ASCII digit", path: "/api/v1/maintainers/%EF%BC%91", wantStatus: 400, wantCode: "invalid_request"},
		{name: "query", path: "/api/v1/maintainers/7?extra=true", wantStatus: 400, wantCode: "invalid_request"},
		{name: "missing segment", path: "/api/v1/maintainers/", wantStatus: 404, wantCode: "not_found"},
		{name: "extra segment", path: "/api/v1/maintainers/7/extra", wantStatus: 404, wantCode: "not_found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(t.Context(), contextKey{}, "request")
			getCalled := false
			db := stubDatabase{get: func(ctx context.Context, id int64) (storage.Maintainer, error) {
				getCalled = true
				if id != tt.wantID {
					t.Errorf("ID = %d, want %d", id, tt.wantID)
				}
				if ctx.Value(contextKey{}) != "request" {
					t.Error("storage must receive the request context")
				}
				return storage.Maintainer{ID: id, Name: "Alex", Email: "alex@example.com"}, tt.storageErr
			}}
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			request := httptest.NewRequest(method, tt.path, nil).WithContext(ctx)
			response := httptest.NewRecorder()
			NewRouter(db).ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if getCalled != tt.wantGet {
				t.Errorf("get called = %t, want %t", getCalled, tt.wantGet)
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Error("response must use application/json")
			}
			if method == http.MethodHead {
				if response.Body.Len() != 0 {
					t.Error("HEAD must not return a body")
				}
				return
			}
			if tt.wantCode != "" {
				var body errorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != tt.wantCode || body.Error.Message == "" {
					t.Errorf("unexpected error: %+v", body)
				}
				if strings.Contains(response.Body.String(), "private database details") {
					t.Error("response exposes internal database details")
				}
				return
			}
			var body storage.Maintainer
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body != (storage.Maintainer{ID: tt.wantID, Name: "Alex", Email: "alex@example.com"}) {
				t.Errorf("unexpected resource: %+v", body)
			}
		})
	}
}

func TestListMaintainers(t *testing.T) {
	t.Parallel()
	records := []storage.Maintainer{
		{ID: 3, Name: "Alex", Email: "alex@example.com"},
		{ID: 8, Name: "Sam", Email: "sam@example.com"},
	}
	for _, tt := range []struct {
		name       string
		method     string
		path       string
		records    []storage.Maintainer
		storageErr error
		wantStatus int
		wantCode   string
		wantList   bool
	}{
		{name: "empty", records: []storage.Maintainer{}, wantStatus: 200, wantList: true},
		{name: "records", records: records, wantStatus: 200, wantList: true},
		{name: "head", method: "HEAD", records: records, wantStatus: 200, wantList: true},
		{name: "busy", storageErr: storage.ErrUnavailable, wantStatus: 503, wantCode: "database_unavailable", wantList: true},
		{name: "storage failure", storageErr: errors.New("private database details"), wantStatus: 500, wantCode: "internal_error", wantList: true},
		{name: "query", path: "/api/v1/maintainers?extra=true", wantStatus: 400, wantCode: "invalid_request"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(t.Context(), contextKey{}, "request")
			listCalled := false
			db := stubDatabase{list: func(ctx context.Context) ([]storage.Maintainer, error) {
				listCalled = true
				if ctx.Value(contextKey{}) != "request" {
					t.Error("storage must receive the request context")
				}
				return tt.records, tt.storageErr
			}}
			method, path := tt.method, tt.path
			if method == "" {
				method = http.MethodGet
			}
			if path == "" {
				path = "/api/v1/maintainers"
			}
			response := httptest.NewRecorder()
			NewRouter(db).ServeHTTP(response, httptest.NewRequest(method, path, nil).WithContext(ctx))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if listCalled != tt.wantList {
				t.Errorf("list called = %t, want %t", listCalled, tt.wantList)
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Error("response must use application/json")
			}
			if method == http.MethodHead {
				if response.Body.Len() != 0 {
					t.Error("HEAD must not return a body")
				}
				return
			}
			if tt.wantCode != "" {
				var body errorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != tt.wantCode || body.Error.Message == "" {
					t.Errorf("unexpected error: %+v", body)
				}
				if strings.Contains(response.Body.String(), "private database details") {
					t.Error("response exposes internal database details")
				}
				return
			}
			var body []storage.Maintainer
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body, tt.records) {
				t.Errorf("collection = %+v, want %+v", body, tt.records)
			}
			if len(tt.records) == 0 && strings.TrimSpace(response.Body.String()) != "[]" {
				t.Error("empty collection must be [], not null")
			}
		})
	}
}

func TestMaintainerItemRouteMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewRouter(stubDatabase{}).ServeHTTP(response, httptest.NewRequest(method, "/api/v1/maintainers/1", nil))
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD, PUT" {
				t.Fatalf("status = %d, Allow = %q", response.Code, response.Header().Get("Allow"))
			}
		})
	}
}
