package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// EnvConfigJSONPath is the env var that points to the JSON config file
	// used when emx-config is not available.
	EnvConfigJSONPath = "EMX_MAIL_CONFIG_JSON"
)

// ProtocolSettings holds connection settings common to IMAP, POP3 and SMTP.
type ProtocolSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`

	// SSL enables implicit TLS (connect directly over TLS).
	SSL bool `json:"ssl"`
	// StartTLS enables opportunistic TLS upgrade after connecting in plaintext.
	StartTLS bool `json:"starttls"`
}

// AccountConfig holds email account configuration
//
// NOTE: This structure mirrors the emx-config nested config schema.
// See ExampleRootConfig for the expected JSON shape.
type AccountConfig struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	FromName string `json:"from_name,omitempty"`

	IMAP ProtocolSettings `json:"imap"`
	POP3 ProtocolSettings `json:"pop3"`
	SMTP ProtocolSettings `json:"smtp"`

	// Watch settings
	Watch *WatchConfig `json:"watch,omitempty"`
}

// Domain returns the domain part of the account email address.
// Returns "localhost" if no domain can be extracted.
func (a *AccountConfig) Domain() string {
	if idx := strings.Index(a.Email, "@"); idx >= 0 {
		return a.Email[idx+1:]
	}
	return "localhost"
}

// WatchConfig holds watch mode configuration
type WatchConfig struct {
	Folder       string `json:"folder,omitempty"`        // Folder to watch, default "INBOX"
	HandlerCmd   string `json:"handler_cmd,omitempty"`   // Handler command (e.g., "/path/to/handler --opt")
	KeepAlive    int    `json:"keep_alive,omitempty"`    // Keep-alive interval in seconds, default 30
	PollInterval int    `json:"poll_interval,omitempty"` // Poll interval in seconds, default 30
	MaxRetries   int    `json:"max_retries,omitempty"`   // Max retry attempts, default 5
}

// Config holds the application configuration
//
// accounts is a map keyed by account name.
// default_account selects the account when none is specified.
type Config struct {
	Accounts       map[string]AccountConfig `json:"accounts"`
	DefaultAccount string                   `json:"default_account,omitempty"`
}

// RootConfig wraps the app config to align with emx-config list --json output.
type RootConfig struct {
	Mail Config `json:"mail"`
}

// HasEmxConfig returns true when the emx-config CLI is available in PATH.
func HasEmxConfig() bool {
	_, err := exec.LookPath("emx-config")
	return err == nil
}

// LoadConfig loads configuration based on the new emx-config-first mechanism.
//
// 1) If emx-config exists: read config from `emx-config list --json`.
// 2) Otherwise: read config from the JSON file specified by EnvConfigJSONPath.
func LoadConfig() (*Config, error) {
	if HasEmxConfig() {
		return loadFromEmxConfig()
	}
	return loadFromEnvJSON()
}

// LoadConfigFile loads configuration from a JSON file path.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return parseRootConfig(data)
}

// SaveConfig saves configuration to a JSON file path.
func SaveConfig(path string, root *RootConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetEnvConfigPath returns the config file path from EnvConfigJSONPath.
func GetEnvConfigPath() (string, error) {
	path := strings.TrimSpace(os.Getenv(EnvConfigJSONPath))
	if path == "" {
		return "", fmt.Errorf("%s is not set", EnvConfigJSONPath)
	}
	return path, nil
}

// GetAccount returns an account by name or email.
func (c *Config) GetAccount(identifier string) (*AccountConfig, error) {
	if c.Accounts == nil || len(c.Accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured")
	}

	if identifier == "" {
		if c.DefaultAccount != "" {
			identifier = c.DefaultAccount
		} else {
			// Deterministic fallback to the first key
			keys := make([]string, 0, len(c.Accounts))
			for k := range c.Accounts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			identifier = keys[0]
		}
	}

	// Direct name match (map key)
	if acc, ok := c.Accounts[identifier]; ok {
		return &acc, nil
	}

	// Search by name or email fields
	for name, acc := range c.Accounts {
		if acc.Name == identifier || acc.Email == identifier {
			if acc.Name == "" {
				acc.Name = name
			}
			return &acc, nil
		}
	}

	return nil, fmt.Errorf("account not found: %s", identifier)
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Accounts == nil || len(c.Accounts) == 0 {
		return fmt.Errorf("no accounts configured")
	}

	for name, acc := range c.Accounts {
		if acc.Name == "" {
			acc.Name = name
		}
		if acc.Email == "" {
			return fmt.Errorf("account %s: email is required", acc.Name)
		}

		// At least one of IMAP or POP3 should be configured
		if acc.IMAP.Host == "" && acc.POP3.Host == "" {
			return fmt.Errorf("account %s: at least one of IMAP or POP3 must be configured", acc.Name)
		}
	}

	if c.DefaultAccount != "" {
		if _, ok := c.Accounts[c.DefaultAccount]; !ok {
			return fmt.Errorf("default_account not found: %s", c.DefaultAccount)
		}
	}

	return nil
}

// ExampleRootConfig returns an example configuration for "init".
func ExampleRootConfig() *RootConfig {
	return &RootConfig{
		Mail: Config{
			DefaultAccount: "work",
			Accounts: map[string]AccountConfig{
				"work": {
					Name:     "Work Account",
					Email:    "user@example.com",
					FromName: "Your Name",
					IMAP: ProtocolSettings{
						Host:     "imap.example.com",
						Port:     993,
						Username: "user@example.com",
						SSL:      true,
					},
					SMTP: ProtocolSettings{
						Host:     "smtp.example.com",
						Port:     587,
						Username: "user@example.com",
						StartTLS: true,
					},
					POP3: ProtocolSettings{
						Host:     "pop3.example.com",
						Port:     995,
						Username: "user@example.com",
						SSL:      true,
					},
				},
			},
		},
	}
}

// --- internal helpers ---

func loadFromEnvJSON() (*Config, error) {
	path, err := GetEnvConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadConfigFile(path)
}

func loadFromEmxConfig() (*Config, error) {
	cmd := exec.Command("emx-config", "list", "--json")
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		stderr := strings.TrimSpace(errOut.String())
		if stderr != "" {
			return nil, fmt.Errorf("emx-config list --json failed: %w: %s", err, stderr)
		}
		return nil, fmt.Errorf("emx-config list --json failed: %w", err)
	}

	return parseRootConfig(out.Bytes())
}

func parseRootConfig(data []byte) (*Config, error) {
	var root RootConfig
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg := &root.Mail
	if cfg.Accounts == nil {
		return nil, fmt.Errorf("missing required key: mail.accounts")
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
