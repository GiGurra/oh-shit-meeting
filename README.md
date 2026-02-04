# oh-shit-meeting

A calendar reminder daemon that makes sure you never miss a meeting by displaying obnoxiously large, flashing red popups with sound alerts. Lives in your menu bar as a systray app.

> **Note:** This is a quick hack by Claude Code. Use at your own risk.

## Features

- **Systray app** - Lives in your menu bar
- **Flashing red popup** - Impossible to ignore
- **Looping alert sound** - Keeps playing until you acknowledge
- **Google Calendar integration** - Uses `gog` CLI to fetch events
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

- [gog](https://github.com/steipete/gogcli) - Google API CLI tool for calendar access

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
# Run with defaults (poll every 5m, warn 5m before meetings)
./oh-shit-meeting

# Custom settings
./oh-shit-meeting --poll-interval=1m --warn-before=10m

# Different alert sound (macOS)
./oh-shit-meeting --sound=Funk

# Run in background (fish shell)
./oh-shit-meeting &; disown
```

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `--poll-interval` | `5m` | How often to poll Google Calendar |
| `--warn-before` | `5m` | Global reminder time before meetings |
| `--sound` | `Hero` | Alert sound (macOS: Glass, Hero, Funk, etc. or `none`) |

## How It Works

1. Runs as a systray app (red circle in menu bar)
2. Checks reminders every second against cached events
3. Polls Google Calendar via `gog calendar list` every poll interval
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

