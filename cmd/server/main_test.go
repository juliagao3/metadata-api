package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
