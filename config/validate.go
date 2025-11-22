package config

import "fmt"

// Validate validates the entire configuration
func (c *Config) Validate() error {
	checks := []func(*Config) error{
		validateStreamConfig,
		validateServerConfig,
	}

	for _, check := range checks {
		if err := check(c); err != nil {
			return err
		}
	}

	return nil
}

func validateStreamConfig(cfg *Config) error {
	if cfg.Stream == (StreamConfig{}) {
		return fmt.Errorf("stream config is empty")
	}

	return nil
}

func validateServerConfig(cfg *Config) error {
	if cfg.Server == (ServerConfig{}) {
		return fmt.Errorf("server config is empty")
	}

	if cfg.Server.Host == "" {
		return fmt.Errorf("server host is empty")
	}

	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid server port, must be between 1 and 65535, got %d", cfg.Server.Port)
	}

	return nil
}
