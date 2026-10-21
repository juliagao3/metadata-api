package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juliagao3/metadata-api/internal/storage"
)

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
				object := map[string]any{"title": "App", "version": "1", "maintainer_id": 7}
				object[field.name] = strings.Repeat("\u754c", length)
				body, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				createCalled := false
				db := stubDatabase{createApplication: func(ctx context.Context, input storage.Application) (storage.Application, error) {
					createCalled = true
					input.ID = 42
					return input, nil
				}}
				request := httptest.NewRequest(http.MethodPost, "/api/v1/applications", strings.NewReader(string(body)))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				NewRouter(db).ServeHTTP(response, request)
				if response.Code != wantStatus || createCalled != (length == field.max) {
					t.Fatalf("status = %d, want %d; create called = %t", response.Code, wantStatus, createCalled)
				}
			})
		}
	}
}

func TestApplicationRouteMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewRouter(stubDatabase{}).ServeHTTP(response, httptest.NewRequest(method, "/api/v1/applications", nil))
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "POST" {
				t.Fatalf("status = %d, Allow = %q", response.Code, response.Header().Get("Allow"))
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Error("HEAD error must not include a body")
			}
		})
	}
}
