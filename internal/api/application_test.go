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

func TestUpdateApplication(t *testing.T) {
	t.Parallel()
	validBody := `{"title":"Updated App","version":"2.0"}`
	for _, tt := range []struct {
		name        string
		body        string
		path        string
		contentType string
		storageErr  error
		wantStatus  int
		wantCode    string
		wantFields  []string
		wantUpdate  bool
	}{
		{name: "minimal", body: validBody, contentType: "application/json", wantStatus: 200, wantUpdate: true},
		{name: "full metadata", body: `{"title":" Updated App ","version":" latest ","company":" Example ","website":"not a URL","source":"local","license":"custom","description":"First line\nSecond line"}`, contentType: "application/json; charset=utf-8", wantStatus: 200, wantUpdate: true},
		{name: "exact byte limit", body: validBody + strings.Repeat(" ", maxRequestBytes-len(validBody)), contentType: "application/json", wantStatus: 200, wantUpdate: true},
		{name: "missing title", body: `{"version":"2"}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"title"}},
		{name: "missing version", body: `{"title":"App"}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"version"}},
		{name: "empty object", body: `{}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"title", "version"}},
		{name: "blank title", body: `{"title":"\u2003","version":"2"}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"title"}},
		{name: "blank version", body: `{"title":"App","version":" "}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"version"}},
		{name: "changed creator", body: `{"title":"App","version":"2","maintainer_id":4}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "unchanged creator", body: `{"title":"App","version":"2","maintainer_id":3}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "client supplied ID", body: `{"title":"App","version":"2","id":7}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "null optional", body: `{"title":"App","version":"2","company":null}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "wrong type", body: `{"title":"App","version":2}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "unknown field", body: `{"title":"App","version":"2","extra":true}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "wrong casing", body: `{"Title":"App","version":"2"}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "malformed", body: `{"title":`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "multiple objects", body: validBody + `{}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "missing content type", body: validBody, wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "wrong content type", body: validBody, contentType: "text/plain", wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "oversized", body: validBody + strings.Repeat(" ", maxRequestBytes), contentType: "application/json", wantStatus: 413, wantCode: "request_too_large"},
		{name: "invalid ID", body: validBody, path: "/api/v1/applications/0", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "query", body: validBody, path: "/api/v1/applications/7?extra=true", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "missing", body: validBody, contentType: "application/json", storageErr: fmt.Errorf("update: %w", storage.ErrNotFound), wantStatus: 404, wantCode: "not_found", wantUpdate: true},
		{name: "busy", body: validBody, contentType: "application/json", storageErr: fmt.Errorf("update: %w", storage.ErrUnavailable), wantStatus: 503, wantCode: "database_unavailable", wantUpdate: true},
		{name: "failure", body: validBody, contentType: "application/json", storageErr: errors.New("private database details"), wantStatus: 500, wantCode: "internal_error", wantUpdate: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(t.Context(), contextKey{}, "request")
			updateCalled := false
			db := stubDatabase{updateApplication: func(ctx context.Context, id int64, input storage.Application) (storage.Application, error) {
				updateCalled = true
				if id != 7 || ctx.Value(contextKey{}) != "request" || input.ID != 0 || input.MaintainerID != 0 {
					t.Error("storage must receive the path ID, request context, and only editable input")
				}
				input.ID = id
				input.MaintainerID = 3
				return input, tt.storageErr
			}}
			path := tt.path
			if path == "" {
				path = "/api/v1/applications/7"
			}
			request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(tt.body)).WithContext(ctx)
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			response := httptest.NewRecorder()
			NewRouter(db).ServeHTTP(response, request)
			if response.Code != tt.wantStatus || updateCalled != tt.wantUpdate {
				t.Fatalf("status = %d, want %d; update called = %t: %s", response.Code, tt.wantStatus, updateCalled, response.Body.String())
			}
			if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Location") != "" {
				t.Error("response must be JSON without a creation Location")
			}
			if tt.wantCode != "" {
				var body errorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != tt.wantCode || body.Error.Message == "" ||
					strings.Contains(response.Body.String(), "private database details") {
					t.Errorf("unexpected error: %+v", body)
				}
				for _, field := range tt.wantFields {
					if body.Error.Fields[field] == "" {
						t.Errorf("missing field error for %s", field)
					}
				}
				if body.Error.Fields["maintainer_id"] != "" {
					t.Error("PUT must not require the immutable creator")
				}
				return
			}
			var got, want storage.Application
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.body), &want); err != nil {
				t.Fatal(err)
			}
			want.ID = 7
			want.MaintainerID = 3
			if got != want {
				t.Errorf("updated = %+v, want %+v", got, want)
			}
		})
	}
}

func TestGetApplication(t *testing.T) {
	t.Parallel()
	record := storage.Application{
		Title: " App ", Version: "latest", Company: "Example", Website: "https://example.com",
		Source: "local source", License: "custom", Description: "Description\nSecond line", MaintainerID: 3,
	}
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
		{name: "found", path: "/api/v1/applications/7", wantID: 7, wantStatus: 200, wantGet: true},
		{name: "leading zeros", path: "/api/v1/applications/0007", wantID: 7, wantStatus: 200, wantGet: true},
		{name: "largest ID", path: "/api/v1/applications/9223372036854775807", wantID: 9223372036854775807, wantStatus: 200, wantGet: true},
		{name: "missing", path: "/api/v1/applications/7", wantID: 7, storageErr: fmt.Errorf("get: %w", storage.ErrNotFound), wantStatus: 404, wantCode: "not_found", wantGet: true},
		{name: "busy", path: "/api/v1/applications/7", wantID: 7, storageErr: fmt.Errorf("get: %w", storage.ErrUnavailable), wantStatus: 503, wantCode: "database_unavailable", wantGet: true},
		{name: "failure", path: "/api/v1/applications/7", wantID: 7, storageErr: errors.New("private database details"), wantStatus: 500, wantCode: "internal_error", wantGet: true},
		{name: "head", method: "HEAD", path: "/api/v1/applications/7", wantID: 7, wantStatus: 200, wantGet: true},
		{name: "head missing", method: "HEAD", path: "/api/v1/applications/7", wantID: 7, storageErr: storage.ErrNotFound, wantStatus: 404, wantGet: true},
		{name: "zero", path: "/api/v1/applications/0", wantStatus: 400, wantCode: "invalid_request"},
		{name: "negative", path: "/api/v1/applications/-7", wantStatus: 400, wantCode: "invalid_request"},
		{name: "plus sign", path: "/api/v1/applications/+7", wantStatus: 400, wantCode: "invalid_request"},
		{name: "nonnumeric", path: "/api/v1/applications/seven", wantStatus: 400, wantCode: "invalid_request"},
		{name: "overflow", path: "/api/v1/applications/9223372036854775808", wantStatus: 400, wantCode: "invalid_request"},
		{name: "query", path: "/api/v1/applications/7?extra=true", wantStatus: 400, wantCode: "invalid_request"},
		{name: "missing segment", path: "/api/v1/applications/", wantStatus: 404, wantCode: "not_found"},
		{name: "extra segment", path: "/api/v1/applications/7/extra", wantStatus: 404, wantCode: "not_found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(t.Context(), contextKey{}, "request")
			getCalled := false
			db := stubDatabase{getApplication: func(ctx context.Context, id int64) (storage.Application, error) {
				getCalled = true
				if id != tt.wantID || ctx.Value(contextKey{}) != "request" {
					t.Errorf("unexpected ID or missing request context: %d", id)
				}
				result := record
				result.ID = id
				return result, tt.storageErr
			}}
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			response := httptest.NewRecorder()
			NewRouter(db).ServeHTTP(response, httptest.NewRequest(method, tt.path, nil).WithContext(ctx))
			if response.Code != tt.wantStatus || getCalled != tt.wantGet {
				t.Fatalf("status = %d, want %d; get called = %t: %s", response.Code, tt.wantStatus, getCalled, response.Body.String())
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
				if body.Error.Code != tt.wantCode || body.Error.Message == "" ||
					strings.Contains(response.Body.String(), "private database details") {
					t.Errorf("unexpected error: %+v", body)
				}
				return
			}
			var got storage.Application
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := record
			want.ID = tt.wantID
			if got != want {
				t.Errorf("application = %+v, want %+v", got, want)
			}
		})
	}
}

func TestListApplications(t *testing.T) {
	t.Parallel()
	records := []storage.Application{
		{ID: 3, Title: "First", Version: "1", MaintainerID: 7},
		{ID: 20, Title: "Second", Version: "latest", Company: "Example", Website: "https://example.com",
			Source: "local", License: "custom", Description: "Description", MaintainerID: 8},
	}
	for _, tt := range []struct {
		name       string
		method     string
		path       string
		records    []storage.Application
		storageErr error
		wantStatus int
		wantCode   string
		wantList   bool
	}{
		{name: "empty", records: []storage.Application{}, wantStatus: 200, wantList: true},
		{name: "records", records: records, wantStatus: 200, wantList: true},
		{name: "head", method: "HEAD", records: records, wantStatus: 200, wantList: true},
		{name: "head busy", method: "HEAD", storageErr: storage.ErrUnavailable, wantStatus: 503, wantList: true},
		{name: "busy", storageErr: fmt.Errorf("list: %w", storage.ErrUnavailable), wantStatus: 503, wantCode: "database_unavailable", wantList: true},
		{name: "failure", storageErr: errors.New("private database details"), wantStatus: 500, wantCode: "internal_error", wantList: true},
		{name: "query", path: "/api/v1/applications?extra=true", wantStatus: 400, wantCode: "invalid_request"},
		{name: "empty query", path: "/api/v1/applications?", wantStatus: 400, wantCode: "invalid_request"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(t.Context(), contextKey{}, "request")
			listCalled := false
			db := stubDatabase{listApplications: func(ctx context.Context) ([]storage.Application, error) {
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
				path = "/api/v1/applications"
			}
			response := httptest.NewRecorder()
			NewRouter(db).ServeHTTP(response, httptest.NewRequest(method, path, nil).WithContext(ctx))
			if response.Code != tt.wantStatus || listCalled != tt.wantList {
				t.Fatalf("status = %d, want %d; list called = %t: %s", response.Code, tt.wantStatus, listCalled, response.Body.String())
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
				if body.Error.Code != tt.wantCode || body.Error.Message == "" ||
					strings.Contains(response.Body.String(), "private database details") {
					t.Errorf("unexpected error: %+v", body)
				}
				return
			}
			var got []storage.Application
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.records) {
				t.Errorf("applications = %+v, want %+v", got, tt.records)
			}
			if len(tt.records) == 0 && strings.TrimSpace(response.Body.String()) != "[]" {
				t.Error("empty collection must be [], not null")
			}
		})
	}
}

func TestApplicationItemRouteMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewRouter(stubDatabase{}).ServeHTTP(response, httptest.NewRequest(method, "/api/v1/applications/1", nil))
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD, PUT" {
				t.Fatalf("status = %d, Allow = %q", response.Code, response.Header().Get("Allow"))
			}
		})
	}
}

func TestCreateApplication(t *testing.T) {
	t.Parallel()
	validBody := `{"title":"My App","version":"1.0","maintainer_id":7}`
	for _, tt := range []struct {
		name        string
		body        string
		path        string
		contentType string
		storageErr  error
		wantStatus  int
		wantCode    string
		wantFields  []string
		wantCreate  bool
	}{
		{name: "minimal", body: validBody, contentType: "application/json", wantStatus: 201, wantCreate: true},
		{name: "all fields preserved", body: `{"title":" My App ","version":" latest ","company":"Example","website":"not a URL","source":"local source","license":"custom","description":"Description\nSecond line","maintainer_id":7}`, contentType: "application/json; charset=utf-8", wantStatus: 201, wantCreate: true},
		{name: "maximum maintainer ID", body: `{"title":"App","version":"1","maintainer_id":9223372036854775807}`, contentType: "application/json", wantStatus: 201, wantCreate: true},
		{name: "exact byte limit", body: validBody + strings.Repeat(" ", maxRequestBytes-len(validBody)), contentType: "application/json", wantStatus: 201, wantCreate: true},
		{name: "missing content type", body: validBody, wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "wrong content type", body: validBody, contentType: "text/plain", wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "empty", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "malformed", body: `{"title":`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "multiple objects", body: validBody + `{}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "array", body: `[]`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "null object", body: `null`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "null optional field", body: `{"title":"App","version":"1","maintainer_id":7,"company":null}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "wrong string type", body: `{"title":1,"version":"1","maintainer_id":7}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "unknown field", body: `{"title":"App","version":"1","maintainer_id":7,"extra":true}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "client supplied ID", body: `{"id":1,"title":"App","version":"1","maintainer_id":7}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "wrong field casing", body: `{"Title":"App","version":"1","maintainer_id":7}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "invalid UTF-8", body: "{\"title\":\"\xff\",\"version\":\"1\",\"maintainer_id\":7}", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "oversized", body: validBody + strings.Repeat(" ", maxRequestBytes), contentType: "application/json", wantStatus: 413, wantCode: "request_too_large"},
		{name: "missing required fields", body: `{}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"title", "version", "maintainer_id"}},
		{name: "blank title", body: `{"title":" \t","version":"1","maintainer_id":7}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"title"}},
		{name: "blank version", body: `{"title":"App","version":"\u2003","maintainer_id":7}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"version"}},
		{name: "zero maintainer", body: `{"title":"App","version":"1","maintainer_id":0}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"maintainer_id"}},
		{name: "negative maintainer", body: `{"title":"App","version":"1","maintainer_id":-7}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"maintainer_id"}},
		{name: "string maintainer", body: `{"title":"App","version":"1","maintainer_id":"7"}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "fractional maintainer", body: `{"title":"App","version":"1","maintainer_id":7.5}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "overflow maintainer", body: `{"title":"App","version":"1","maintainer_id":9223372036854775808}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "null maintainer", body: `{"title":"App","version":"1","maintainer_id":null}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "missing maintainer", body: validBody, contentType: "application/json", storageErr: fmt.Errorf("create: %w", storage.ErrForeignKey), wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"maintainer_id"}, wantCreate: true},
		{name: "query", body: validBody, path: "/api/v1/applications?extra=true", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "empty query", body: validBody, path: "/api/v1/applications?", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "busy", body: validBody, contentType: "application/json", storageErr: fmt.Errorf("create: %w", storage.ErrUnavailable), wantStatus: 503, wantCode: "database_unavailable", wantCreate: true},
		{name: "failure", body: validBody, contentType: "application/json", storageErr: errors.New("private database details"), wantStatus: 500, wantCode: "internal_error", wantCreate: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(t.Context(), contextKey{}, "request")
			createCalled := false
			db := stubDatabase{createApplication: func(ctx context.Context, input storage.Application) (storage.Application, error) {
				createCalled = true
				if ctx.Value(contextKey{}) != "request" {
					t.Error("storage must receive the request context")
				}
				if input.ID != 0 {
					t.Error("client must not assign an ID")
				}
				input.ID = 42
				return input, tt.storageErr
			}}
			path := tt.path
			if path == "" {
				path = "/api/v1/applications"
			}
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tt.body)).WithContext(ctx)
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			response := httptest.NewRecorder()
			NewRouter(db).ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if createCalled != tt.wantCreate {
				t.Errorf("create called = %t, want %t", createCalled, tt.wantCreate)
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Error("response must use application/json")
			}
			if tt.wantCode != "" {
				var body errorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != tt.wantCode || body.Error.Message == "" {
					t.Errorf("unexpected error: %+v", body)
				}
				for _, field := range tt.wantFields {
					if body.Error.Fields[field] == "" {
						t.Errorf("missing field error for %s", field)
					}
				}
				if response.Header().Get("Location") != "" || strings.Contains(response.Body.String(), "private database details") {
					t.Error("error must not claim creation or expose database details")
				}
				return
			}
			if response.Header().Get("Location") != "/api/v1/applications/42" {
				t.Error("Location must identify the new application")
			}
			var got, want storage.Application
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.body), &want); err != nil {
				t.Fatal(err)
			}
			want.ID = 42
			if got != want {
				t.Errorf("application = %+v, want %+v", got, want)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 9 {
				t.Errorf("response must include all nine fields, including empty optional strings: %s", response.Body.String())
			}
		})
	}
}

func TestApplicationFieldLengths(t *testing.T) {
	t.Parallel()
	for _, field := range []struct {
		name string
		max  int
		set  func(*storage.Application, string)
	}{
		{"title", 200, func(a *storage.Application, value string) { a.Title = value }},
		{"version", 100, func(a *storage.Application, value string) { a.Version = value }},
		{"company", 200, func(a *storage.Application, value string) { a.Company = value }},
		{"website", 2048, func(a *storage.Application, value string) { a.Website = value }},
		{"source", 2048, func(a *storage.Application, value string) { a.Source = value }},
		{"license", 100, func(a *storage.Application, value string) { a.License = value }},
		{"description", 10000, func(a *storage.Application, value string) { a.Description = value }},
	} {
		for _, length := range []int{field.max, field.max + 1} {
			t.Run(fmt.Sprintf("%s/%d", field.name, length), func(t *testing.T) {
				input := storage.Application{Title: "App", Version: "1", MaintainerID: 7}
				field.set(&input, strings.Repeat("\u754c", length))
				fields := validateApplication(input)
				wantStatus := http.StatusCreated
				if length == field.max {
					if len(fields) != 0 {
						t.Fatalf("exact Unicode character limit must be accepted: %v", fields)
					}
				} else {
					wantStatus = http.StatusUnprocessableEntity
					if len(fields) != 1 || fields[field.name] == "" {
						t.Errorf("over-limit field error = %v", fields)
					}
				}
				for _, method := range []string{http.MethodPost, http.MethodPut} {
					t.Run(method, func(t *testing.T) {
						path := "/api/v1/applications"
						status := wantStatus
						object := map[string]any{"title": "App", "version": "1"}
						if method == http.MethodPost {
							object["maintainer_id"] = 7
						} else {
							path += "/42"
							if status == http.StatusCreated {
								status = http.StatusOK
							}
						}
						object[field.name] = strings.Repeat("\u754c", length)
						body, err := json.Marshal(object)
						if err != nil {
							t.Fatal(err)
						}
						writeCalled := false
						db := stubDatabase{
							createApplication: func(ctx context.Context, input storage.Application) (storage.Application, error) {
								writeCalled = true
								input.ID = 42
								return input, nil
							},
							updateApplication: func(ctx context.Context, id int64, input storage.Application) (storage.Application, error) {
								writeCalled = true
								input.ID, input.MaintainerID = id, 7
								return input, nil
							},
						}
						request := httptest.NewRequest(method, path, strings.NewReader(string(body)))
						request.Header.Set("Content-Type", "application/json")
						response := httptest.NewRecorder()
						NewRouter(db).ServeHTTP(response, request)
						if response.Code != status || writeCalled != (length == field.max) {
							t.Fatalf("status = %d, want %d; write called = %t", response.Code, status, writeCalled)
						}
					})
				}
			})
		}
	}
}

func TestApplicationCollectionRouteMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewRouter(stubDatabase{}).ServeHTTP(response, httptest.NewRequest(method, "/api/v1/applications", nil))
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD, POST" {
				t.Fatalf("status = %d, Allow = %q", response.Code, response.Header().Get("Allow"))
			}
		})
	}
}
