package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"upfluence-stream-analyzer/config"
	"upfluence-stream-analyzer/internal/handlers"
	"upfluence-stream-analyzer/internal/services"
)

// New creates and initializes a new application instance with all dependencies
func New(cfg *config.Config, logger *slog.Logger) *application {
	// Initialize services with dependency injection
	streamClient := services.NewStreamClient(cfg.GetStreamURL(), logger)
	streamAnalyzer := services.NewStreamAnalyzer(streamClient, logger)
	streamAnalysisHandler := handlers.NewStreamAnalysisHandler(streamAnalyzer, logger)

	// Setup HTTP router.
	// Accept only HTTP GET requests for the '/analysis' endpoint.
	// Return a 404 response for all other routes.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /analysis", streamAnalysisHandler.HandleAnalysis)

	// Configure HTTP server
	server := &http.Server{
		Addr:    cfg.GetServerAddress(),
		Handler: mux,
	}

	return &application{
		config: cfg,
		logger: logger,
		server: server,
	}
}

// Run starts the HTTP server and handles graceful shutdown.
// Uses BaseContext to propagate cancellation to all active requests when shutdown is initiated.
func (app *application) Run() error {
	// Create a context that will be cancelled when shutdown is initiated.
	// This context is used as the BaseContext for the HTTP server.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set BaseContext for graceful shutdown propagation.
	// This is called for each incoming request to create the request's context.
	// All requests inherit it and they will be notified when we call cancel() during shutdown.
	app.server.BaseContext = func(_ net.Listener) context.Context {
		return ctx
	}

	// Channel to communicate shutdown errors from the shutdown goroutine
	shutdownErrCh := make(chan error)

	// Handle graceful shutdown in a separate goroutine
	go func() {
		// Channel to receive OS signals for graceful shutdown
		signalCh := make(chan os.Signal, 1)
		signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

		// Block until we receive a shutdown signal
		sig := <-signalCh
		app.logger.Info("Shutdown signal received", "signal", sig.String())

		// Cancel the base context (this signals all active requests that shutdown is happening)
		cancel()

		// Create a context with timeout for the shutdown process itself
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()

		// Attempt graceful shutdown
		app.logger.Info("Shutting down server gracefully...")
		err := app.server.Shutdown(shutdownCtx)
		if err != nil {
			shutdownErrCh <- err
			return
		}

		app.logger.Info("Server stopped gracefully")
		shutdownErrCh <- nil
	}()

	// Start the server (this blocks until the server is shut down)
	app.logger.Info("HTTP server starting", "address", app.config.GetServerAddress())
	err := app.server.ListenAndServe()

	// ListenAndServe always returns an error.
	// After Shutdown or Close, the error is ErrServerClosed.
	// This is expected and not an error condition.
	if !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server failed: %w", err)
	}

	// Wait for the shutdown goroutine to finish and report any errors
	err = <-shutdownErrCh
	if err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	return nil
}
