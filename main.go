package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/gofrs/flock"
	"github.com/gigurra/oh-shit-meeting/internal/ack"
	"github.com/gigurra/oh-shit-meeting/internal/calendar"
	"github.com/gigurra/oh-shit-meeting/internal/format"
	"github.com/gigurra/oh-shit-meeting/internal/gui"
	"github.com/gigurra/oh-shit-meeting/internal/reminder"
	"github.com/gigurra/oh-shit-meeting/internal/secret"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// CommonParams is embedded into every command so the insecure-storage flag
// is available everywhere that touches secrets.
type CommonParams struct {
	AcceptInsecureSecretStorage bool `descr:"Fall back to a plaintext file (~/.config/oh-shit-meeting/secrets.json) when the system keychain is unavailable" default:"false"`
}

type Params struct {
	CommonParams
	PollInterval time.Duration `descr:"How often to poll Google Calendar for events" default:"5m"`
	WarnBefore   time.Duration `descr:"Global alert time before meeting" default:"5m"`
	Sound        string        `descr:"Alert sound (none, or system sound name like Glass, Hero, Funk)" default:"Hero"`
	Fullscreen   bool          `descr:"Show alerts in fullscreen mode for maximum obnoxiousness" default:"false"`
	Backend       string        `descr:"Calendar backend to use" default:"auto" alts:"auto,google,gws,gog"`
	LookaheadDays int           `descr:"How many days ahead to look for events" default:"3"`
	Port          int           `descr:"Port for the local dashboard HTTP server" default:"47448"`
	DisplayTestAlert bool       `descr:"Fire a synthetic alert and exit when acknowledged (for testing)" default:"false"`
}

type ListEventsParams struct {
	CommonParams
	Backend       string `descr:"Calendar backend to use" default:"auto" alts:"auto,google,gws,gog"`
	Json          bool   `descr:"Output as JSON" default:"false"`
	LookaheadDays int    `descr:"How many days ahead to look for events" default:"3"`
}

type AuthParams struct {
	CommonParams
	Credentials string `optional:"true" descr:"Path to Google OAuth client credentials JSON from GCP console"`
	Interactive bool   `short:"i" descr:"Enter client ID and secret interactively" default:"false"`
}

type StatusParams struct {
	CommonParams
}

type LogoutParams struct {
	CommonParams
}

// describeLoc converts a secret.Location string into a human-readable phrase.
func describeLoc(loc string) string {
	switch loc {
	case "":
		return "(unknown)"
	case "keychain":
		return "system keychain"
	default:
		return loc
	}
}

func getVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	boa.CmdT[Params]{
		Use:     "oh-shit-meeting",
		Short:   "Calendar reminder daemon",
		Long:    "Monitors your calendar and displays warnings when meetings are about to start",
		Version: getVersion(),
		RunFunc: func(params *Params, cmd *cobra.Command, args []string) {
			secret.AcceptInsecure(params.AcceptInsecureSecretStorage)
			run(params)
		},
		SubCmds: boa.SubCmds(
			boa.CmdT[AuthParams]{
				Use:   "auth",
				Short: "Authenticate with Google Calendar (OAuth2 browser flow)",
				Long: `Authenticates with Google Calendar using OAuth2.

Provide the path to a client credentials JSON file downloaded from the
Google Cloud Console. The path is saved for subsequent use.

By default, the OAuth token and client secret are stored in the system
keychain. If --accept-insecure-secret-storage is set and the keychain is
unavailable (e.g. on WSL without gnome-keyring), they fall back to a
plaintext JSON file under the platform's user config directory.

To get a credentials file:
  1. Go to https://console.cloud.google.com/apis/credentials
  2. Create an OAuth 2.0 Client ID (Desktop app)
  3. Enable the Google Calendar API
  4. Download the JSON file`,
				RunFunc: func(params *AuthParams, cmd *cobra.Command, args []string) {
					secret.AcceptInsecure(params.AcceptInsecureSecretStorage)
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
					secret.AcceptInsecure(params.AcceptInsecureSecretStorage)
					status := calendar.GetTokenStatus()

					if !status.HasToken && !status.HasCredentials {
						fmt.Println("Not authenticated.")
						fmt.Println("Run: oh-shit-meeting auth --credentials <file>")
						fmt.Println("  or: oh-shit-meeting auth --interactive")
						return
					}

					if status.HasCredentials {
						fmt.Printf("Client ID: %s\n", status.ClientID)
						fmt.Printf("Client secret: stored in %s\n", describeLoc(status.CredentialsLocation))
					} else {
						fmt.Println("Credentials: not configured")
					}

					if status.HasToken {
						fmt.Printf("Token: stored in %s\n", describeLoc(status.TokenLocation))
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
				Short: "Remove the stored Google OAuth token",
				RunFunc: func(params *LogoutParams, cmd *cobra.Command, args []string) {
					secret.AcceptInsecure(params.AcceptInsecureSecretStorage)
					if !calendar.HasGoogleToken() {
						fmt.Println("No stored token.")
						return
					}
					if err := calendar.Logout(); err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						os.Exit(1)
					}
					fmt.Println("Token removed.")
				},
			},
			boa.CmdT[ListEventsParams]{
				Use:   "list-events",
				Short: "List upcoming calendar events (live integration test)",
				RunFunc: func(params *ListEventsParams, cmd *cobra.Command, args []string) {
					secret.AcceptInsecure(params.AcceptInsecureSecretStorage)
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
	if params.DisplayTestAlert {
		runTestAlert(params)
		return
	}

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

	// Clean up ack files older than 7 days
	ack.Cleanup(7 * 24 * time.Hour)

	store := &eventStore{}
	ackStore := &ack.FileStore{}
	if err := gui.Init(gui.Config{
		Port:     params.Port,
		EventsFn: store.get,
		IsEventAckedFn: func(eventID string, startTime time.Time) bool {
			return ackStore.IsAcked(reminder.AckEventKey(eventID, startTime), reminder.EventAckID)
		},
		AckEventFn: func(eventID string, startTime time.Time) error {
			return ackStore.MarkAcked(reminder.AckEventKey(eventID, startTime), reminder.EventAckID)
		},
		UnackEventFn: func(eventID string, startTime time.Time) error {
			return ackStore.Unack(reminder.AckEventKey(eventID, startTime), reminder.EventAckID)
		},
	}); err != nil {
		slog.Error("failed to init dashboard", "error", err)
		os.Exit(1)
	}
	go runLoop(params, store, ackStore)
	gui.Run()
}

func runTestAlert(params *Params) {
	if err := gui.Init(gui.Config{
		Port:     params.Port,
		EventsFn: func() []calendar.Event { return nil },
	}); err != nil {
		slog.Error("failed to init dashboard", "error", err)
		os.Exit(1)
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		start := time.Now().Add(2 * time.Minute)
		slog.Info("firing test alert", "dashboard", fmt.Sprintf("http://127.0.0.1:%d/", params.Port))
		gui.ShowPopupBlocking(gui.ReminderInfo{
			Summary:       "TEST ALERT — oh shit, a meeting!",
			StartTime:     start,
			EndTime:       start.Add(30 * time.Minute),
			TimeUntil:     time.Until(start),
			ReminderID:    "test-alert",
			Sound:         params.Sound,
			Location:      "Your screen",
			OrganizerName: "oh-shit-meeting self-test",
			Fullscreen:    params.Fullscreen,
			Calendar:      "Test calendar",
			Description: "<p>This is a <b>synthetic</b> alert fired with <code>--display-test-alert</code>.</p>" +
				"<p>The real alert renders the event description here — sanitized against an allowlist so tags like <a href=\"https://example.com\">safe links</a>, <i>italics</i>, and lists work, but <code>&lt;script&gt;</code> and event handlers are stripped.</p>" +
				"<ul><li>Attendees below</li><li>Join Meet button above</li><li>Open in Google Calendar link at the bottom</li></ul>" +
				"<p>Acknowledge to exit.</p>",
			HangoutLink:   "https://meet.google.com/test-test-test",
			HtmlLink:      "https://calendar.google.com/",
			Attendees: []gui.Attendee{
				{DisplayName: "You", Email: "you@example.com", ResponseStatus: "accepted", Self: true},
				{DisplayName: "A colleague", Email: "colleague@example.com", ResponseStatus: "accepted"},
				{DisplayName: "Someone busy", Email: "busy@example.com", ResponseStatus: "tentative"},
			},
		})
		slog.Info("test alert acknowledged — exiting in 2s")
		// Give the page time to poll /state once more and flip back to the
		// dashboard view so the user sees confirmation before we exit.
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
	gui.Run()
}

type eventStore struct {
	mu     sync.RWMutex
	events []calendar.Event
}

func (s *eventStore) get() []calendar.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.events
}

func (s *eventStore) set(e []calendar.Event) {
	s.mu.Lock()
	s.events = e
	s.mu.Unlock()
}

func runLoop(params *Params, store *eventStore, ackStore *ack.FileStore) {
	clock := &reminder.RealClock{}
	finder := reminder.NewFinder(ackStore, clock, reminder.Config{
		WarnBefore: params.WarnBefore,
		Sound:      params.Sound,
	})

	// Poll calendar in a separate goroutine so slow/hung API calls
	// never block the alert check loop.
	go func() {
		for {
			calendar.ReAuthIfStale()
			store.set(calendar.Poll(params.Backend, params.LookaheadDays))
			time.Sleep(params.PollInterval)
		}
	}()

	// Check for reminders every second, independent of polling.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		evts := store.get()

		info := finder.FindNext(evts)
		if info != nil {
			slog.Warn("MEETING STARTING SOON",
				"event", info.Event.Summary,
				"startsIn", format.Duration(info.TimeUntil),
				"startTime", info.StartTime.Local().Format("15:04"),
				"location", info.Event.Location,
				"source", info.ReminderID,
			)

			var endTime time.Time
			if info.Event.End.DateTime != "" {
				endTime, _ = time.Parse(time.RFC3339, info.Event.End.DateTime)
			}
			attendees := make([]gui.Attendee, 0, len(info.Event.Attendees))
			for _, a := range info.Event.Attendees {
				attendees = append(attendees, gui.Attendee{
					Email:          a.Email,
					DisplayName:    a.DisplayName,
					ResponseStatus: a.ResponseStatus,
					Self:           a.Self,
					Organizer:      a.Organizer,
				})
			}
			gui.ShowPopupBlocking(gui.ReminderInfo{
				Summary:        info.Event.Summary,
				StartTime:      info.StartTime,
				EndTime:        endTime,
				TimeUntil:      info.TimeUntil,
				ReminderID:     info.ReminderID,
				Sound:          info.Sound,
				Location:       info.Event.Location,
				OrganizerName:  info.Event.Organizer.DisplayName,
				OrganizerEmail: info.Event.Organizer.Email,
				Fullscreen:     params.Fullscreen,
				Calendar:       info.Event.Calendar,
				Description:    info.Event.Description,
				HangoutLink:    info.Event.HangoutLink,
				HtmlLink:       info.Event.HtmlLink,
				Attendees:      attendees,
			})

			if err := ackStore.MarkAcked(info.AckEventKey, info.ReminderID); err != nil {
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
