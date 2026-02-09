package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AccountConfig holds email account configuration
type AccountConfig struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	FromName string `json:"from_name,omitempty"`

	// IMAP settings
	IMAP struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password,omitempty"`

		// SSL or implicit TLS
		SSL bool `json:"ssl"`
		// Start TLS
		StartTLS bool `json:"starttls"`
	} `json:"imap"`

	// POP3 settings
	POP3 struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password,omitempty"`

		SSL      bool `json:"ssl"`
		StartTLS bool `json:"starttls"`
	} `json:"pop3"`

	// SMTP settings
	SMTP struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password,omitempty"`

		SSL      bool `json:"ssl"`
		StartTLS bool `json:"starttls"`
	} `json:"smtp"`
}

// Config holds the application configuration
type Config struct {
	Accounts     []AccountConfig `json:"accounts"`
	DefaultAccount string        `json:"default_account,omitempty"`
}

// LoadConfig loads configuration from a file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// SaveConfig saves configuration to a file
func SaveConfig(path string, cfg *Config) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetDefaultConfigPath returns the default configuration file path
func GetDefaultConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(homeDir, ".emx-mail", "config.json"), nil
}

// GetAccount returns an account by name or email
func (c *Config) GetAccount(identifier string) (*AccountConfig, error) {
	// If identifier is empty, use default account
	if identifier == "" {
		if c.DefaultAccount != "" {
			identifier = c.DefaultAccount
		} else if len(c.Accounts) > 0 {
			// Use first account as default
			return &c.Accounts[0], nil
		} else {
			return nil, fmt.Errorf("no accounts configured")
		}
	}

	// Search by name or email
	for i := range c.Accounts {
		acc := &c.Accounts[i]
		if acc.Name == identifier || acc.Email == identifier {
			return acc, nil
		}
	}

	return nil, fmt.Errorf("account not found: %s", identifier)
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if len(c.Accounts) == 0 {
		return fmt.Errorf("no accounts configured")
	}

	for i, acc := range c.Accounts {
		if acc.Name == "" {
			return fmt.Errorf("account %d: name is required", i)
		}
		if acc.Email == "" {
			return fmt.Errorf("account %s: email is required", acc.Name)
		}

		// At least one of IMAP or POP3 should be configured
		if acc.IMAP.Host == "" && acc.POP3.Host == "" {
			return fmt.Errorf("account %s: at least one of IMAP or POP3 must be configured", acc.Name)
		}
	}

	return nil
}

// ExampleConfig returns an example configuration for "init"
func ExampleConfig() *Config {
	return &Config{
		Accounts: []AccountConfig{
			{
				Name:     "Example Account",
				Email:    "user@example.com",
				FromName: "Your Name",
				IMAP: struct {
					Host     string `json:"host"`
					Port     int    `json:"port"`
					Username string `json:"username"`
					Password string `json:"password,omitempty"`
					SSL      bool   `json:"ssl"`
					StartTLS bool   `json:"starttls"`
				}{
					Host:     "imap.example.com",
					Port:     993,
					Username: "user@example.com",
					SSL:      true,
				},
				SMTP: struct {
					Host     string `json:"host"`
					Port     int    `json:"port"`
					Username string `json:"username"`
					Password string `json:"password,omitempty"`
					SSL      bool   `json:"ssl"`
					StartTLS bool   `json:"starttls"`
				}{
					Host:     "smtp.example.com",
					Port:     587,
					Username: "user@example.com",
					StartTLS: true,
				},
			},
		},
	}
}
