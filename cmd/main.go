package main

import (
	"log/slog"
	"os"
	"upfluence-stream-analyzer/config"
)

type App struct {
	Config config.Config
	Logger *slog.Logger
}

func main() {
	// Create the logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var cfg config.Config

	// Get the configuration
	err := config.Load(&cfg)
	if err != nil {
		logger.Error("failed to load config", "err", err.Error())
		os.Exit(1)
	}
}
