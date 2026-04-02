# oh-shit-meeting

A calendar reminder daemon that makes sure you never miss a meeting by displaying obnoxiously large, flashing red popups with sound alerts. Lives in your menu bar as a systray app.

> **Note:** This is a quick hack by Claude Code. Use at your own risk.

## Features

- **Systray app** - Lives in your menu bar
- **Flashing red popup** - Impossible to ignore
- **Looping alert sound** - Keeps playing until you acknowledge
- **Google Calendar integration** - Native OAuth2 or via `gws`/`gog` CLI
- **Custom reminder support** - Respects your calendar's reminder settings (30m, 2h, etc.)
- **Global fallback reminder** - Configurable default reminder for all events
- **Cross-platform audio** - macOS system sounds; generated tones on Linux/Windows (experimental, untested)
- **Deduplication** - Each reminder only fires once (persisted to disk)

## Screenshot

*Illustration by Claude Code. The actual window looks different.*

```
┌──────────────────────────────────────────────────────────────────┐
│                                                                  │
│                     ████  FLASHING RED  ████                     │
│                                                                  │
│                        Team Standup                              │
│                                                                  │
│               Starts in 4m 32s (at 10:00)                        │
│               Calendar: work@company.com                         │
│               Location: Zoom                                     │
│               Reminder: 5m                                       │
│                                                                  │
│                    [ ACKNOWLEDGE ]                               │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
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

All secrets (OAuth token, client secret) are stored securely in your system keychain (macOS Keychain, GNOME Keyring, or Windows Credential Manager). You only need to authenticate once — re-running `oh-shit-meeting auth` reuses stored credentials.

**Alternative:** Use one of these Google Calendar CLI tools instead:

- [gws](https://github.com/googleworkspace/cli) - Google Workspace CLI
- [gog](https://github.com/steipete/gogcli) - Google API CLI tool

By default, the native Google API is used if authenticated, otherwise falls back to `gws`, then `gog`. Use `--backend` to override.

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

## Usage

```bash
# Authenticate with Google Calendar (first time setup)
oh-shit-meeting auth --credentials /path/to/credentials.json
# Or enter client ID/secret interactively
oh-shit-meeting auth --interactive

# Run with defaults (poll every 5m, warn 5m before meetings)
oh-shit-meeting

# Custom settings
oh-shit-meeting --poll-interval=1m --warn-before=10m

# Different alert sound (macOS)
oh-shit-meeting --sound=Funk

# Force a specific backend
oh-shit-meeting --backend=gog

# List upcoming events (integration test)
oh-shit-meeting list-events
oh-shit-meeting list-events --json
oh-shit-meeting list-events --backend=gws --json

# Remove stored token
oh-shit-meeting logout

# Run in background (fish shell)
oh-shit-meeting &; disown
```

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `--poll-interval` | `5m` | How often to poll Google Calendar |
| `--warn-before` | `5m` | Global reminder time before meetings |
| `--sound` | `Hero` | Alert sound (macOS: Glass, Hero, Funk, etc. or `none`) |
| `--backend` | `auto` | Calendar backend: `auto`, `google`, `gws`, or `gog` |
| `--lookahead-days` | `3` | How many days ahead to look for events |

## How It Works

1. Runs as a systray app (red circle in menu bar)
2. Checks reminders every second against cached events
3. Polls Google Calendar (native API, or via `gws`/`gog`) every poll interval
4. For each upcoming event, checks:
   - Custom reminder overrides (popup reminders only)
   - Global `--warn-before` threshold
5. When a reminder triggers:
   - Creates an ack file at `~/.oh-shit-meeting/<event-id>/<reminder>.acked`
   - Shows flashing red popup window
   - Plays alert sound on loop
6. Click ACKNOWLEDGE to dismiss

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

