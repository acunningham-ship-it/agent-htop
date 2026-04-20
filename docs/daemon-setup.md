# Agent-htop Daemon Setup

The agent-htop daemon runs in the background to continuously monitor your AI agent fleets. This allows the tool to enforce policies (auto-kill, pause, alerts) without requiring an open terminal.

## Quick Start

Start the daemon:
```bash
agent-htop daemon --company <YOUR_COMPANY_ID>
```

Check daemon status:
```bash
agent-htop daemon status
```

Stop the daemon:
```bash
agent-htop daemon stop
```

## Auto-Start on Boot

### Linux (systemd)

1. Copy the service file:
```bash
mkdir -p ~/.config/systemd/user
cp systemd/agent-htop.service ~/.config/systemd/user/
```

2. Reload systemd and enable the service:
```bash
systemctl --user daemon-reload
systemctl --user enable agent-htop.service
systemctl --user start agent-htop.service
```

3. Verify it's running:
```bash
systemctl --user status agent-htop.service
```

4. View logs:
```bash
journalctl --user -u agent-htop.service -f
```

**Uninstall:**
```bash
systemctl --user stop agent-htop.service
systemctl --user disable agent-htop.service
rm ~/.config/systemd/user/agent-htop.service
systemctl --user daemon-reload
```

### macOS (launchd)

1. Copy the plist file:
```bash
cp launchd/com.acunningham.agent-htop.plist ~/Library/LaunchAgents/
```

2. Load and start the service:
```bash
launchctl load ~/Library/LaunchAgents/com.acunningham.agent-htop.plist
```

3. Verify it's running:
```bash
launchctl list | grep agent-htop
```

4. View logs:
```bash
tail -f ~/.config/agent-htop/daemon.log
```

**Uninstall:**
```bash
launchctl unload ~/Library/LaunchAgents/com.acunningham.agent-htop.plist
rm ~/Library/LaunchAgents/com.acunningham.agent-htop.plist
```

## Configuration

The daemon uses the same configuration file as the TUI:
- **Config file:** `~/.config/agent-htop/config.toml`
- **Daemon log:** `~/.config/agent-htop/daemon.log`
- **Daemon PID:** `~/.config/agent-htop/daemon.pid`

Override configuration via command-line flags:
```bash
agent-htop daemon \
  --company 920a3930-f429-45cd-8fb8-774fa81cbd96 \
  --api-url http://api.example.com:3101 \
  --runtime paperclip,claude \
  --refresh-ms 5000 \
  --alert-level critical
```

## TUI Integration

When you run `agent-htop` in TUI mode, it automatically connects to a running daemon (if one exists). Both daemon and TUI will monitor the same fleet state. This gives you:

- **Consistency:** TUI shows exactly what the daemon is enforcing
- **Zero cold-start:** TUI connects to warm daemon instantly (no re-parsing logs)
- **Parallel monitoring:** Keep daemon running, open TUI only when needed

## Troubleshooting

### Daemon won't start
Check the log file:
```bash
tail ~/.config/agent-htop/daemon.log
```

Common issues:
- **Paperclip API not reachable:** Ensure the API URL is correct and the API is running
- **Log directory missing:** Ensure `~/.paperclip/instances/default/data/run-logs` exists (for Paperclip monitoring)
- **Permission denied:** Check that `~/.config/agent-htop/` is writable

### Daemon using too much CPU/RAM
The daemon refreshes its view every 5 seconds by default. To reduce resource usage:
1. Increase the refresh interval: `--refresh-ms 10000` (10 seconds)
2. Reduce monitoring to only needed runtimes: `--runtime paperclip` (instead of auto-detecting all)
3. Filter to a single company if monitoring multiple: `--company <ID>`

### Daemon logs growing too large
The daemon log file rotates when reaching 10MB (keeping 3 historical logs). Manually clean up:
```bash
rm ~/.config/agent-htop/daemon.log*
```

Or redirect to journald (Linux) for automatic rotation.
