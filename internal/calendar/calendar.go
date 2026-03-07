package calendar

import (
	"log/slog"
	"os/exec"
	"sort"
	"time"
)

// Fetcher defines the interface for fetching calendar events
type Fetcher interface {
	FetchEvents(from, to string) ([]Event, error)
}

// DefaultFetcher picks gws if available, otherwise falls back to gog
type DefaultFetcher struct{}

func (f *DefaultFetcher) FetchEvents(from, to string) ([]Event, error) {
	return FetchEvents(from, to)
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

func FetchEvents(from, to string) ([]Event, error) {
	if _, err := exec.LookPath("gws"); err == nil {
		return fetchEventsGWS(from, to)
	}
	slog.Info("gws not found, falling back to gog")
	return fetchEventsGog(from, to)
}

// Poll fetches events from Google Calendar and returns valid events only
func Poll() []Event {
	now := time.Now()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(72 * time.Hour).Format(time.RFC3339)

	events, err := FetchEvents(from, to)
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

	slog.Info("Polled Google Calendar", "eventCount", len(validEvents))
	return validEvents
}
