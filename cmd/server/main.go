package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/juliagao3/metadata-api/internal/api"
	"github.com/juliagao3/metadata-api/internal/storage"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	databasePath := flag.String("db", "metadata.db", "SQLite database file (parent directory must exist)")
	flag.Parse()
	if flag.NArg() != 0 {
		log.Print("unexpected positional arguments; use -help for usage")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *address, *databasePath); err != nil {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, address, databasePath string) (err error) {
	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	db, err := storage.Open(startupCtx, databasePath)
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, db.Close())
	}()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", address, err)
	}
	server := &http.Server{
		Handler:           api.NewRouter(db),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("listening on http://%s", listener.Addr())
	return serve(ctx, server, listener)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		shutdownErr := server.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, server.Close())
		}
		serveErr := <-serveResult
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(shutdownErr, serveErr)
	}
}
