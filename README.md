# oh-shit-meeting

A calendar reminder daemon that makes sure you never miss a meeting. Serves a local dashboard in your browser and fires an obnoxiously large flashing red panic page the moment a meeting is about to start.

> **Note:** This is a quick hack by Claude Code. Use at your own risk.

## Features

- **Local dashboard** — `http://127.0.0.1:47448/` shows that the daemon is running and lists upcoming events. Each event is an accordion that expands into description, attendees, Meet link, and "Open in Google Calendar"
- **Flashing red panic page** — the same dashboard tab flips into a full-viewport alert when a reminder fires; the browser is also auto-opened/focused for good measure
- **Looping alert sound** — keeps playing until you click ACKNOWLEDGE
- **Systray icon** — flashes red during active alerts; "Open dashboard" / "Quit" menu. Pure-Go on Windows; gracefully no-ops where no tray host is available (e.g. WSL, some GNOME setups)
- **Google Calendar integration** — native OAuth2 (or `gws`/`gog` CLI as an explicit backend)
- **Custom reminder support** — respects your calendar's reminder settings (30m, 2h, etc.)
- **Global fallback reminder** — configurable default reminder for all events
- **Non-blocking polling** — calendar polling runs in a background goroutine with timeouts, so alerts always fire on time even if the API is slow
- **Cross-platform audio** — macOS system sounds; generated tones on Linux/Windows (experimental, untested)
- **Deduplication** — each reminder only fires once per event instance (persisted to disk, auto-cleaned after 7 days)
- **`--display-test-alert`** — fire a synthetic alert and exit on ack, useful for dogfooding without waiting for a real meeting
- **Keychain fallback** — opt-in plaintext file storage via `--accept-insecure-secret-storage` when no system keychain is available (e.g. WSL without `gnome-keyring`)

## What it looks like

The dashboard lives at `http://127.0.0.1:47448/` (override with `--port`). In idle state it shows upcoming events as expandable cards with countdowns, attendees, Meet links, descriptions, and a link back into Google Calendar. When a reminder fires, the same page flips into a flashing red panic view with a huge ACKNOWLEDGE button and a prominent Join Google Meet button if the event has a hangout link. The browser tab is auto-focused so you can't hide from it.

Try it with no calendar connection required:

```bash
oh-shit-meeting --display-test-alert
```

## Prerequisites

**Recommended:** Use the built-in Google Calendar integration (no external tools needed):

```bash
# Option 1: Use a credentials JSON file from GCP
oh-shit-meeting auth --credentials /path/to/credentials.json

# Option 2: Enter client ID and secret interactively
oh-shit-meeting auth --interactive
```

To get credentials:
1. Go to [Google Cloud Console - Credentials](https://console.cloud.google.com/apis/credentials)
2. Create an OAuth 2.0 Client ID (Desktop app)
3. Enable the [Google Calendar API](https://console.cloud.google.com/apis/library/calendar-json.googleapis.com)
4. Download the JSON file, or copy the client ID and secret

All secrets (OAuth token, client secret) are stored securely in your system keychain (macOS Keychain, GNOME Keyring, or Windows Credential Manager) by default. You only need to authenticate once — re-running `oh-shit-meeting auth` reuses stored credentials.

If you're running somewhere without a working keychain (common on WSL without `gnome-keyring`, or minimal Docker images), pass `--accept-insecure-secret-storage` to opt into a plaintext JSON fallback at `~/.config/oh-shit-meeting/secrets.json` (mode `0600`). The fallback only kicks in when the keychain is actually unavailable; as soon as the keychain starts working again, the next `auth`/`Set` clears the stale plaintext entry.

**Alternative:** You can also use one of these CLI tools as an explicit backend (`--backend=gws` or `--backend=gog`):

- [gws](https://github.com/googleworkspace/cli) - Google Workspace CLI
- [gog](https://github.com/steipete/gogcli) - Google API CLI tool

## Installation

```bash
go install github.com/gigurra/oh-shit-meeting@latest
```

Or build from source:

```bash
git clone https://github.com/gigurra/oh-shit-meeting.git
cd oh-shit-meeting
go build .
```

No C toolchain required — the UI is a pure-Go HTTP server + your existing browser. The tray icon uses `fyne.io/systray`, which is pure-Go on Windows and needs `gcc` + `libayatana-appindicator3-dev` only on Linux/macOS (and even there the rest of the app runs fine without a tray host).

## Usage

```bash
# Authenticate with Google Calendar (first time setup)
oh-shit-meeting auth --credentials /path/to/credentials.json
# Or enter client ID/secret interactively
oh-shit-meeting auth --interactive

# Run with defaults (dashboard at http://127.0.0.1:47448/, poll every 5m, warn 5m before)
oh-shit-meeting

# Fire a synthetic alert right now (no calendar needed) and exit on ack
oh-shit-meeting --display-test-alert

# Different dashboard port
oh-shit-meeting --port=9876

# Custom settings
oh-shit-meeting --poll-interval=1m --warn-before=10m

# Different alert sound (macOS)
oh-shit-meeting --sound=Funk

# Fullscreen panic mode
oh-shit-meeting --fullscreen

# Force a specific backend
oh-shit-meeting --backend=gog

# Run somewhere without a working keychain (e.g. WSL)
oh-shit-meeting --accept-insecure-secret-storage

# List upcoming events (integration test)
oh-shit-meeting list-events
oh-shit-meeting list-events --json
oh-shit-meeting list-events --backend=gws --json

# Remove stored token (keychain + plaintext fallback)
oh-shit-meeting logout

# Run in background (fish shell)
oh-shit-meeting &; disown
```

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `--poll-interval` | `5m` | How often to poll Google Calendar |
| `--warn-before` | `5m` | Global reminder time before meetings |
| `--fullscreen`  | `false` | Alerts shown in full-screen mode |
| `--sound` | `Hero` | Alert sound (macOS: Glass, Hero, Funk, etc. or `none`) |
| `--backend` | `auto` | Calendar backend: `auto`, `google`, `gws`, or `gog` |
| `--lookahead-days` | `3` | How many days ahead to look for events |
| `--port` | `47448` | Port for the local dashboard HTTP server (4SHIT on a phone keypad) |
| `--display-test-alert` | `false` | Fire a synthetic alert and exit when acknowledged |
| `--accept-insecure-secret-storage` | `false` | Allow plaintext JSON fallback when the system keychain is unavailable (available on every subcommand) |

## How It Works

1. Starts a local HTTP server on `127.0.0.1:<--port>` serving a single-page dashboard; also puts a circle icon in the system tray where one is available
2. Polls Google Calendar in a **background goroutine** every poll interval (with 30s timeout per API call)
3. Checks reminders **every second** against cached events (never blocked by polling)
4. For each upcoming event, checks:
   - Custom reminder overrides (popup reminders only)
   - Global `--warn-before` threshold
5. When a reminder triggers:
   - Creates an ack file at `~/.oh-shit-meeting/<event-id>_<start-time>/<reminder>.acked`
   - Flips the dashboard page into a flashing red panic view (via state polling — any tab open to the dashboard sees it)
   - Opens/focuses the browser so the alert comes to the front
   - Flashes the tray icon between green and red
   - Plays the alert sound on loop
6. Click ACKNOWLEDGE (the big white button on the panic page) to dismiss
7. Stale ack files are cleaned up automatically after 7 days

The dashboard endpoints (`/`, `/state`, `/ack`) are bound to loopback only, reject requests with non-loopback `Host` headers (DNS-rebinding defence), and require a same-origin `Origin` on unsafe methods. ACK requires the exact reminder ID of the currently active alert.

## Running as a Background Service

> **Warning**: This section is experimental and provided as-is.

### macOS (launchd)

Create `~/Library/LaunchAgents/com.gigurra.oh-shit-meeting.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.gigurra.oh-shit-meeting</string>
    <key>ProgramArguments</key>
    <array>
        <string>/path/to/oh-shit-meeting</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

Then:

```bash
launchctl load ~/Library/LaunchAgents/com.gigurra.oh-shit-meeting.plist
```

