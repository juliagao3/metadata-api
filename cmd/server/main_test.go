package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type logWriterFunc func([]byte) (int, error)

func (f logWriterFunc) Write(data []byte) (int, error) {
	return f(data)
}

func TestRunStartsAndStops(t *testing.T) {
	// Redirecting the process-wide logger requires a non-parallel test.
	started := make(chan string, 1)
	original := log.Writer()
	log.SetOutput(logWriterFunc(func(data []byte) (int, error) {
		if _, address, found := strings.Cut(string(data), "listening on "); found {
			started <- strings.TrimSpace(address)
		}
		return len(data), nil
	}))
	t.Cleanup(func() { log.SetOutput(original) })

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var runErr error
	path := filepath.Join(t.TempDir(), "metadata.db")
	go func() {
		runErr = run(ctx, "127.0.0.1:0", path)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if runErr != nil {
				t.Errorf("run returned an error: %v", runErr)
			}
		case <-time.After(10 * time.Second):
			t.Error("run did not shut down")
		}
	})

	var address string
	select {
	case address = <-started:
	case <-done:
		t.Fatalf("server exited before listening: %v", runErr)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start listening")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	response, err := client.Get(address + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"status":"ready"}` {
		t.Fatalf("readiness status = %d, body = %s", response.StatusCode, body)
	}
}

func TestRunReportsStartupErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		address string
		path    string
		want    string
	}{
		{name: "database", address: "127.0.0.1:0", path: t.TempDir(), want: "SQLite"},
		{name: "listener", address: "127.0.0.1:invalid", path: filepath.Join(t.TempDir(), "metadata.db"), want: "listen"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := run(t.Context(), tt.address, tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want message containing %q", err, tt.want)
			}
		})
	}
}

func TestServeDrainsRequestsOnShutdown(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	shutdownStarted := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			select {
			case <-release:
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
			}
		}),
	}
	defer server.Close()
	server.RegisterOnShutdown(func() { close(shutdownStarted) })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	serveResult := make(chan error, 1)
	go func() { serveResult <- serve(ctx, server, listener) }()

	requestResult := make(chan error, 1)
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	go func() {
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			if closeErr := response.Body.Close(); err == nil {
				err = closeErr
			}
			if response.StatusCode != http.StatusOK {
				t.Errorf("request status = %d, want 200", response.StatusCode)
			}
		}
		requestResult <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach the server")
	}
	cancel()
	select {
	case <-shutdownStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not start")
	}
	select {
	case err := <-serveResult:
		t.Fatalf("server exited before the request completed: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-requestResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish")
	}
	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := serve(t.Context(), &http.Server{}, listener); err == nil {
		t.Fatal("expected an error from a closed listener")
	}
}

func TestServeForcesCloseAfterShutdownDeadline(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	entered := make(chan struct{})
	canceled := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
			close(canceled)
		}),
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	serveResult := make(chan error, 1)
	go func() { serveResult <- serve(ctx, server, listener) }()

	client := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	requestResult := make(chan error, 1)
	go func() {
		response, err := client.Get("http://" + listener.Addr().String())
		if response != nil {
			err = errors.Join(err, response.Body.Close())
		}
		requestResult <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach the server")
	}
	cancel()
	select {
	case err := <-serveResult:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error = %v, want deadline exceeded", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not force the active connection closed")
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("forced close did not cancel the handler's request context")
	}
	select {
	case err := <-requestResult:
		if err == nil {
			t.Fatal("interrupted request unexpectedly succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client remained blocked after forced shutdown")
	}
}
