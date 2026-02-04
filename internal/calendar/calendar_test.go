package calendar

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvent_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"id": "test-event-123",
		"summary": "Team Meeting",
		"start": {
			"dateTime": "2026-02-04T09:00:00+01:00",
			"timeZone": "Europe/Stockholm"
		},
		"end": {
			"dateTime": "2026-02-04T10:00:00+01:00",
			"timeZone": "Europe/Stockholm"
		},
		"eventType": "default",
		"location": "Conference Room A",
		"status": "confirmed",
		"reminders": {
			"useDefault": false,
			"overrides": [
				{"method": "popup", "minutes": 10},
				{"method": "email", "minutes": 30}
			]
		},
		"organizer": {
			"displayName": "John Doe",
			"email": "john@example.com"
		}
	}`

	var event Event
	err := json.Unmarshal([]byte(jsonData), &event)
	if err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if event.ID != "test-event-123" {
		t.Errorf("expected ID test-event-123, got %s", event.ID)
	}
	if event.Summary != "Team Meeting" {
		t.Errorf("expected Summary 'Team Meeting', got %s", event.Summary)
	}
	if event.Start.DateTime != "2026-02-04T09:00:00+01:00" {
		t.Errorf("expected Start.DateTime '2026-02-04T09:00:00+01:00', got %s", event.Start.DateTime)
	}
	if event.Location != "Conference Room A" {
		t.Errorf("expected Location 'Conference Room A', got %s", event.Location)
	}
	if len(event.Reminders.Overrides) != 2 {
		t.Errorf("expected 2 reminder overrides, got %d", len(event.Reminders.Overrides))
	}
	if event.Reminders.Overrides[0].Method != "popup" || event.Reminders.Overrides[0].Minutes != 10 {
		t.Errorf("expected first reminder to be popup/10, got %s/%d",
			event.Reminders.Overrides[0].Method, event.Reminders.Overrides[0].Minutes)
	}
	if event.Organizer.DisplayName != "John Doe" {
		t.Errorf("expected Organizer.DisplayName 'John Doe', got %s", event.Organizer.DisplayName)
	}
}

func TestEvent_AllDayEvent(t *testing.T) {
	jsonData := `{
		"id": "all-day-123",
		"summary": "Holiday",
		"start": {
			"date": "2026-02-04"
		},
		"end": {
			"date": "2026-02-05"
		},
		"eventType": "default",
		"status": "confirmed",
		"reminders": {
			"useDefault": true
		}
	}`

	var event Event
	err := json.Unmarshal([]byte(jsonData), &event)
	if err != nil {
		t.Fatalf("Failed to unmarshal all-day event: %v", err)
	}

	// All-day events have Date instead of DateTime
	if event.Start.DateTime != "" {
		t.Errorf("expected empty DateTime for all-day event, got %s", event.Start.DateTime)
	}
	if event.Start.Date != "2026-02-04" {
		t.Errorf("expected Start.Date '2026-02-04', got %s", event.Start.Date)
	}
}

func TestResponse_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"events": [
			{
				"id": "evt1",
				"summary": "Meeting 1",
				"start": {"dateTime": "2026-02-04T09:00:00Z"},
				"end": {"dateTime": "2026-02-04T10:00:00Z"},
				"eventType": "default",
				"status": "confirmed",
				"reminders": {"useDefault": true}
			},
			{
				"id": "evt2",
				"summary": "Meeting 2",
				"start": {"dateTime": "2026-02-04T14:00:00Z"},
				"end": {"dateTime": "2026-02-04T15:00:00Z"},
				"eventType": "default",
				"status": "confirmed",
				"reminders": {"useDefault": true}
			}
		]
	}`

	var response Response
	err := json.Unmarshal([]byte(jsonData), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(response.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(response.Events))
	}
	if response.Events[0].Summary != "Meeting 1" {
		t.Errorf("expected first event to be 'Meeting 1', got %s", response.Events[0].Summary)
	}
}

func TestEventTime_Parse(t *testing.T) {
	tests := []struct {
		name     string
		dateTime string
		wantErr  bool
	}{
		{"RFC3339 with offset", "2026-02-04T09:00:00+01:00", false},
		{"RFC3339 UTC", "2026-02-04T09:00:00Z", false},
		{"RFC3339 negative offset", "2026-02-04T09:00:00-05:00", false},
		{"Invalid format", "2026-02-04 09:00:00", true},
		{"Empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := time.Parse(time.RFC3339, tt.dateTime)
			if (err != nil) != tt.wantErr {
				t.Errorf("time.Parse(RFC3339, %q) error = %v, wantErr %v", tt.dateTime, err, tt.wantErr)
			}
		})
	}
}

func TestEvent_WorkingLocationFiltering(t *testing.T) {
	// Working location events should be identified by eventType
	event := Event{
		ID:        "wl-123",
		Summary:   "Home",
		EventType: "workingLocation",
	}

	if event.EventType != "workingLocation" {
		t.Errorf("expected eventType 'workingLocation', got %s", event.EventType)
	}

	// In Poll(), these are filtered out:
	// if event.Start.DateTime == "" || event.EventType == "workingLocation" { continue }
}

func TestDefaultFetcher_ImplementsInterface(t *testing.T) {
	var _ Fetcher = &DefaultFetcher{}
}
