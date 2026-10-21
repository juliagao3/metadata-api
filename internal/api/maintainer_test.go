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

func TestCreateMaintainer(t *testing.T) {
	t.Parallel()
	validBody := `{"name":"Alex","email":"alex@example.com"}`
	tests := []struct {
		name        string
		body        string
		contentType string
		path        string
		storageErr  error
		wantStatus  int
		wantCode    string
		wantFields  []string
		wantCreate  bool
	}{
		{name: "created", body: validBody, contentType: "application/json", wantStatus: 201, wantCreate: true},
		{name: "charset", body: validBody, contentType: "application/json; charset=utf-8", wantStatus: 201, wantCreate: true},
		{name: "preserve values", body: `{"name":" Alex ","email":"Alex@Example.com"}`, contentType: "application/json", wantStatus: 201, wantCreate: true},
		{name: "exact byte limit", body: validBody + strings.Repeat(" ", maxRequestBytes-len(validBody)), contentType: "application/json", wantStatus: 201, wantCreate: true},
		{name: "missing content type", body: validBody, wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "wrong content type", body: validBody, contentType: "text/plain", wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "trailing media type separator", body: validBody, contentType: "application/json;", wantStatus: 201, wantCreate: true},
		{name: "invalid content type parameter", body: validBody, contentType: "application/json; charset", wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "empty body", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "broken JSON", body: `{"name":`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "multiple JSON values", body: validBody + `{}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "trailing junk", body: validBody + `invalid`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "array", body: `[]`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "string", body: `"Alex"`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "null body", body: `null`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "null field", body: `{"name":null,"email":"alex@example.com"}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "wrong field type", body: `{"name":42,"email":"alex@example.com"}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "unknown field", body: `{"name":"Alex","email":"alex@example.com","extra":true}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "client supplied ID", body: `{"id":1,"name":"Alex","email":"alex@example.com"}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "wrong field casing", body: `{"Name":"Alex","email":"alex@example.com"}`, contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "invalid UTF-8", body: "{\"name\":\"\xff\",\"email\":\"alex@example.com\"}", contentType: "application/json", wantStatus: 400, wantCode: "invalid_request"},
		{name: "oversized object", body: `{"name":"` + strings.Repeat("a", maxRequestBytes) + `"}`, contentType: "application/json", wantStatus: 413, wantCode: "request_too_large"},
		{name: "oversized trailing whitespace", body: validBody + strings.Repeat(" ", maxRequestBytes), contentType: "application/json", wantStatus: 413, wantCode: "request_too_large"},
		{name: "missing required fields", body: `{}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"name", "email"}},
		{name: "blank name", body: `{"name":"  ","email":"alex@example.com"}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"name"}},
		{name: "invalid email", body: `{"name":"Alex","email":"not-an-email"}`, contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"email"}},
		{name: "long name", body: fmt.Sprintf(`{"name":%q,"email":"alex@example.com"}`, strings.Repeat("a", 201)), contentType: "application/json", wantStatus: 422, wantCode: "validation_failed", wantFields: []string{"name"}},
		{name: "query parameter", body: validBody, contentType: "application/json", path: "/api/v1/maintainers?extra=true", wantStatus: 400, wantCode: "invalid_request"},
		{name: "storage failure", body: validBody, contentType: "application/json", storageErr: errors.New("private database details"), wantStatus: 500, wantCode: "internal_error", wantCreate: true},
		{name: "storage busy", body: validBody, contentType: "application/json", storageErr: fmt.Errorf("wrapped: %w", storage.ErrUnavailable), wantStatus: 503, wantCode: "database_unavailable", wantCreate: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createCalled := false
			db := stubDatabase{create: func(ctx context.Context, name, email string) (storage.Maintainer, error) {
				createCalled = true
				return storage.Maintainer{ID: 42, Name: name, Email: email}, tt.storageErr
			}}
			path := tt.path
			if path == "" {
				path = "/api/v1/maintainers"
			}
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tt.body))
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
					t.Errorf("unexpected error response: %+v", body)
				}
				if len(body.Error.Fields) != len(tt.wantFields) {
					t.Errorf("field errors = %v, want %v", body.Error.Fields, tt.wantFields)
				}
				for _, field := range tt.wantFields {
					if body.Error.Fields[field] == "" {
						t.Errorf("missing field error for %s", field)
					}
				}
				if strings.Contains(response.Body.String(), "private database details") {
					t.Error("response exposes database details")
				}
				if response.Header().Get("Location") != "" {
					t.Error("failed creation must not include Location")
				}
				return
			}
			var got storage.Maintainer
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			var input maintainerInput
			if err := json.Unmarshal([]byte(tt.body), &input); err != nil {
				t.Fatal(err)
			}
			if got.ID != 42 || got.Name != input.Name || got.Email != input.Email {
				t.Errorf("unexpected resource: %+v", got)
			}
			if response.Header().Get("Location") != "/api/v1/maintainers/42" {
				t.Error("creation must include the resource Location")
			}
		})
	}
}

func TestValidateMaintainer(t *testing.T) {
	t.Parallel()
	longestEmail := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	for _, tt := range []struct {
		name       string
		input      maintainerInput
		wantFields []string
	}{
		{name: "valid", input: maintainerInput{Name: "Alex", Email: "alex@example.com"}},
		{name: "Unicode name at limit", input: maintainerInput{Name: strings.Repeat("\u754c", 200), Email: "alex@example.com"}},
		{name: "Unicode name over limit", input: maintainerInput{Name: strings.Repeat("\u754c", 201), Email: "alex@example.com"}, wantFields: []string{"name"}},
		{name: "Unicode whitespace", input: maintainerInput{Name: "\u2003", Email: "alex@example.com"}, wantFields: []string{"name"}},
		{name: "missing fields", wantFields: []string{"name", "email"}},
		{name: "email at limit", input: maintainerInput{Name: "Alex", Email: longestEmail}},
		{name: "email over limit", input: maintainerInput{Name: "Alex", Email: longestEmail + "d"}, wantFields: []string{"email"}},
		{name: "display name", input: maintainerInput{Name: "Alex", Email: "Alex <alex@example.com>"}, wantFields: []string{"email"}},
		{name: "angle address", input: maintainerInput{Name: "Alex", Email: "<alex@example.com>"}, wantFields: []string{"email"}},
		{name: "leading whitespace", input: maintainerInput{Name: "Alex", Email: " alex@example.com"}, wantFields: []string{"email"}},
		{name: "trailing whitespace", input: maintainerInput{Name: "Alex", Email: "alex@example.com "}, wantFields: []string{"email"}},
		{name: "comment", input: maintainerInput{Name: "Alex", Email: "alex@example.com (Alex)"}, wantFields: []string{"email"}},
		{name: "quoted local part", input: maintainerInput{Name: "Alex", Email: `"alex"@example.com`}},
		{name: "multiple emails", input: maintainerInput{Name: "Alex", Email: "alex@example.com,sam@example.com"}, wantFields: []string{"email"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fields := validateMaintainer(tt.input)
			if len(fields) != len(tt.wantFields) {
				t.Fatalf("field errors = %v, want %v", fields, tt.wantFields)
			}
			for _, field := range tt.wantFields {
				if fields[field] == "" {
					t.Errorf("missing error for %s", field)
				}
			}
		})
	}
}

func TestMaintainerRouteMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewRouter(stubDatabase{}).ServeHTTP(response, httptest.NewRequest(method, "/api/v1/maintainers", nil))
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "POST" {
				t.Fatalf("status = %d, Allow = %q", response.Code, response.Header().Get("Allow"))
			}
		})
	}
}

func TestCreateMaintainerPropagatesContext(t *testing.T) {
	t.Parallel()
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "request")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/maintainers", strings.NewReader(`{"name":"Alex","email":"alex@example.com"}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	NewRouter(stubDatabase{create: func(ctx context.Context, name, email string) (storage.Maintainer, error) {
		if ctx.Value(contextKey{}) != "request" {
			t.Error("storage must receive the request context")
		}
		return storage.Maintainer{ID: 1, Name: name, Email: email}, nil
	}}).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", response.Code)
	}
}
