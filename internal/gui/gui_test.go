package gui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gigurra/oh-shit-meeting/internal/calendar"
)

func mkEvent(id, summary string, start, end time.Time) calendar.Event {
	return calendar.Event{
		ID:      id,
		Summary: summary,
		Start:   calendar.EventTime{DateTime: start.Format(time.RFC3339)},
		End:     calendar.EventTime{DateTime: end.Format(time.RFC3339)},
	}
}

func TestSplitEvents_SplitsAroundNow(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	events := []calendar.Event{
		mkEvent("past", "Past", now.Add(-30*time.Minute), now.Add(-10*time.Minute)),
		mkEvent("future", "Future", now.Add(10*time.Minute), now.Add(30*time.Minute)),
	}

	previous, upcoming := splitEvents(events, now, nil)
	if len(previous) != 1 || previous[0].ID != "past" {
		t.Errorf("expected previous to contain past event, got %+v", previous)
	}
	if len(upcoming) != 1 || upcoming[0].ID != "future" {
		t.Errorf("expected upcoming to contain future event, got %+v", upcoming)
	}
}

func TestSplitEvents_KeepsEarlierEventsFromSameDay(t *testing.T) {
	// At 16:00, lookback should extend to 00:00 (start-of-day), so a 09:00
	// event from the same day is still listed under "previous".
	now := time.Date(2026, 4, 20, 16, 0, 0, 0, time.UTC)
	events := []calendar.Event{
		mkEvent("morning", "Morning", time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC), time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)),
	}

	previous, _ := splitEvents(events, now, nil)
	if len(previous) != 1 || previous[0].ID != "morning" {
		t.Errorf("expected morning event in previous, got %+v", previous)
	}
}

func TestSplitEvents_DropsEventsBeforeStartOfToday(t *testing.T) {
	// An event from yesterday afternoon is outside the window.
	now := time.Date(2026, 4, 20, 16, 0, 0, 0, time.UTC)
	events := []calendar.Event{
		mkEvent("yesterday", "Yesterday", time.Date(2026, 4, 19, 14, 0, 0, 0, time.UTC), time.Date(2026, 4, 19, 15, 0, 0, 0, time.UTC)),
		mkEvent("today", "Today", time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC), time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)),
	}

	previous, _ := splitEvents(events, now, nil)
	if len(previous) != 1 || previous[0].ID != "today" {
		t.Errorf("expected only today's event, got %+v", previous)
	}
}

func TestSplitEvents_JustAfterMidnightUsesRollingHour(t *testing.T) {
	// At 00:30 the rolling -1h (23:30 prev day) is earlier than start-of-day
	// (today 00:00), so a 23:45 event from yesterday should still show.
	now := time.Date(2026, 4, 20, 0, 30, 0, 0, time.UTC)
	events := []calendar.Event{
		mkEvent("late-yesterday", "Late", time.Date(2026, 4, 19, 23, 45, 0, 0, time.UTC), time.Date(2026, 4, 20, 0, 15, 0, 0, time.UTC)),
	}

	previous, _ := splitEvents(events, now, nil)
	if len(previous) != 1 || previous[0].ID != "late-yesterday" {
		t.Errorf("expected late-yesterday event to show right after midnight, got %+v", previous)
	}
}

func TestSplitEvents_PopulatesAckedFlag(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	acked := mkEvent("ack", "Acked", now.Add(5*time.Minute), now.Add(30*time.Minute))
	notAcked := mkEvent("no-ack", "Clear", now.Add(10*time.Minute), now.Add(40*time.Minute))

	isAcked := func(id string, _ time.Time) bool { return id == "ack" }
	_, upcoming := splitEvents([]calendar.Event{acked, notAcked}, now, isAcked)

	if len(upcoming) != 2 {
		t.Fatalf("expected 2 upcoming events, got %d", len(upcoming))
	}
	byID := map[string]eventDTO{}
	for _, e := range upcoming {
		byID[e.ID] = e
	}
	if !byID["ack"].Acked {
		t.Error("expected event 'ack' to be flagged Acked=true")
	}
	if byID["no-ack"].Acked {
		t.Error("expected event 'no-ack' to be flagged Acked=false")
	}
}

func TestSplitEvents_PassesStartTimeToAckLookup(t *testing.T) {
	// Verifies the ack lookup receives the parsed start time, not a zero value —
	// the ack key depends on it, so getting it wrong would silently fail.
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	start := now.Add(5 * time.Minute)
	e := mkEvent("evt", "Stamped", start, now.Add(30*time.Minute))

	var seenStart time.Time
	lookup := func(id string, st time.Time) bool {
		seenStart = st
		return false
	}
	_, _ = splitEvents([]calendar.Event{e}, now, lookup)

	if !seenStart.Equal(start) {
		t.Errorf("expected lookup to receive start=%v, got %v", start, seenStart)
	}
}

func TestSplitEvents_SkipsEventsWithNoStartTime(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "no-start", Summary: "Missing"},
		mkEvent("ok", "OK", now.Add(5*time.Minute), now.Add(30*time.Minute)),
	}

	previous, upcoming := splitEvents(events, now, nil)
	if len(previous) != 0 {
		t.Errorf("expected no previous, got %+v", previous)
	}
	if len(upcoming) != 1 || upcoming[0].ID != "ok" {
		t.Errorf("expected only 'ok' upcoming, got %+v", upcoming)
	}
}

func TestSplitEvents_NilIsAckedIsSafe(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	e := mkEvent("evt", "OK", now.Add(5*time.Minute), now.Add(30*time.Minute))
	_, upcoming := splitEvents([]calendar.Event{e}, now, nil)
	if len(upcoming) != 1 || upcoming[0].Acked {
		t.Errorf("expected single non-acked event, got %+v", upcoming)
	}
}

// withStubConfig swaps the package-level cfg for the duration of the test
// and restores it after. Allows exercising the HTTP handlers against fakes.
func withStubConfig(t *testing.T, c Config) {
	t.Helper()
	prev := cfg
	cfg = c
	t.Cleanup(func() { cfg = prev })
}

func TestHandleEventAck_CallsAckFunc(t *testing.T) {
	var gotID string
	var gotStart time.Time
	withStubConfig(t, Config{
		AckEventFn: func(id string, st time.Time) error {
			gotID, gotStart = id, st
			return nil
		},
	})

	start := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodPost, "/ack-event?eventId=evt1&startTime="+start.Format(time.RFC3339), nil)
	w := httptest.NewRecorder()

	handleEventAck(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d (%s)", w.Code, w.Body.String())
	}
	if gotID != "evt1" {
		t.Errorf("expected eventId=evt1, got %q", gotID)
	}
	if !gotStart.Equal(start) {
		t.Errorf("expected startTime %v, got %v", start, gotStart)
	}
}

func TestHandleEventUnack_CallsUnackFunc(t *testing.T) {
	called := false
	withStubConfig(t, Config{
		UnackEventFn: func(id string, st time.Time) error {
			called = true
			return nil
		},
	})

	start := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodPost, "/unack-event?eventId=evt1&startTime="+start.Format(time.RFC3339), nil)
	w := httptest.NewRecorder()

	handleEventUnack(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
	if !called {
		t.Error("expected UnackEventFn to be called")
	}
}

func TestHandleEventAck_RejectsNonPost(t *testing.T) {
	withStubConfig(t, Config{AckEventFn: func(string, time.Time) error { return nil }})
	req := httptest.NewRequest(http.MethodGet, "/ack-event?eventId=evt1&startTime=2026-04-20T12:00:00Z", nil)
	w := httptest.NewRecorder()
	handleEventAck(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleEventAck_RejectsMissingParams(t *testing.T) {
	withStubConfig(t, Config{AckEventFn: func(string, time.Time) error { return nil }})
	req := httptest.NewRequest(http.MethodPost, "/ack-event?eventId=evt1", nil)
	w := httptest.NewRecorder()
	handleEventAck(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing startTime, got %d", w.Code)
	}
}

func TestHandleEventAck_RejectsBadStartTime(t *testing.T) {
	withStubConfig(t, Config{AckEventFn: func(string, time.Time) error { return nil }})
	req := httptest.NewRequest(http.MethodPost, "/ack-event?eventId=evt1&startTime=not-a-time", nil)
	w := httptest.NewRecorder()
	handleEventAck(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid startTime, got %d", w.Code)
	}
}
