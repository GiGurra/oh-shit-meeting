package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/gofrs/flock"
	"github.com/gigurra/oh-shit-meeting/internal/ack"
	"github.com/gigurra/oh-shit-meeting/internal/calendar"
	"github.com/gigurra/oh-shit-meeting/internal/format"
	"github.com/gigurra/oh-shit-meeting/internal/gui"
	"github.com/gigurra/oh-shit-meeting/internal/reminder"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type Params struct {
	PollInterval time.Duration `descr:"How often to poll Google Calendar for events" default:"5m"`
	WarnBefore   time.Duration `descr:"Global alert time before meeting" default:"5m"`
	Sound        string        `descr:"Alert sound (none, or system sound name like Glass, Hero, Funk)" default:"Hero"`
	Fullscreen   bool          `descr:"Show alerts in fullscreen mode for maximum obnoxiousness" default:"false"`
	Backend       string        `descr:"Calendar backend to use" default:"auto" alts:"auto,google,gws,gog"`
	LookaheadDays int           `descr:"How many days ahead to look for events" default:"3"`
}

type ListEventsParams struct {
	Backend       string `descr:"Calendar backend to use" default:"auto" alts:"auto,google,gws,gog"`
	Json          bool   `descr:"Output as JSON" default:"false"`
	LookaheadDays int    `descr:"How many days ahead to look for events" default:"3"`
}

type AuthParams struct {
	Credentials string `optional:"true" descr:"Path to Google OAuth client credentials JSON from GCP console"`
	Interactive bool   `short:"i" descr:"Enter client ID and secret interactively" default:"false"`
}

type StatusParams struct{}

type LogoutParams struct{}

func main() {
	boa.CmdT[Params]{
		Use:   "oh-shit-meeting",
		Short: "Calendar reminder daemon",
		Long:  "Monitors your calendar and displays warnings when meetings are about to start",
		RunFunc: func(params *Params, cmd *cobra.Command, args []string) {
			run(params)
		},
		SubCmds: boa.SubCmds(
			boa.CmdT[AuthParams]{
				Use:   "auth",
				Short: "Authenticate with Google Calendar (OAuth2 browser flow)",
				Long: `Authenticates with Google Calendar using OAuth2.

Provide the path to a client credentials JSON file downloaded from the
Google Cloud Console. The path is saved for subsequent use.

The OAuth token is stored securely in the system keychain.

To get a credentials file:
  1. Go to https://console.cloud.google.com/apis/credentials
  2. Create an OAuth 2.0 Client ID (Desktop app)
  3. Enable the Google Calendar API
  4. Download the JSON file`,
				RunFunc: func(params *AuthParams, cmd *cobra.Command, args []string) {
					if params.Interactive {
						clientID, clientSecret := readClientCredentials()
						if err := calendar.AuthenticateWithClientIDSecret(clientID, clientSecret); err != nil {
							fmt.Fprintf(os.Stderr, "Error: %v\n", err)
							os.Exit(1)
						}
						fmt.Println("Authenticated successfully.")
						return
					}
					// Try credentials file if provided
					if params.Credentials != "" {
						if err := calendar.Authenticate(params.Credentials); err != nil {
							fmt.Fprintf(os.Stderr, "Error: %v\n", err)
							os.Exit(1)
						}
						fmt.Println("Authenticated successfully.")
						return
					}
					// Try stored client credentials from previous auth
					if calendar.HasGoogleCredentials() {
						if err := calendar.ReAuthenticate(); err != nil {
							fmt.Fprintf(os.Stderr, "Error: %v\n", err)
							os.Exit(1)
						}
						fmt.Println("Authenticated successfully.")
						return
					}
					fmt.Fprintln(os.Stderr, "Error: no stored credentials found")
					fmt.Fprintln(os.Stderr, "")
					fmt.Fprintln(os.Stderr, "Usage:")
					fmt.Fprintln(os.Stderr, "  oh-shit-meeting auth --credentials /path/to/credentials.json")
					fmt.Fprintln(os.Stderr, "  oh-shit-meeting auth --interactive")
					os.Exit(1)
				},
			},
			boa.CmdT[StatusParams]{
				Use:   "status",
				Short: "Show Google Calendar authentication status",
				RunFunc: func(params *StatusParams, cmd *cobra.Command, args []string) {
					status := calendar.GetTokenStatus()

					if !status.HasToken && !status.HasCredentials {
						fmt.Println("Not authenticated.")
						fmt.Println("Run: oh-shit-meeting auth --credentials <file>")
						fmt.Println("  or: oh-shit-meeting auth --interactive")
						return
					}

					if status.HasCredentials {
						fmt.Printf("Client ID: %s\n", status.ClientID)
						fmt.Println("Client secret: stored in system keychain")
					} else {
						fmt.Println("Credentials: not configured")
					}

					if status.HasToken {
						fmt.Println("Token: stored in system keychain")
						if status.HasRefreshToken {
							fmt.Println("Refresh token: yes (token auto-renews)")
						} else {
							fmt.Println("Refresh token: no (re-auth needed when access token expires)")
						}
						if !status.Expiry.IsZero() {
							if status.Expiry.After(time.Now()) {
								fmt.Printf("Access token: valid (expires in %s)\n", time.Until(status.Expiry).Round(time.Second))
							} else {
								fmt.Println("Access token: expired (will auto-refresh on next use)")
							}
						}
						if !status.AuthenticatedAt.IsZero() {
							age := time.Since(status.AuthenticatedAt).Round(time.Minute)
							fmt.Printf("Authenticated: %s (%s ago)\n",
								status.AuthenticatedAt.Local().Format("2006-01-02 15:04"),
								age)
							if age > 4*24*time.Hour {
								fmt.Println("Warning: refresh token may expire soon — consider running 'oh-shit-meeting auth'")
							}
						}
					} else {
						fmt.Println("Token: not found")
					}
				},
			},
			boa.CmdT[LogoutParams]{
				Use:   "logout",
				Short: "Remove stored Google OAuth token from system keychain",
				RunFunc: func(params *LogoutParams, cmd *cobra.Command, args []string) {
					if !calendar.HasGoogleToken() {
						fmt.Println("No token found in keychain.")
						return
					}
					if err := calendar.Logout(); err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						os.Exit(1)
					}
					fmt.Println("Token removed from system keychain.")
				},
			},
			boa.CmdT[ListEventsParams]{
				Use:   "list-events",
				Short: "List upcoming calendar events (live integration test)",
				RunFunc: func(params *ListEventsParams, cmd *cobra.Command, args []string) {
					calendar.ReAuthIfStale()
					events := calendar.Poll(params.Backend, params.LookaheadDays)
					if len(events) == 0 {
						fmt.Println("No upcoming events found.")
						return
					}
					if params.Json {
						enc := json.NewEncoder(os.Stdout)
						enc.SetIndent("", "  ")
						enc.Encode(events)
						return
					}
					for _, e := range events {
						start, _ := time.Parse(time.RFC3339, e.Start.DateTime)
						fmt.Printf("  %s  %-40s  %s\n",
							start.Local().Format("Mon 02 Jan 15:04"),
							e.Summary,
							e.Location,
						)
					}
				},
			},
		),
	}.Run()
}

func run(params *Params) {
	lockPath := filepath.Join(os.TempDir(), "oh-shit-meeting.lock")
	fileLock := flock.New(lockPath)

	locked, err := fileLock.TryLock()
	if err != nil {
		slog.Error("Failed to acquire lock", "error", err)
		os.Exit(1)
	}
	if !locked {
		slog.Error("Another instance is already running")
		os.Exit(1)
	}
	defer fileLock.Unlock()

	slog.Info("Starting calendar reminder",
		"pollInterval", params.PollInterval,
		"warnBefore", params.WarnBefore,
	)

	// Log auth status at startup
	status := calendar.GetTokenStatus()
	if status.HasToken {
		if status.AuthenticatedAt.IsZero() {
			slog.Info("Google auth: token found, auth time unknown")
		} else {
			age := time.Since(status.AuthenticatedAt).Round(time.Minute)
			slog.Info("Google auth: token found",
				"authenticatedAt", status.AuthenticatedAt.Local().Format("2006-01-02 15:04"),
				"age", age,
				"hasRefreshToken", status.HasRefreshToken)
		}
	} else if status.HasCredentials {
		slog.Info("Google auth: credentials stored but no token — will authenticate on first poll")
	} else {
		slog.Warn("Google auth: not configured — run 'oh-shit-meeting auth'")
	}

	// Re-auth before starting if token is stale
	calendar.ReAuthIfStale()

	gui.Init()
	go runLoop(params)
	gui.Run()
}

func runLoop(params *Params) {
	var events []calendar.Event
	var lastPoll time.Time

	ackStore := &ack.FileStore{}
	clock := &reminder.RealClock{}
	finder := reminder.NewFinder(ackStore, clock, reminder.Config{
		WarnBefore: params.WarnBefore,
		Sound:      params.Sound,
	})

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Poll Google if needed
		if time.Since(lastPoll) >= params.PollInterval {
			calendar.ReAuthIfStale()
			events = calendar.Poll(params.Backend, params.LookaheadDays)
			lastPoll = time.Now()
		}

		// Check for reminders to fire
		info := finder.FindNext(events)
		if info != nil {
			slog.Warn("MEETING STARTING SOON",
				"event", info.Event.Summary,
				"startsIn", format.Duration(info.TimeUntil),
				"startTime", info.StartTime.Local().Format("15:04"),
				"location", info.Event.Location,
				"source", info.ReminderID,
			)

			gui.ShowPopupBlocking(gui.ReminderInfo{
				Summary:        info.Event.Summary,
				StartTime:      info.StartTime,
				TimeUntil:      info.TimeUntil,
				ReminderID:     info.ReminderID,
				Sound:          info.Sound,
				Location:       info.Event.Location,
				OrganizerName:  info.Event.Organizer.DisplayName,
				OrganizerEmail: info.Event.Organizer.Email,
				Fullscreen:     params.Fullscreen,
			})

			if err := ackStore.MarkAcked(info.Event.ID, info.ReminderID); err != nil {
				slog.Error("Failed to mark reminder as acknowledged", "error", err)
			}
		}
	}
}

func readClientCredentials() (clientID, clientSecret string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Client ID: ")
	clientID, _ = reader.ReadString('\n')
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		fmt.Fprintln(os.Stderr, "Error: client ID cannot be empty")
		os.Exit(1)
	}

	fmt.Print("Client Secret: ")
	secretBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // newline after hidden input
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading secret: %v\n", err)
		os.Exit(1)
	}
	clientSecret = strings.TrimSpace(string(secretBytes))
	if clientSecret == "" {
		fmt.Fprintln(os.Stderr, "Error: client secret cannot be empty")
		os.Exit(1)
	}

	return clientID, clientSecret
}
