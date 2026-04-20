package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Queue represents a queue definition in config.
type Queue struct {
	Name          string `toml:"name"`
	MaxRetries    int    `toml:"max_retries"`
	RetentionDays int    `toml:"retention_days"`
}

// Config represents the agent-htop configuration.
type Config struct {
	APIURL            string      `toml:"api_url"`
	RefreshRateMs     int         `toml:"refresh_rate_ms"`
	DiscordWebhook    string      `toml:"discord_webhook"`
	Runtimes          []string    `toml:"runtimes"`
	Theme             Theme       `toml:"theme"`
	Alerts            Alerts      `toml:"alerts"`
	Policies          []RawPolicy `toml:"policy"`
	Queues            []Queue     `toml:"queue"`
}

// Theme contains theme-related configuration.
type Theme struct {
	ErrorColor string `toml:"error_color"`
	WarnColor  string `toml:"warn_color"`
}

// Alerts contains alert-related configuration.
type Alerts struct {
	SpendPerHourThreshold float64 `toml:"spend_per_hour_threshold"`
	ErrorStreakCount      int     `toml:"error_streak_count"`
}

// RawPolicy is a raw policy definition from TOML config.
// Maps directly to policy.Policy.
type RawPolicy struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	When        string `toml:"when"`
	Action      string `toml:"action"`
	Reason      string `toml:"reason"`
	Channel     string `toml:"channel"`
	DryRunTTL   string `toml:"dry_run_ttl"`
	Enabled     bool   `toml:"enabled"`
}

// DefaultConfig returns a new Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		APIURL:         "http://localhost:3101",
		RefreshRateMs:  1000,
		DiscordWebhook: "",
		Runtimes:       []string{"auto"},
		Theme: Theme{
			ErrorColor: "red",
			WarnColor:  "yellow",
		},
		Alerts: Alerts{
			SpendPerHourThreshold: 0.50,
			ErrorStreakCount:      3,
		},
	}
}

// Load loads configuration from the specified path, or the default location if not specified.
// It returns a Config with defaults merged with loaded values and environment variable overrides.
func Load(configPath string) (Config, error) {
	cfg := DefaultConfig()

	// Determine which config file to load
	var filePath string
	if configPath != "" {
		filePath = configPath
	} else {
		filePath = defaultConfigPath()
	}

	// Load from file if it exists
	if filePath != "" {
		if _, err := os.Stat(filePath); err == nil {
			if _, err := toml.DecodeFile(filePath, &cfg); err != nil {
				return Config{}, fmt.Errorf("failed to parse config file %s: %w", filePath, err)
			}
		} else if configPath != "" && os.IsNotExist(err) {
			// If explicit config path was provided but doesn't exist, error
			return Config{}, fmt.Errorf("config file not found: %s", configPath)
		}
		// If no explicit path and default location doesn't exist, silently use defaults
	}

	// Apply environment variable overrides
	applyEnvOverrides(&cfg)

	return cfg, nil
}

// LoadOrCreate loads configuration, or writes defaults to the standard location if missing.
func LoadOrCreate(configPath string) (Config, string, error) {
	cfg := DefaultConfig()

	// Determine which config file to use
	var filePath string
	if configPath != "" {
		filePath = configPath
	} else {
		filePath = defaultConfigPath()
	}

	// Load from file if it exists
	fileExists := false
	if filePath != "" {
		if _, err := os.Stat(filePath); err == nil {
			fileExists = true
			if _, err := toml.DecodeFile(filePath, &cfg); err != nil {
				return Config{}, "", fmt.Errorf("failed to parse config file %s: %w", filePath, err)
			}
		} else if !os.IsNotExist(err) {
			return Config{}, "", fmt.Errorf("failed to check config file %s: %w", filePath, err)
		}
	}

	// If file doesn't exist and we have a path, write defaults
	if !fileExists && filePath != "" {
		if err := writeDefaultConfig(filePath, cfg); err != nil {
			return Config{}, "", err
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(&cfg)

	return cfg, filePath, nil
}

// defaultConfigPath returns the default config file path.
// Searches in order: ~/.config/agent-htop/config.toml, ~/.agent-htop.toml
// Returns empty string if no standard location found.
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Try ~/.config/agent-htop/config.toml
	configPath := filepath.Join(home, ".config", "agent-htop", "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return configPath
	}

	// Try ~/.agent-htop/config.toml
	configPath = filepath.Join(home, ".agent-htop", "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return configPath
	}

	// Return the first location as the canonical default location
	return filepath.Join(home, ".config", "agent-htop", "config.toml")
}

// writeDefaultConfig writes the default configuration to the specified path.
func writeDefaultConfig(filePath string, cfg Config) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	// Write config to file
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create config file %s: %w", filePath, err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", filePath, err)
	}

	return nil
}

// applyEnvOverrides applies environment variable overrides to the configuration.
func applyEnvOverrides(cfg *Config) {
	if url := os.Getenv("AGENT_HTOP_API_URL"); url != "" {
		cfg.APIURL = url
	}
	if ms := os.Getenv("AGENT_HTOP_REFRESH_MS"); ms != "" {
		if val, err := strconv.Atoi(ms); err == nil {
			cfg.RefreshRateMs = val
		}
	}
	if webhook := os.Getenv("AGENT_HTOP_DISCORD_WEBHOOK"); webhook != "" {
		cfg.DiscordWebhook = webhook
	}
	if runtimes := os.Getenv("AGENT_HTOP_RUNTIMES"); runtimes != "" {
		cfg.Runtimes = strings.Split(runtimes, ",")
		// Trim whitespace from each runtime
		for i := range cfg.Runtimes {
			cfg.Runtimes[i] = strings.TrimSpace(cfg.Runtimes[i])
		}
	}
	if color := os.Getenv("AGENT_HTOP_THEME_ERROR_COLOR"); color != "" {
		cfg.Theme.ErrorColor = color
	}
	if color := os.Getenv("AGENT_HTOP_THEME_WARN_COLOR"); color != "" {
		cfg.Theme.WarnColor = color
	}
	if threshold := os.Getenv("AGENT_HTOP_ALERTS_SPEND_THRESHOLD"); threshold != "" {
		if val, err := strconv.ParseFloat(threshold, 64); err == nil {
			cfg.Alerts.SpendPerHourThreshold = val
		}
	}
	if count := os.Getenv("AGENT_HTOP_ALERTS_ERROR_STREAK"); count != "" {
		if val, err := strconv.Atoi(count); err == nil {
			cfg.Alerts.ErrorStreakCount = val
		}
	}
}
