package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/acunningham-ship-it/agent-htop/internal/aggregator"
	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/config"
	"github.com/acunningham-ship-it/agent-htop/internal/notify"
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
	"github.com/acunningham-ship-it/agent-htop/internal/ui"
	"github.com/acunningham-ship-it/agent-htop/internal/watcher"
)

// Version and GitSHA are injected via ldflags
var (
	Version = "v0.1.0-dev"
	GitSHA  = "unknown"
)

// resolveRuntimes processes a list of runtime strings (which may include "auto" or "all")
// and returns the expanded list of parser.Runtime values with proper validation and detection.
func resolveRuntimes(runtimeStrs []string, companyID string) ([]parser.Runtime, error) {
	var resolved []parser.Runtime

	// Check for "auto" runtime (auto-detection)
	if len(runtimeStrs) == 1 && runtimeStrs[0] == "auto" {
		var detected []parser.Runtime

		// Detect Claude Code: check if ~/.claude/projects/ exists and has .jsonl files
		home, err := os.UserHomeDir()
		if err == nil {
			claudeDir := filepath.Join(home, ".claude", "projects")
			if hasJSONLFiles(claudeDir) {
				detected = append(detected, parser.RuntimeClaude)
			}
		}

		// Detect Paperclip: only if company flag was set
		if companyID != "" {
			detected = append(detected, parser.RuntimePaperclip)
		}

		// Detect Codex: check if ~/.codex/ exists and has session files
		if err == nil {
			codexDir := filepath.Join(home, ".codex")
			if _, err := os.Stat(codexDir); err == nil {
				detected = append(detected, parser.RuntimeCodex)
			}
		}

		// Fallback: if nothing detected, show Claude even if no sessions yet
		if len(detected) == 0 {
			detected = append(detected, parser.RuntimeClaude)
		}

		return detected, nil
	}

	// Handle "all" runtime (enable all runtimes)
	if len(runtimeStrs) == 1 && runtimeStrs[0] == "all" {
		resolved = append(resolved, parser.RuntimeClaude)
		resolved = append(resolved, parser.RuntimeCodex)
		// Paperclip requires company ID
		if companyID != "" {
			resolved = append(resolved, parser.RuntimePaperclip)
		} else {
			log.Printf("Warning: paperclip runtime requires --company flag; skipping")
		}
		return resolved, nil
	}

	// Parse explicit list of runtimes
	for _, r := range runtimeStrs {
		switch r {
		case "paperclip":
			if companyID == "" {
				return nil, fmt.Errorf("paperclip runtime requires --company flag")
			}
			resolved = append(resolved, parser.RuntimePaperclip)
		case "claude":
			resolved = append(resolved, parser.RuntimeClaude)
		case "codex":
			resolved = append(resolved, parser.RuntimeCodex)
		default:
			return nil, fmt.Errorf("invalid runtime value: %s (valid: auto|all|claude|codex|paperclip)", r)
		}
	}

	return resolved, nil
}

// hasJSONLFiles checks if a directory exists and contains any .jsonl files.
func hasJSONLFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Recursively check subdirectories
			subdir := filepath.Join(dir, entry.Name())
			if hasJSONLFilesInDir(subdir) {
				return true
			}
		}
	}
	return false
}

// hasJSONLFilesInDir recursively searches for .jsonl files in a directory.
func hasJSONLFilesInDir(dir string) bool {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors, keep searching
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			return filepath.SkipDir // Signal that we found a file (stop walking)
		}
		return nil
	})
	return err == filepath.SkipDir
}

func main() {
	// Flags
	companyID := flag.String("company", "", "Company ID (optional)")
	configPath := flag.String("config", "", "Configuration file path (optional)")
	apiURL := flag.String("api-url", "", "Paperclip API URL (overrides config file)")
	refreshMs := flag.Int("refresh-ms", -1, "Refresh interval in milliseconds (overrides config file)")
	runtimesStr := flag.String("runtime", "", "Runtime types to monitor (comma-separated): paperclip,claude,codex (overrides config file)")
	alertLevel := flag.String("alert-level", "info", "Minimum alert level for Discord: info|warn|critical")
	verbose := flag.Bool("v", false, "Enable verbose debug logging to stderr")
	showVersion := flag.Bool("version", false, "Show version and exit")
	showHelp := flag.Bool("help", false, "Show help and exit")

	// Custom usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `agent-htop - Live-updating terminal dashboard for AI agent fleets

Usage: agent-htop [options]

Options:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Configuration:
  Config file location (auto-discovered): ~/.config/agent-htop/config.toml
  If missing on first run, defaults are written to ~/.config/agent-htop/config.toml

Environment Variables (override config file):
  AGENT_HTOP_API_URL                  Paperclip API URL
  AGENT_HTOP_REFRESH_MS              Refresh rate in milliseconds
  AGENT_HTOP_DISCORD_WEBHOOK         Discord webhook URL for alerts
  AGENT_HTOP_RUNTIMES                Comma-separated: paperclip,claude,codex
  AGENT_HTOP_THEME_ERROR_COLOR       Error color (e.g., red)
  AGENT_HTOP_THEME_WARN_COLOR        Warning color (e.g., yellow)
  AGENT_HTOP_ALERTS_SPEND_THRESHOLD  Spend threshold (e.g., 0.50)
  AGENT_HTOP_ALERTS_ERROR_STREAK     Error streak count (e.g., 3)

Examples:
  # Auto-discover Claude Code sessions (no config needed)
  agent-htop

  # Add Paperclip agents alongside Claude Code
  agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96

  # Monitor with custom config file
  agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 --config /path/to/config.toml

  # Override API URL via flag
  agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96 --api-url http://192.168.1.100:3101 -v

  # Override settings via environment
  AGENT_HTOP_REFRESH_MS=500 AGENT_HTOP_RUNTIMES=claude agent-htop

  # Specify runtime via CLI flag
  agent-htop --runtime claude,paperclip
`)
	}

	flag.Parse()

	// Handle --version flag
	if *showVersion {
		fmt.Printf("agent-htop %s (git: %s)\n", Version, GitSHA)
		os.Exit(0)
	}

	// Handle --help flag (stdlib flag already handles -h, but be explicit)
	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// Load or create config
	cfg, cfgPath, err := config.LoadOrCreate(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Apply command-line flag overrides
	if *apiURL != "" {
		cfg.APIURL = *apiURL
	}
	if *refreshMs >= 0 {
		cfg.RefreshRateMs = *refreshMs
	}
	if *runtimesStr != "" {
		// Parse comma-separated runtime list
		runtimeList := strings.Split(*runtimesStr, ",")
		cfg.Runtimes = make([]string, 0, len(runtimeList))
		for _, rt := range runtimeList {
			rt = strings.TrimSpace(rt)
			if rt != "" {
				cfg.Runtimes = append(cfg.Runtimes, rt)
			}
		}
	}

	// Setup logging if verbose
	if *verbose {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("DEBUG: Starting agent-htop %s (git: %s)\n", Version, GitSHA)
		log.Printf("DEBUG: Company ID: %s\n", *companyID)
		log.Printf("DEBUG: Config file: %s\n", cfgPath)
		log.Printf("DEBUG: API URL: %s\n", cfg.APIURL)
		log.Printf("DEBUG: Runtimes: %v\n", cfg.Runtimes)
		log.Printf("DEBUG: Refresh interval: %dms\n", cfg.RefreshRateMs)
		log.Printf("DEBUG: Alert level: %s\n", *alertLevel)
		log.Printf("DEBUG: Discord webhook: %s\n", cfg.DiscordWebhook)
	} else {
		log.SetOutput(io.Discard)
	}

	// Setup paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}

	// Only use Paperclip logDir if a company was specified
	var logDir string
	if *companyID != "" {
		logDir = filepath.Join(home, ".paperclip", "instances", "default", "data", "run-logs")
		// Verify log directory exists
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			log.Fatalf("Log directory does not exist: %s", logDir)
		}
	}

	// Create API client and agent namer (only if company specified)
	var apiClient *api.Client
	var agentNamer aggregator.AgentNamer
	ctx := context.Background()
	if *companyID != "" {
		apiClient = api.NewClient(cfg.APIURL)
		agentNamer = apiClient
		// Check if Paperclip is reachable
		if err := apiClient.Health(ctx); err != nil {
			log.Printf("Warning: Paperclip API not reachable: %v", err)
			log.Printf("Will continue with limited functionality")
		}
	} else {
		// No company specified - use no-op agent namer
		agentNamer = aggregator.NullAgentNamer{}
	}

	// Resolve config runtimes (handles "auto" detection, "all" expansion, and validation)
	runtimes, err := resolveRuntimes(cfg.Runtimes, *companyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create watcher
	w, err := watcher.NewWatcher(logDir)
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}

	// Create aggregator with selected runtimes
	agg := aggregator.NewAggregatorWithRuntimes(*companyID, logDir, agentNamer, w, runtimes)

	// Create Discord notifier if webhook is configured
	dashboardURL := fmt.Sprintf("%s/fleet/%s", cfg.APIURL, *companyID)
	discordNotifier := notify.NewNotifier(cfg.DiscordWebhook, dashboardURL, notify.AlertLevel(*alertLevel))

	// Start watcher
	if err := w.Start(ctx); err != nil {
		log.Fatalf("Failed to start watcher: %v", err)
	}

	// Start aggregator
	if err := agg.Start(ctx); err != nil {
		log.Fatalf("Failed to start aggregator: %v", err)
	}

	// Start Discord notifier
	discordNotifier.Start(ctx, agg.GetDetector())

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create TUI model
	uiModel := ui.New(agg, apiClient, *companyID)

	// Start the Bubble Tea program
	p := tea.NewProgram(uiModel, tea.WithAltScreen())

	// Run in a goroutine so we can handle signals
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	// Wait for either signal or TUI to quit
	select {
	case <-sigChan:
		fmt.Println("\nShutting down...")
	case err := <-done:
		if err != nil {
			log.Printf("TUI error: %v", err)
		}
	}

	// Cleanup
	discordNotifier.Stop()
	agg.Stop()
	if err := w.Stop(); err != nil {
		log.Printf("Error stopping watcher: %v", err)
	}

	fmt.Println("Done")
}
