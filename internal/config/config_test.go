package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.APIURL != "http://localhost:3101" {
		t.Errorf("expected APIURL http://localhost:3101, got %s", cfg.APIURL)
	}
	if cfg.RefreshRateMs != 1000 {
		t.Errorf("expected RefreshRateMs 1000, got %d", cfg.RefreshRateMs)
	}
	if cfg.DiscordWebhook != "" {
		t.Errorf("expected empty DiscordWebhook, got %s", cfg.DiscordWebhook)
	}
	if len(cfg.Runtimes) != 2 {
		t.Errorf("expected 2 runtimes, got %d", len(cfg.Runtimes))
	}
	if cfg.Theme.ErrorColor != "red" {
		t.Errorf("expected error_color red, got %s", cfg.Theme.ErrorColor)
	}
	if cfg.Theme.WarnColor != "yellow" {
		t.Errorf("expected warn_color yellow, got %s", cfg.Theme.WarnColor)
	}
	if cfg.Alerts.SpendPerHourThreshold != 0.50 {
		t.Errorf("expected spend_per_hour_threshold 0.50, got %f", cfg.Alerts.SpendPerHourThreshold)
	}
	if cfg.Alerts.ErrorStreakCount != 3 {
		t.Errorf("expected error_streak_count 3, got %d", cfg.Alerts.ErrorStreakCount)
	}
}

func TestLoadNonexistentPath(t *testing.T) {
	_, err := Load("/nonexistent/path/config.toml")
	if err == nil {
		t.Error("expected error for nonexistent explicit path, got nil")
	}
}

func TestLoadDefaults(t *testing.T) {
	// Create a temporary directory with no config file
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	// Set HOME to temp dir so defaultConfigPath returns a path in temp
	os.Setenv("HOME", tmpDir)

	loadedCfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if loadedCfg.APIURL != "http://localhost:3101" {
		t.Errorf("expected APIURL http://localhost:3101, got %s", loadedCfg.APIURL)
	}
}

func TestLoadWithEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a test config file
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `api_url = "http://api.example.com"
refresh_rate_ms = 2000
runtimes = ["paperclip"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Set environment variable overrides
	oldURL := os.Getenv("AGENT_HTOP_API_URL")
	oldMS := os.Getenv("AGENT_HTOP_REFRESH_MS")
	defer func() {
		os.Setenv("AGENT_HTOP_API_URL", oldURL)
		os.Setenv("AGENT_HTOP_REFRESH_MS", oldMS)
	}()

	os.Setenv("AGENT_HTOP_API_URL", "http://override.example.com")
	os.Setenv("AGENT_HTOP_REFRESH_MS", "5000")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.APIURL != "http://override.example.com" {
		t.Errorf("expected APIURL http://override.example.com, got %s", cfg.APIURL)
	}
	if cfg.RefreshRateMs != 5000 {
		t.Errorf("expected RefreshRateMs 5000, got %d", cfg.RefreshRateMs)
	}
	if len(cfg.Runtimes) != 1 || cfg.Runtimes[0] != "paperclip" {
		t.Errorf("expected runtimes [paperclip], got %v", cfg.Runtimes)
	}
}

func TestLoadOrCreate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// File shouldn't exist yet
	if _, err := os.Stat(configPath); err == nil {
		t.Fatal("config file should not exist yet")
	}

	cfg, path, err := LoadOrCreate(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if path != configPath {
		t.Errorf("expected path %s, got %s", configPath, path)
	}

	// File should now exist
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}

	// Config should have defaults
	if cfg.APIURL != "http://localhost:3101" {
		t.Errorf("expected default APIURL, got %s", cfg.APIURL)
	}

	// Load it again and verify it's the same
	cfg2, path2, err := LoadOrCreate(configPath)
	if err != nil {
		t.Fatalf("unexpected error on second load: %v", err)
	}

	if path2 != configPath {
		t.Errorf("expected path %s, got %s", configPath, path2)
	}

	// Should still have the same values
	if cfg2.APIURL != cfg.APIURL {
		t.Errorf("expected same APIURL on reload")
	}
}

func TestLoadOrCreateWithDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "config.toml")

	cfg, path, err := LoadOrCreate(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if path != configPath {
		t.Errorf("expected path %s, got %s", configPath, path)
	}

	// Both file and directory should now exist
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(configPath)); err != nil {
		t.Fatalf("config directory was not created: %v", err)
	}

	if cfg.APIURL != "http://localhost:3101" {
		t.Errorf("expected default APIURL, got %s", cfg.APIURL)
	}
}

func TestEnvVariableRuntimesParsing(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write minimal config
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	oldRuntimes := os.Getenv("AGENT_HTOP_RUNTIMES")
	defer os.Setenv("AGENT_HTOP_RUNTIMES", oldRuntimes)

	os.Setenv("AGENT_HTOP_RUNTIMES", "paperclip, claude, codex")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"paperclip", "claude", "codex"}
	if len(cfg.Runtimes) != len(expected) {
		t.Errorf("expected %d runtimes, got %d", len(expected), len(cfg.Runtimes))
	}
	for i, r := range cfg.Runtimes {
		if r != expected[i] {
			t.Errorf("expected runtime[%d]=%s, got %s", i, expected[i], r)
		}
	}
}
