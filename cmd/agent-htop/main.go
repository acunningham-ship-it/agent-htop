package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/acunningham-ship-it/agent-htop/internal/aggregator"
	"github.com/acunningham-ship-it/agent-htop/internal/api"
	"github.com/acunningham-ship-it/agent-htop/internal/config"
	"github.com/acunningham-ship-it/agent-htop/internal/notify"
	"github.com/acunningham-ship-it/agent-htop/internal/parser"
	"github.com/acunningham-ship-it/agent-htop/internal/ui"
	"github.com/acunningham-ship-it/agent-htop/internal/watcher"
)

const (
	daemonPIDFile  = ".config/agent-htop/daemon.pid"
	daemonLogFile  = ".config/agent-htop/daemon.log"
	daemonLogSize  = 10 * 1024 * 1024 // 10MB
	daemonLogCount = 3                 // Keep 3 rotated logs
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

// readPID reads the daemon PID from the pid file
func readPID() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	pidPath := filepath.Join(home, daemonPIDFile)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, fmt.Errorf("no daemon running: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file: %v", err)
	}
	return pid, nil
}

// writePID writes the daemon PID to the pid file
func writePID(pid int) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pidPath := filepath.Join(home, daemonPIDFile)
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

// removePID removes the daemon PID file
func removePID() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pidPath := filepath.Join(home, daemonPIDFile)
	return os.Remove(pidPath)
}

// setupDaemonLogging sets up log file for daemon
func setupDaemonLogging(verbose bool) (*os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(home, daemonLogFile)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return logFile, nil
}

// cmdDaemonStart starts the daemon process
func cmdDaemonStart(companyID, configPath, apiURL string, refreshMs int, runtimesStr, alertLevel string, verbose bool) {
	// Check if daemon is already running
	if pid, err := readPID(); err == nil {
		// Check if process exists
		proc, err := os.FindProcess(pid)
		if err == nil {
			err := proc.Signal(syscall.Signal(0)) // Signal 0 just checks if process exists
			if err == nil {
				fmt.Fprintf(os.Stderr, "Daemon already running (PID: %d)\n", pid)
				os.Exit(1)
			}
		}
		// Stale PID file, remove it
		removePID()
	}

	// Setup daemon logging
	logFile, err := setupDaemonLogging(verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up daemon logging: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	log.Printf("Starting agent-htop daemon v%s (git: %s)", Version, GitSHA)

	// Write PID file
	if err := writePID(os.Getpid()); err != nil {
		log.Fatalf("Failed to write PID file: %v", err)
	}

	// Run daemon process
	runDaemon(companyID, configPath, apiURL, refreshMs, runtimesStr, alertLevel, verbose)
}

// cmdDaemonStop stops the running daemon
func cmdDaemonStop(verbose bool) {
	pid, err := readPID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: daemon process not found (PID: %d)\n", pid)
		removePID()
		os.Exit(1)
	}

	// Send SIGTERM
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to stop daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Sent stop signal to daemon (PID: %d)\n", pid)

	// Wait a bit for graceful shutdown
	for i := 0; i < 50; i++ { // 5 seconds
		proc, err := os.FindProcess(pid)
		if err != nil {
			removePID()
			fmt.Printf("Daemon stopped\n")
			return
		}
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			removePID()
			fmt.Printf("Daemon stopped\n")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill if still running
	proc.Kill()
	removePID()
	fmt.Printf("Daemon force-killed (was not responding to SIGTERM)\n")
}

// cmdDaemonStatus checks the status of the daemon
func cmdDaemonStatus() {
	pid, err := readPID()
	if err != nil {
		fmt.Printf("Daemon is not running\n")
		os.Exit(1)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("Daemon PID file exists but process not found (stale PID: %d)\n", pid)
		os.Exit(1)
	}

	if err := proc.Signal(syscall.Signal(0)); err != nil {
		fmt.Printf("Daemon PID file exists but process not responding (stale PID: %d)\n", pid)
		os.Exit(1)
	}

	fmt.Printf("Daemon is running (PID: %d)\n", pid)
}

// runDaemon runs the actual daemon monitoring logic
func runDaemon(companyID, configPath, apiURL string, refreshMs int, runtimesStr, alertLevel string, verbose bool) {
	// Load or create config
	cfg, _, err := config.LoadOrCreate(configPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Apply command-line flag overrides
	if apiURL != "" {
		cfg.APIURL = apiURL
	}
	if refreshMs >= 0 {
		cfg.RefreshRateMs = refreshMs
	}
	if runtimesStr != "" {
		runtimeList := strings.Split(runtimesStr, ",")
		cfg.Runtimes = make([]string, 0, len(runtimeList))
		for _, rt := range runtimeList {
			rt = strings.TrimSpace(rt)
			if rt != "" {
				cfg.Runtimes = append(cfg.Runtimes, rt)
			}
		}
	}

	// Setup paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}

	var logDir string
	if companyID != "" {
		logDir = filepath.Join(home, ".paperclip", "instances", "default", "data", "run-logs")
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			log.Fatalf("Log directory does not exist: %s", logDir)
		}
	}

	// Create API client
	var apiClient *api.Client
	var agentNamer aggregator.AgentNamer
	ctx := context.Background()
	if companyID != "" {
		apiClient = api.NewClient(cfg.APIURL)
		agentNamer = apiClient
		if err := apiClient.Health(ctx); err != nil {
			log.Printf("Warning: Paperclip API not reachable: %v", err)
		}
	} else {
		agentNamer = aggregator.NullAgentNamer{}
	}

	// Resolve runtimes
	runtimes, err := resolveRuntimes(cfg.Runtimes, companyID)
	if err != nil {
		log.Fatalf("Error resolving runtimes: %v", err)
	}

	// Create watcher
	w, err := watcher.NewWatcher(logDir)
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}

	// Create aggregator
	agg := aggregator.NewAggregatorWithRuntimes(companyID, logDir, agentNamer, w, runtimes)

	// Create Discord notifier
	dashboardURL := fmt.Sprintf("%s/fleet/%s", cfg.APIURL, companyID)
	discordNotifier := notify.NewNotifier(cfg.DiscordWebhook, dashboardURL, notify.AlertLevel(alertLevel))

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

	log.Printf("Daemon started successfully. Monitoring %d runtimes", len(runtimes))

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for signal
	<-sigChan
	log.Printf("Received shutdown signal")

	// Cleanup
	discordNotifier.Stop()
	agg.Stop()
	if err := w.Stop(); err != nil {
		log.Printf("Error stopping watcher: %v", err)
	}

	// Remove PID file
	if err := removePID(); err != nil {
		log.Printf("Error removing PID file: %v", err)
	}

	log.Printf("Daemon stopped")
}

func main() {
	// Determine subcommand from first argument
	var subcommand string
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		subcommand = os.Args[1]
		// Remove subcommand from arguments for flag parsing
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

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
	jsonOutput := flag.Bool("json", false, "Output JSON snapshot instead of TUI (requires --once)")
	fullOutput := flag.Bool("full", false, "Include complete system state (requires --json)")
	runOnce := flag.Bool("once", false, "Run once and exit instead of launching interactive TUI")

	// Custom usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `agent-htop - Live-updating terminal dashboard for AI agent fleets

Usage: agent-htop [command] [options]

Commands:
  daemon                   Start background daemon (no TUI)
  daemon stop             Stop running daemon
  daemon status           Check daemon status

Options:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
TUI mode (default):
  agent-htop                       # Interactive dashboard
  agent-htop --company <id>       # Monitor Paperclip agents

JSON snapshot mode:
  agent-htop --once --json               # Print current fleet state as JSON and exit
  agent-htop --once --json --company <id> # Include Paperclip agents in snapshot

Configuration:
  Config file location (auto-discovered): ~/.config/agent-htop/config.toml
  Daemon log: ~/.config/agent-htop/daemon.log
  Daemon PID: ~/.config/agent-htop/daemon.pid

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

  # Start daemon mode (runs in background)
  agent-htop daemon --company 920a3930-f429-45cd-8fb8-774fa81cbd96

  # Check daemon status
  agent-htop daemon status

  # Stop running daemon
  agent-htop daemon stop

  # Add Paperclip agents to TUI (auto-connects to daemon if running)
  agent-htop --company 920a3930-f429-45cd-8fb8-774fa81cbd96
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

	// Handle daemon subcommands
	if subcommand == "daemon" {
		daemonCmd := flag.Arg(0)
		switch daemonCmd {
		case "stop":
			cmdDaemonStop(*verbose)
			return
		case "status":
			cmdDaemonStatus()
			return
		default:
			// Default daemon action is start
			cmdDaemonStart(*companyID, *configPath, *apiURL, *refreshMs, *runtimesStr, *alertLevel, *verbose)
			return
		}
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

	// Validate --json requires --once
	if *jsonOutput && !*runOnce {
		fmt.Fprintf(os.Stderr, "Error: --json requires --once flag\n")
		os.Exit(1)
	}

	// Validate --full requires --json
	if *fullOutput && !*jsonOutput {
		fmt.Fprintf(os.Stderr, "Error: --full requires --json flag\n")
		os.Exit(1)
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

	// Handle --once mode (JSON snapshot or single run)
	if *runOnce || *jsonOutput {
		// Suppress aggregator debug output if JSON mode
		var oldStdout *os.File
		if *jsonOutput {
			oldStdout = os.Stdout
			devNull, _ := os.Open(os.DevNull)
			os.Stdout = devNull
		}

		// Start watcher
		if err := w.Start(ctx); err != nil {
			if *jsonOutput {
				os.Stdout = oldStdout
			}
			log.Fatalf("Failed to start watcher: %v", err)
		}

		// Start aggregator
		if err := agg.Start(ctx); err != nil {
			if *jsonOutput {
				os.Stdout = oldStdout
			}
			log.Fatalf("Failed to start aggregator: %v", err)
		}

		// Restore stdout before waiting for state
		if *jsonOutput {
			os.Stdout = oldStdout
		}

		// Wait for initial state with timeout
		startTime := time.Now()
		timeout := 10 * time.Second
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if *jsonOutput {
					// Output JSON snapshot
					var output interface{}
					if *fullOutput {
						// Full output: complete system state
						output = agg.GetSystemState()
					} else {
						// Standard output: fleet state
						output = agg.GetFleetState()
					}

					data, err := json.MarshalIndent(output, "", "  ")
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
						agg.Stop()
						w.Stop()
						os.Exit(1)
					}
					fmt.Println(string(data))

					// Exit code 0 always for JSON output (data may be empty but valid)
					agg.Stop()
					w.Stop()
					os.Exit(0)
				} else {
					// --once without --json: just wait for load then exit
					state := agg.GetFleetState()
					if len(state.Agents) > 0 || time.Since(startTime) > timeout {
						agg.Stop()
						w.Stop()
						os.Exit(0)
					}
				}
			case <-ctx.Done():
				agg.Stop()
				w.Stop()
				os.Exit(1)
			}
		}
	}

	// Interactive TUI mode (default)
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
