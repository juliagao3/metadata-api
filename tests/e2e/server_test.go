package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMetadataAPI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "metadata-api")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/server")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build server: %v\n%s", err, output)
	}

	t.Run("CRUD relationships and restart persistence", func(t *testing.T) {
		database := filepath.Join(t.TempDir(), "metadata data #1.db")
		server := startServer(t, binary, database)
		for _, path := range []string{"/api/v1/maintainers", "/api/v1/applications"} {
			server.request(t, "GET", path, "", 200).expectJSON(t, `[]`)
		}

		maintainerBody := `{"name":"Alice","email":"alice@example.com"}`
		created := server.request(t, "POST", "/api/v1/maintainers", maintainerBody, 201, "application/json; charset=utf-8")
		maintainerID, maintainerPath := created.resource(t, "/api/v1/maintainers")
		originalMaintainer := fmt.Sprintf(`{"id":%d,"name":"Alice","email":"alice@example.com"}`, maintainerID)
		created.expectJSON(t, originalMaintainer)
		server.request(t, "GET", maintainerPath, "", 200).expectJSON(t, originalMaintainer)

		duplicate := server.request(t, "POST", "/api/v1/maintainers", maintainerBody, 201)
		otherID, otherPath := duplicate.resource(t, "/api/v1/maintainers")
		if otherID <= maintainerID {
			t.Fatalf("second maintainer ID %d must be greater than %d", otherID, maintainerID)
		}
		otherMaintainer := fmt.Sprintf(`{"id":%d,"name":"Alice","email":"alice@example.com"}`, otherID)
		duplicate.expectJSON(t, otherMaintainer)

		updatedMaintainer := fmt.Sprintf(`{"id":%d,"name":"Alice Updated","email":"updated@example.com"}`, maintainerID)
		for range 2 {
			server.request(t, "PUT", maintainerPath, `{"name":"Alice Updated","email":"updated@example.com"}`, 200).
				expectJSON(t, updatedMaintainer)
		}
		server.request(t, "GET", "/api/v1/maintainers", "", 200).
			expectJSON(t, fmt.Sprintf(`[%s,%s]`, updatedMaintainer, otherMaintainer))

		fields := `"title":"O'Reilly Metadata","version":"1.0","company":"Example","website":"https://example.com","source":"https://example.com/source","license":"MIT","description":"Original metadata"`
		applicationBody := fmt.Sprintf(`{%s,"maintainer_id":%d}`, fields, maintainerID)
		created = server.request(t, "POST", "/api/v1/applications", applicationBody, 201)
		applicationID, applicationPath := created.resource(t, "/api/v1/applications")
		originalApplication := fmt.Sprintf(`{"id":%d,%s,"maintainer_id":%d}`, applicationID, fields, maintainerID)
		created.expectJSON(t, originalApplication)
		server.request(t, "GET", applicationPath, "", 200).expectJSON(t, originalApplication)

		duplicate = server.request(t, "POST", "/api/v1/applications", applicationBody, 201)
		duplicateID, duplicatePath := duplicate.resource(t, "/api/v1/applications")
		if duplicateID <= applicationID {
			t.Fatalf("second application ID %d must be greater than %d", duplicateID, applicationID)
		}
		duplicateApplication := fmt.Sprintf(`{"id":%d,%s,"maintainer_id":%d}`, duplicateID, fields, maintainerID)
		duplicate.expectJSON(t, duplicateApplication)
		server.request(t, "GET", "/api/v1/applications", "", 200).
			expectJSON(t, fmt.Sprintf(`[%s,%s]`, originalApplication, duplicateApplication))

		for _, path := range []string{"/readyz", "/api/v1/maintainers", maintainerPath, "/api/v1/applications", applicationPath} {
			server.request(t, "HEAD", path, "", 200)
		}

		updatedApplication := fmt.Sprintf(`{"id":%d,"title":"Updated","version":"2.0","company":"","website":"","source":"","license":"","description":"","maintainer_id":%d}`, applicationID, maintainerID)
		for range 2 {
			server.request(t, "PUT", applicationPath, `{"title":"Updated","version":"2.0"}`, 200).
				expectJSON(t, updatedApplication)
		}
		server.request(t, "PUT", applicationPath,
			fmt.Sprintf(`{"title":"Reassigned","version":"3.0","maintainer_id":%d}`, otherID), 400).
			expectError(t, "invalid_request", "")
		server.request(t, "GET", applicationPath, "", 200).expectJSON(t, updatedApplication)
		server.request(t, "GET", duplicatePath, "", 200).expectJSON(t, duplicateApplication)
		server.request(t, "DELETE", maintainerPath, "", 409).expectError(t, "maintainer_in_use", "")
		server.request(t, "GET", maintainerPath, "", 200).expectJSON(t, updatedMaintainer)

		server.request(t, "DELETE", duplicatePath, "", 204)
		server.request(t, "DELETE", duplicatePath, "", 404).expectError(t, "not_found", "")
		server.request(t, "DELETE", maintainerPath, "", 409).expectError(t, "maintainer_in_use", "")
		server.request(t, "GET", applicationPath, "", 200).expectJSON(t, updatedApplication)
		server.request(t, "DELETE", otherPath, "", 204)

		// Kill rather than signal so the restart also works on Windows.
		server.stop(t)
		server = startServer(t, binary, database)
		server.request(t, "GET", maintainerPath, "", 200).expectJSON(t, updatedMaintainer)
		server.request(t, "GET", applicationPath, "", 200).expectJSON(t, updatedApplication)
		server.request(t, "GET", "/api/v1/maintainers", "", 200).expectJSON(t, "["+updatedMaintainer+"]")
		server.request(t, "GET", "/api/v1/applications", "", 200).expectJSON(t, "["+updatedApplication+"]")
		server.request(t, "GET", otherPath, "", 404).expectError(t, "not_found", "")
		server.request(t, "GET", duplicatePath, "", 404).expectError(t, "not_found", "")
		server.request(t, "PUT", otherPath, maintainerBody, 404).expectError(t, "not_found", "")
		server.request(t, "PUT", duplicatePath, `{"title":"Missing","version":"1.0"}`, 404).
			expectError(t, "not_found", "")
		server.request(t, "DELETE", maintainerPath, "", 409).expectError(t, "maintainer_in_use", "")

		server.request(t, "DELETE", applicationPath, "", 204)
		server.request(t, "GET", maintainerPath, "", 200).expectJSON(t, updatedMaintainer)
		server.request(t, "DELETE", maintainerPath, "", 204)
		server.request(t, "DELETE", maintainerPath, "", 404).expectError(t, "not_found", "")
		for _, path := range []string{"/api/v1/maintainers", "/api/v1/applications"} {
			server.request(t, "GET", path, "", 200).expectJSON(t, `[]`)
		}
	})

	t.Run("validation and routing", func(t *testing.T) {
		server := startServer(t, binary, filepath.Join(t.TempDir(), "metadata.db"))
		maintainerBody := `{"name":"Alice","email":"alice@example.com"}`
		created := server.request(t, "POST", "/api/v1/maintainers", maintainerBody, 201)
		maintainerID, maintainerPath := created.resource(t, "/api/v1/maintainers")
		applicationBody := fmt.Sprintf(`{"title":"Metadata","version":"1.0","maintainer_id":%d}`, maintainerID)
		created = server.request(t, "POST", "/api/v1/applications", applicationBody, 201)
		applicationID, applicationPath := created.resource(t, "/api/v1/applications")

		for _, route := range []struct {
			method, path, field string
		}{
			{"POST", "/api/v1/maintainers", "name"},
			{"PUT", maintainerPath, "name"},
			{"POST", "/api/v1/applications", "title"},
			{"PUT", applicationPath, "title"},
		} {
			t.Run(route.method+" "+route.path, func(t *testing.T) {
				for _, test := range []struct {
					name, body, contentType, code, field string
					status                               int
				}{
					{"empty body", "", "application/json", "invalid_request", "", 400},
					{"malformed JSON", "{", "application/json", "invalid_request", "", 400},
					{"multiple objects", "{} {}", "application/json", "invalid_request", "", 400},
					{"array", "[]", "application/json", "invalid_request", "", 400},
					{"null body", "null", "application/json", "invalid_request", "", 400},
					{"unknown field", `{"unknown":"value"}`, "application/json", "invalid_request", "", 400},
					{"capitalized field", fmt.Sprintf(`{%q:"value"}`, strings.ToUpper(route.field)), "application/json", "invalid_request", "", 400},
					{"null field", fmt.Sprintf(`{%q:null}`, route.field), "application/json", "invalid_request", "", 400},
					{"incorrect type", fmt.Sprintf(`{%q:123}`, route.field), "application/json", "invalid_request", "", 400},
					{"invalid UTF-8", fmt.Sprintf(`{%q:"`+"\xff"+`"}`, route.field), "application/json", "invalid_request", "", 400},
					{"missing required fields", "{}", "application/json", "validation_failed", route.field, 422},
					{"oversized body", `{"description":"` + strings.Repeat("x", 65536) + `"}`, "application/json", "request_too_large", "", 413},
					{"missing content type", "{}", "", "unsupported_media_type", "", 415},
					{"unsupported content type", "{}", "text/plain", "unsupported_media_type", "", 415},
				} {
					t.Run(test.name, func(t *testing.T) {
						server.request(t, route.method, route.path, test.body, test.status, test.contentType).
							expectError(t, test.code, test.field)
					})
				}
			})
		}

		for _, test := range []struct {
			name, method, path, body, code, field, allow string
			status                                       int
		}{
			{"invalid email", "POST", "/api/v1/maintainers", `{"name":"Alice","email":"invalid"}`, "validation_failed", "email", "", 422},
			{"invalid updated email", "PUT", maintainerPath, `{"name":"Alice","email":"invalid"}`, "validation_failed", "email", "", 422},
			{"missing creator", "POST", "/api/v1/applications", `{"title":"App","version":"1"}`, "validation_failed", "maintainer_id", "", 422},
			{"zero creator", "POST", "/api/v1/applications", `{"title":"App","version":"1","maintainer_id":0}`, "validation_failed", "maintainer_id", "", 422},
			{"negative creator", "POST", "/api/v1/applications", `{"title":"App","version":"1","maintainer_id":-1}`, "validation_failed", "maintainer_id", "", 422},
			{"nonexistent creator", "POST", "/api/v1/applications", `{"title":"App","version":"1","maintainer_id":9223372036854775807}`, "validation_failed", "maintainer_id", "", 422},
			{"incorrect creator type", "POST", "/api/v1/applications", `{"title":"App","version":"1","maintainer_id":"1"}`, "invalid_request", "", "", 400},
			{"creator overflow", "POST", "/api/v1/applications", `{"title":"App","version":"1","maintainer_id":9223372036854775808}`, "invalid_request", "", "", 400},
			{"immutable creator", "PUT", applicationPath, applicationBody, "invalid_request", "", "", 400},
			{"immutable application ID", "PUT", applicationPath, `{"title":"App","version":"1","id":1}`, "invalid_request", "", "", 400},
			{"immutable maintainer ID", "PUT", maintainerPath, `{"name":"Alice","email":"alice@example.com","id":1}`, "invalid_request", "", "", 400},
			{"maintainer query", "GET", "/api/v1/maintainers?name=Alice", "", "invalid_request", "", "", 400},
			{"application bare query", "GET", "/api/v1/applications?", "", "invalid_request", "", "", 400},
			{"unknown route", "GET", "/missing", "", "not_found", "", "", 404},
			{"missing maintainer", "GET", "/api/v1/maintainers/9223372036854775807", "", "not_found", "", "", 404},
			{"missing application", "GET", "/api/v1/applications/9223372036854775807", "", "not_found", "", "", 404},
			{"readiness method", "POST", "/readyz", "", "method_not_allowed", "", "GET, HEAD", 405},
			{"maintainer collection method", "DELETE", "/api/v1/maintainers", "", "method_not_allowed", "", "GET, HEAD, POST", 405},
			{"application collection method", "DELETE", "/api/v1/applications", "", "method_not_allowed", "", "GET, HEAD, POST", 405},
			{"maintainer item method", "POST", maintainerPath, maintainerBody, "method_not_allowed", "", "GET, HEAD, PUT, DELETE", 405},
			{"application item method", "POST", applicationPath, applicationBody, "method_not_allowed", "", "GET, HEAD, PUT, DELETE", 405},
		} {
			t.Run(test.name, func(t *testing.T) {
				response := server.request(t, test.method, test.path, test.body, test.status)
				response.expectError(t, test.code, test.field)
				if got := response.header.Get("Allow"); got != test.allow {
					t.Errorf("Allow = %q, want %q", got, test.allow)
				}
			})
		}
		for _, collection := range []string{"/api/v1/maintainers", "/api/v1/applications"} {
			for _, id := range []string{"0", "-1", "+1", "abc", "9223372036854775808"} {
				t.Run(collection+"/"+id, func(t *testing.T) {
					server.request(t, "GET", collection+"/"+id, "", 400).expectError(t, "invalid_request", "")
				})
			}
		}

		server.request(t, "GET", "/api/v1/maintainers", "", 200).
			expectJSON(t, fmt.Sprintf(`[{"id":%d,"name":"Alice","email":"alice@example.com"}]`, maintainerID))
		server.request(t, "GET", "/api/v1/applications", "", 200).
			expectJSON(t, fmt.Sprintf(`[{"id":%d,"title":"Metadata","version":"1.0","company":"","website":"","source":"","license":"","description":"","maintainer_id":%d}]`, applicationID, maintainerID))
	})
}

type testServer struct {
	url     string
	client  *http.Client
	cmd     *exec.Cmd
	done    chan error
	log     *os.File
	stopped bool
}

func startServer(t *testing.T, binary, database string) *testServer {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &testServer{
		client: &http.Client{
			Timeout:   3 * time.Second,
			Transport: &http.Transport{},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cmd:  exec.Command(binary, "-addr", "127.0.0.1:0", "-db", database),
		done: make(chan error, 1),
		log:  logFile,
	}
	server.cmd.Stdout = logFile
	server.cmd.Stderr = logFile
	if err := server.cmd.Start(); err != nil {
		if closeErr := logFile.Close(); closeErr != nil {
			t.Errorf("close server log: %v", closeErr)
		}
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		server.stop(t)
		if t.Failed() {
			logs, err := os.ReadFile(logPath)
			if err != nil {
				t.Errorf("read server log: %v", err)
			}
			t.Logf("server log (%s):\n%s", logPath, logs)
		}
	})
	go func() {
		server.done <- server.cmd.Wait()
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastError error
	for {
		logs, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read startup log: %v", err)
		}
		_, address, found := strings.Cut(string(logs), "listening on ")
		line, _, complete := strings.Cut(address, "\n")
		if found && complete {
			server.url = strings.TrimSpace(line)
			request, err := http.NewRequestWithContext(ctx, "GET", server.url+"/readyz", nil)
			if err != nil {
				t.Fatalf("create readiness request: %v", err)
			}
			response, err := server.client.Do(request)
			lastError = err
			if err == nil {
				body, readErr := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil {
					lastError = errors.Join(readErr, closeErr)
				} else if response.StatusCode == 200 {
					apiResponse{body: body}.expectJSON(t, `{"status":"ready"}`)
					return server
				} else {
					lastError = fmt.Errorf("readiness status %d: %s", response.StatusCode, body)
				}
			}
		}
		select {
		case err := <-server.done:
			server.done <- err
			t.Fatalf("server exited before readiness: %v", err)
		case <-ctx.Done():
			t.Fatalf("server did not become ready: %v (last request: %v)", ctx.Err(), lastError)
		case <-ticker.C:
		}
	}
}

func (server *testServer) stop(t *testing.T) {
	t.Helper()
	if server.stopped {
		return
	}
	server.stopped = true
	server.client.CloseIdleConnections()
	select {
	case err := <-server.done:
		t.Errorf("server exited unexpectedly: %v", err)
	default:
		if err := server.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("kill server: %v", err)
		}
		select {
		case err := <-server.done:
			var exitError *exec.ExitError
			if err != nil && !errors.As(err, &exitError) {
				t.Errorf("wait for server exit: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("server did not exit after being killed")
		}
	}
	if err := server.log.Close(); err != nil {
		t.Errorf("close server log: %v", err)
	}
}

type apiResponse struct {
	header http.Header
	body   []byte
}

func (server *testServer) request(t *testing.T, method, path, body string, status int, contentTypes ...string) apiResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, server.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	contentType := "application/json"
	if len(contentTypes) > 0 {
		contentType = contentTypes[0]
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := server.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read %s %s: %v", method, path, errors.Join(readErr, closeErr))
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: status = %d, want %d; body = %s", method, path, response.StatusCode, status, data)
	}
	wantContentType := "application/json"
	if status == http.StatusNoContent {
		wantContentType = ""
	}
	if got := response.Header.Get("Content-Type"); got != wantContentType {
		t.Errorf("%s %s: Content-Type = %q, want %q", method, path, got, wantContentType)
	}
	if (method == "HEAD" || status == http.StatusNoContent) && len(data) != 0 {
		t.Errorf("%s %s: expected no body, got %s", method, path, data)
	}
	return apiResponse{header: response.Header, body: data}
}

func (response apiResponse) resource(t *testing.T, collection string) (int64, string) {
	t.Helper()
	var resource struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(response.body, &resource); err != nil {
		t.Fatalf("decode created resource: %v; body = %s", err, response.body)
	}
	if resource.ID <= 0 {
		t.Fatalf("created ID must be positive, got %d", resource.ID)
	}
	path := fmt.Sprintf("%s/%d", collection, resource.ID)
	if got := response.header.Get("Location"); got != path {
		t.Errorf("Location = %q, want %q", got, path)
	}
	return resource.ID, path
}

func (response apiResponse) expectJSON(t *testing.T, want string) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(response.body, &actual); err != nil {
		t.Fatalf("invalid JSON response: %v; body = %s", err, response.body)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatalf("invalid expected JSON: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("JSON response = %s\nwant = %s", response.body, want)
	}
}

func (response apiResponse) expectError(t *testing.T, code, field string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("invalid JSON error: %v; body = %s", err, response.body)
	}
	if envelope.Error.Code != code || envelope.Error.Message == "" {
		t.Errorf("error must have code %q and a message; got %s", code, response.body)
	}
	if field != "" && envelope.Error.Fields[field] == "" {
		t.Errorf("error must describe field %q; got %s", field, response.body)
	}
}
