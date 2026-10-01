package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/o-ga09/gourmet-mcp/internal/jev"
	"github.com/o-ga09/gourmet-mcp/internal/mcpserver"
	"github.com/o-ga09/gourmet-mcp/internal/places"
	"github.com/o-ga09/gourmet-mcp/internal/search"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	placesKey := os.Getenv("GOOGLE_PLACES_API_KEY")
	jevKey := os.Getenv("TYPESAFE_API_KEY")
	if placesKey == "" || jevKey == "" {
		return errors.New("GOOGLE_PLACES_API_KEY and TYPESAFE_API_KEY are required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	svc := search.NewService(places.NewClient(placesKey), jev.NewClient(jevKey))
	server := mcpserver.New(svc)

	mux := http.NewServeMux()
	// Pod の再起動でセッションが切れないよう stateless で動かす
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
