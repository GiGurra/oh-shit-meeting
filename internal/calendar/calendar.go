package calendar

import (
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// Fetcher defines the interface for fetching calendar events
type Fetcher interface {
	FetchEvents(from, to string) ([]Event, error)
}

// DefaultFetcher picks gws if available, otherwise falls back to gog
type DefaultFetcher struct {
	Backend string
}

func (f *DefaultFetcher) FetchEvents(from, to string) ([]Event, error) {
	events, _, err := FetchEvents(from, to, f.Backend)
	return events, err
}

type Event struct {
	ID        string    `json:"id"`
	Summary   string    `json:"summary"`
	Start     EventTime `json:"start"`
	End       EventTime `json:"end"`
	EventType string    `json:"eventType"`
	Location  string    `json:"location,omitempty"`
	Status    string    `json:"status"`
	Reminders Reminders `json:"reminders"`
	Organizer Organizer `json:"organizer,omitempty"`
	// Calendar is the human-readable name of the source calendar
	// (display override > summary > id). Populated by the native Google
	// backend; left empty by gws/gog backends.
	Calendar    string     `json:"calendar,omitempty"`
	Description string     `json:"description,omitempty"`
	HangoutLink string     `json:"hangoutLink,omitempty"`
	HtmlLink    string     `json:"htmlLink,omitempty"`
	Attendees   []Attendee `json:"attendees,omitempty"`
}

type Attendee struct {
	Email          string `json:"email,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Self           bool   `json:"self,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
}

type Organizer struct {
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
}

type EventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type Reminders struct {
	UseDefault bool               `json:"useDefault"`
	Overrides  []ReminderOverride `json:"overrides,omitempty"`
}

type ReminderOverride struct {
	Method  string `json:"method"`
	Minutes int    `json:"minutes"`
}

// FetchEvents returns events and the name of the backend that was used.
func FetchEvents(from, to, backend string) ([]Event, string, error) {
	switch backend {
	case "google":
		events, err := fetchEventsGoogle(from, to)
		return events, "gcal-native", err
	case "gws":
		events, err := fetchEventsGWS(from, to)
		return events, "gws", err
	case "gog":
		events, err := fetchEventsGog(from, to)
		return events, "gogcli", err
	default: // "auto" or empty
		// Use native Google API if authenticated
		if HasGoogleToken() && HasGoogleCredentials() {
			events, err := fetchEventsGoogle(from, to)
			return events, "gcal-native", err
		}
		return nil, "", fmt.Errorf("no calendar backend available — run 'oh-shit-meeting auth --credentials <file>'")
	}
}

// LookbackStart returns the earlier of (now - minLookback) and the start of
// now's calendar day (in now's own location). This keeps a rolling minimum
// window but always covers everything from 00:00 today so the dashboard can
// show events earlier in the day even hours later.
func LookbackStart(now time.Time, minLookback time.Duration) time.Time {
	rolling := now.Add(-minLookback)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if startOfDay.Before(rolling) {
		return startOfDay
	}
	return rolling
}

// Poll fetches events from Google Calendar and returns valid events only.
// lookaheadDays controls how far ahead to look (0 defaults to 3 days).
func Poll(backend string, lookaheadDays int) []Event {
	if lookaheadDays <= 0 {
		lookaheadDays = 3
	}
	now := time.Now()
	from := LookbackStart(now, 1*time.Hour).Format(time.RFC3339)
	to := now.Add(time.Duration(lookaheadDays) * 24 * time.Hour).Format(time.RFC3339)

	events, usedBackend, err := FetchEvents(from, to, backend)
	if err != nil {
		slog.Error("Failed to fetch calendar events", "error", err)
		return nil
	}

	// Filter to valid events only, log warnings for invalid ones
	var validEvents []Event
	for _, event := range events {
		if event.Start.DateTime == "" || event.EventType == "workingLocation" {
			continue
		}

		_, err := time.Parse(time.RFC3339, event.Start.DateTime)
		if err != nil {
			slog.Warn("Failed to parse event start time",
				"event", event.Summary,
				"startTime", event.Start.DateTime,
				"error", err,
			)
			continue
		}

		validEvents = append(validEvents, event)
	}

	// Sort by start time (earliest first)
	sort.Slice(validEvents, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, validEvents[i].Start.DateTime)
		tj, _ := time.Parse(time.RFC3339, validEvents[j].Start.DateTime)
		return ti.Before(tj)
	})

	slog.Info("Polled Google Calendar", "backend", usedBackend, "eventCount", len(validEvents))
	return validEvents
}
