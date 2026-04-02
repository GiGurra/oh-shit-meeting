package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const (
	keyringService = "oh-shit-meeting"
	keyringUser    = "google-oauth-token"
)

// configDir returns the app's config directory (~/.oh-shit-meeting/).
func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".oh-shit-meeting"
	}
	return filepath.Join(home, ".oh-shit-meeting")
}

// configFilePath returns the path to the config file that stores the credentials path.
func configFilePath() string {
	return filepath.Join(configDir(), "config.json")
}

const keyringClientSecretUser = "google-client-secret"

type appConfig struct {
	GoogleCredentials string `json:"google_credentials,omitempty"`
	GoogleClientID    string `json:"google_client_id,omitempty"`
}

func loadAppConfig() appConfig {
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		return appConfig{}
	}
	var cfg appConfig
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func saveAppConfig(cfg appConfig) error {
	if err := os.MkdirAll(configDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(configFilePath(), data, 0644)
}

// loadCredentials reads the OAuth client credentials JSON and builds an oauth2.Config
// scoped to read-only calendar access.
func loadCredentials(credentialsPath string) (*oauth2.Config, error) {
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	cfg, err := google.ConfigFromJSON(data, gcal.CalendarReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return cfg, nil
}

func oauthConfigFromClientIDSecret(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{gcal.CalendarReadonlyScope},
		Endpoint:     google.Endpoint,
	}
}

// resolveOAuthConfig returns an oauth2.Config from saved credentials (file or client ID/secret).
func resolveOAuthConfig() (*oauth2.Config, error) {
	cfg := loadAppConfig()
	if cfg.GoogleClientID != "" {
		secret, err := keyring.Get(keyringService, keyringClientSecretUser)
		if err == nil && secret != "" {
			return oauthConfigFromClientIDSecret(cfg.GoogleClientID, secret), nil
		}
	}
	if cfg.GoogleCredentials != "" {
		return loadCredentials(cfg.GoogleCredentials)
	}
	return nil, fmt.Errorf("no Google credentials configured — run 'oh-shit-meeting auth'")
}

func loadToken() (*oauth2.Token, error) {
	data, err := keyring.Get(keyringService, keyringUser)
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(data), &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func saveToken(tok *oauth2.Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, keyringUser, string(data))
}

// HasGoogleToken returns true if a saved OAuth token exists in the keychain.
func HasGoogleToken() bool {
	_, err := keyring.Get(keyringService, keyringUser)
	return err == nil
}

// GoogleCredentialsPath returns the saved credentials path, or empty if not configured.
func GoogleCredentialsPath() string {
	return loadAppConfig().GoogleCredentials
}

// TokenStatus contains information about the stored OAuth token.
type TokenStatus struct {
	HasToken        bool
	TokenType       string
	Expiry          time.Time
	HasRefreshToken bool
	HasCredentials  bool
	CredentialType  string // "file" or "client_id"
	CredentialInfo  string // file path or client ID
}

// GetTokenStatus returns the current authentication status.
func GetTokenStatus() TokenStatus {
	status := TokenStatus{}

	tok, err := loadToken()
	if err == nil {
		status.HasToken = true
		status.TokenType = tok.TokenType
		status.Expiry = tok.Expiry
		status.HasRefreshToken = tok.RefreshToken != ""
	}

	cfg := loadAppConfig()
	if cfg.GoogleCredentials != "" {
		status.HasCredentials = true
		status.CredentialType = "file"
		status.CredentialInfo = cfg.GoogleCredentials
	} else if cfg.GoogleClientID != "" {
		secret, err := keyring.Get(keyringService, keyringClientSecretUser)
		if err == nil && secret != "" {
			status.HasCredentials = true
			status.CredentialType = "client_id"
			status.CredentialInfo = cfg.GoogleClientID
		}
	}

	return status
}

// HasGoogleCredentials returns true if any form of Google credentials is configured.
func HasGoogleCredentials() bool {
	cfg := loadAppConfig()
	if cfg.GoogleCredentials != "" {
		return true
	}
	if cfg.GoogleClientID != "" {
		secret, err := keyring.Get(keyringService, keyringClientSecretUser)
		return err == nil && secret != ""
	}
	return false
}

// HasStoredClientCredentials returns true if client ID + secret are saved from a previous auth.
func HasStoredClientCredentials() bool {
	cfg := loadAppConfig()
	if cfg.GoogleClientID == "" {
		return false
	}
	secret, err := keyring.Get(keyringService, keyringClientSecretUser)
	return err == nil && secret != ""
}

// ReAuthenticate re-runs the OAuth2 flow using previously stored credentials.
func ReAuthenticate() error {
	oauthCfg, err := resolveOAuthConfig()
	if err != nil {
		return err
	}
	return doBrowserAuth(oauthCfg)
}

// Authenticate runs the OAuth2 authorization code flow using a credentials JSON file.
func Authenticate(credentialsPath string) error {
	oauthCfg, err := loadCredentials(credentialsPath)
	if err != nil {
		return err
	}

	if err := doBrowserAuth(oauthCfg); err != nil {
		return err
	}

	// Save credentials path to config for future use (clears client ID if previously set)
	absPath, _ := filepath.Abs(credentialsPath)
	cfg := loadAppConfig()
	cfg.GoogleCredentials = absPath
	cfg.GoogleClientID = ""
	if err := saveAppConfig(cfg); err != nil {
		slog.Warn("Could not save credentials path to config", "error", err)
	}
	// Clean up any previous client secret from keychain
	_ = keyring.Delete(keyringService, keyringClientSecretUser)
	return nil
}

// AuthenticateWithClientIDSecret runs the OAuth2 flow using a client ID and secret directly.
func AuthenticateWithClientIDSecret(clientID, clientSecret string) error {
	oauthCfg := oauthConfigFromClientIDSecret(clientID, clientSecret)

	if err := doBrowserAuth(oauthCfg); err != nil {
		return err
	}

	// Save client secret to keychain
	if err := keyring.Set(keyringService, keyringClientSecretUser, clientSecret); err != nil {
		return fmt.Errorf("save client secret to keychain: %w", err)
	}

	// Save client ID to config (not secret — it's in keychain)
	cfg := loadAppConfig()
	cfg.GoogleClientID = clientID
	cfg.GoogleCredentials = ""
	if err := saveAppConfig(cfg); err != nil {
		slog.Warn("Could not save config", "error", err)
	}
	return nil
}

// doBrowserAuth runs the OAuth2 browser flow: opens browser, receives callback, exchanges token.
func doBrowserAuth(oauthCfg *oauth2.Config) error {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("start callback listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	oauthCfg.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", port)

	authURL := oauthCfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			fmt.Fprintln(w, "Error: no authorization code received.")
			return
		}
		codeCh <- code
		fmt.Fprintln(w, "Authorization successful! You can close this tab.")
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	fmt.Println("Opening browser for Google authorization...")
	fmt.Printf("If the browser doesn't open, visit this URL:\n%s\n\n", authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		srv.Close()
		return fmt.Errorf("callback error: %w", err)
	case <-time.After(5 * time.Minute):
		srv.Close()
		return fmt.Errorf("timed out waiting for authorization (5 minutes)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	tok, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange token: %w", err)
	}

	if err := saveToken(tok); err != nil {
		return fmt.Errorf("save token to keychain: %w", err)
	}

	fmt.Println("Token saved to system keychain.")
	return nil
}

// Logout removes the stored OAuth2 token from the system keychain.
func Logout() error {
	return keyring.Delete(keyringService, keyringUser)
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}

// newGoogleService creates an authenticated Google Calendar API service.
func newGoogleService() (*gcal.Service, error) {
	oauthCfg, err := resolveOAuthConfig()
	if err != nil {
		return nil, err
	}

	tok, err := loadToken()
	if err != nil {
		return nil, fmt.Errorf("no saved token — run 'oh-shit-meeting auth' first: %w", err)
	}

	// Create a token source that auto-refreshes
	src := oauthCfg.TokenSource(context.Background(), tok)
	newTok, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("token refresh failed — run 'oh-shit-meeting auth' to re-authenticate: %w", err)
	}
	if newTok.AccessToken != tok.AccessToken {
		saveToken(newTok)
	}

	client := oauth2.NewClient(context.Background(), src)

	svc, err := gcal.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}
	return svc, nil
}

func fetchEventsGoogle(from, to string) ([]Event, error) {
	svc, err := newGoogleService()
	if err != nil {
		return nil, err
	}

	calendars, err := svc.CalendarList.List().Do()
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	var allEvents []Event
	for _, cal := range calendars.Items {
		events, err := fetchGoogleCalendarEvents(svc, cal.Id, from, to)
		if err != nil {
			slog.Warn("Failed to fetch events for calendar, skipping",
				"calendar", cal.Summary, "error", err)
			continue
		}
		allEvents = append(allEvents, events...)
	}
	return allEvents, nil
}

func fetchGoogleCalendarEvents(svc *gcal.Service, calendarID, from, to string) ([]Event, error) {
	var allEvents []Event
	pageToken := ""

	for {
		call := svc.Events.List(calendarID).
			TimeMin(from).
			TimeMax(to).
			SingleEvents(true).
			OrderBy("startTime").
			MaxResults(2500)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, err
		}

		for _, item := range resp.Items {
			ev := Event{
				ID:        item.Id,
				Summary:   item.Summary,
				EventType: item.EventType,
				Location:  item.Location,
				Status:    item.Status,
			}

			if item.Organizer != nil {
				ev.Organizer = Organizer{
					DisplayName: item.Organizer.DisplayName,
					Email:       item.Organizer.Email,
				}
			}

			if item.Start != nil {
				ev.Start = EventTime{
					DateTime: item.Start.DateTime,
					Date:     item.Start.Date,
					TimeZone: item.Start.TimeZone,
				}
			}
			if item.End != nil {
				ev.End = EventTime{
					DateTime: item.End.DateTime,
					Date:     item.End.Date,
					TimeZone: item.End.TimeZone,
				}
			}

			if item.Reminders != nil {
				ev.Reminders.UseDefault = item.Reminders.UseDefault
				for _, o := range item.Reminders.Overrides {
					ev.Reminders.Overrides = append(ev.Reminders.Overrides, ReminderOverride{
						Method:  o.Method,
						Minutes: int(o.Minutes),
					})
				}
			}

			allEvents = append(allEvents, ev)
		}

		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return allEvents, nil
}
