package main

import (
	"log/slog"
	"net/http"
	"os"

	"upfluence-stream-analyzer/config"
)

// application holds the application configuration and dependencies
type application struct {
	config *config.Config
	logger *slog.Logger
	server *http.Server
}

func main() {
	// Initialize the logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	logger.Info("Starting application")

	// Load the config
	var cfg config.Config
	err := config.Load(&cfg)
	if err != nil {
		logger.Error("Failed to load config", "err", err.Error())
		os.Exit(1)
	}

	// Create and initialize the application
	app := New(&cfg, logger)

	// Run the application
	if err := app.Run(); err != nil {
		logger.Error("Failed to run application", "err", err.Error())
		os.Exit(1)
	}

	logger.Info("Application stopped successfully")
}
