package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Config struct {
	Stream StreamConfig `json:"stream"`
	Server ServerConfig `json:"server"`
}

type StreamConfig struct {
	URL string `json:"url"`
}

type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func Load(cfg *Config) error {
	// Open the configuration file
	cfgFile, err := os.Open("./config/config.json")
	if err != nil {
		return fmt.Errorf("failed to open config: %w", err)
	}
	defer cfgFile.Close()

	// Read the configuration file
	cfgBytes, err := io.ReadAll(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Unmarshal the configuration into the cfg struct
	if err = json.Unmarshal(cfgBytes, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("failed to validate config: %w", err)
	}

	return nil
}

// GetServerAddress returns the HTTP server address in host:port format
func (c *Config) GetServerAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// GetStreamURL returns the stream URL
func (c *Config) GetStreamURL() string {
	return c.Stream.URL
}
