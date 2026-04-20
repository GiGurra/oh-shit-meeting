package reminder

import (
	"testing"
	"time"

	"github.com/gigurra/oh-shit-meeting/internal/calendar"
)

// mockClock is a controllable clock for testing
type mockClock struct {
	now time.Time
}

func (c *mockClock) Now() time.Time {
	return c.now
}

// mockAckStore tracks acknowledgments in memory
type mockAckStore struct {
	acked map[string]bool
}

func newMockAckStore() *mockAckStore {
	return &mockAckStore{acked: make(map[string]bool)}
}

func (s *mockAckStore) IsAcked(eventID, reminderID string) bool {
	return s.acked[eventID+":"+reminderID]
}

func (s *mockAckStore) MarkAcked(eventID, reminderID string) error {
	s.acked[eventID+":"+reminderID] = true
	return nil
}

func (s *mockAckStore) Unack(eventID, reminderID string) error {
	delete(s.acked, eventID+":"+reminderID)
	return nil
}

func (s *mockAckStore) setAcked(eventID, reminderID string) {
	s.acked[eventID+":"+reminderID] = true
}

// helper to create events easily
func makeEvent(id, summary string, start, end time.Time) calendar.Event {
	return calendar.Event{
		ID:      id,
		Summary: summary,
		Start:   calendar.EventTime{DateTime: start.Format(time.RFC3339)},
		End:     calendar.EventTime{DateTime: end.Format(time.RFC3339)},
	}
}

func makeEventWithReminders(id, summary string, start, end time.Time, reminderMinutes ...int) calendar.Event {
	event := makeEvent(id, summary, start, end)
	for _, mins := range reminderMinutes {
		event.Reminders.Overrides = append(event.Reminders.Overrides, calendar.ReminderOverride{
			Method:  "popup",
			Minutes: mins,
		})
	}
	return event
}

func TestFindNext_NoEvents(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	result := finder.FindNext(nil)
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}

	result = finder.FindNext([]calendar.Event{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %+v", result)
	}
}

func TestFindNext_EventFarInFuture(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 1 hour in the future - outside warn window
	event := makeEvent("evt1", "Future Meeting", now.Add(1*time.Hour), now.Add(2*time.Hour))
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for far future event, got %+v", result)
	}
}

func TestFindNext_EventInWarnWindow(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute, Sound: "Hero"})

	// Event 3 minutes in the future - inside 5 minute warn window
	start := now.Add(3 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Soon Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder, got nil")
	}
	if result.ReminderID != "global" {
		t.Errorf("expected global reminder, got %s", result.ReminderID)
	}
	if result.Event.ID != "evt1" {
		t.Errorf("expected event evt1, got %s", result.Event.ID)
	}
	if result.Sound != "Hero" {
		t.Errorf("expected sound Hero, got %s", result.Sound)
	}
}

func TestFindNext_EventExactlyAtWarnThreshold(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event exactly 5 minutes away
	start := now.Add(5 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Threshold Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder at exact threshold, got nil")
	}
	if result.ReminderID != "global" {
		t.Errorf("expected global reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_EventJustOutsideWarnWindow(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 5 minutes + 1 second away - just outside window
	start := now.Add(5*time.Minute + 1*time.Second)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Just Outside Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for event just outside warn window, got %+v", result)
	}
}

func TestFindNext_EventAlreadyStarted(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 30, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute, Sound: "Glass"})

	// Event started 15 minutes ago, ends in 15 minutes
	start := now.Add(-15 * time.Minute)
	end := now.Add(15 * time.Minute)
	event := makeEvent("evt1", "Ongoing Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected started reminder, got nil")
	}
	if result.ReminderID != "started" {
		t.Errorf("expected started reminder, got %s", result.ReminderID)
	}
	if result.TimeUntil >= 0 {
		t.Errorf("expected negative TimeUntil for started event, got %v", result.TimeUntil)
	}
	if result.Sound != "Glass" {
		t.Errorf("expected sound Glass, got %s", result.Sound)
	}
}

func TestFindNext_EventAlreadyEnded(t *testing.T) {
	now := time.Date(2026, 2, 4, 10, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event ended 30 minutes ago
	start := now.Add(-1 * time.Hour)
	end := now.Add(-30 * time.Minute)
	event := makeEvent("evt1", "Past Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for ended event, got %+v", result)
	}
}

func TestFindNext_EventEndedJustNow(t *testing.T) {
	now := time.Date(2026, 2, 4, 10, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event ended exactly now (end time == now, so now.After(endTime) is false)
	start := now.Add(-30 * time.Minute)
	end := now
	event := makeEvent("evt1", "Just Ended Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	// now.After(end) where end == now is false, so this should trigger "started"
	if result == nil {
		t.Fatal("expected started reminder for event ending exactly now, got nil")
	}
	if result.ReminderID != "started" {
		t.Errorf("expected started reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_CustomReminderOverride(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 8 minutes away with a 10-minute popup reminder
	start := now.Add(8 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEventWithReminders("evt1", "Custom Reminder Meeting", start, end, 10)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected custom reminder, got nil")
	}
	if result.ReminderID != "10m" {
		t.Errorf("expected 10m reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_CustomReminderNotYetTriggered(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 15 minutes away with a 10-minute popup reminder (not triggered yet)
	start := now.Add(15 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEventWithReminders("evt1", "Future Custom Meeting", start, end, 10)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for custom reminder not yet triggered, got %+v", result)
	}
}

func TestFindNext_MultipleCustomReminders(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 8 minutes away with 5, 10, and 15-minute reminders
	start := now.Add(8 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEventWithReminders("evt1", "Multi Reminder Meeting", start, end, 5, 10, 15)
	events := []calendar.Event{event}

	// Should trigger 10m (first one in list that applies)
	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder, got nil")
	}
	// 5m reminder: 8 min > 5 min, so not triggered
	// 10m reminder: 8 min <= 10 min, triggered
	if result.ReminderID != "10m" {
		t.Errorf("expected 10m reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_NonPopupRemindersIgnored(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 8 minutes away with email reminder only
	start := now.Add(8 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Email Reminder Meeting", start, end)
	event.Reminders.Overrides = []calendar.ReminderOverride{
		{Method: "email", Minutes: 10},
	}
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	// Should fall back to global reminder at 5m, which doesn't apply at 8m
	if result != nil {
		t.Errorf("expected nil (email reminders ignored, outside global window), got %+v", result)
	}
}

func TestFindNext_AlreadyAcknowledged(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 3 minutes in the future
	start := now.Add(3 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Acked Meeting", start, end)
	events := []calendar.Event{event}

	// Mark as already acknowledged (using composite ack key)
	ackStore.setAcked(AckEventKey("evt1", start), "global")

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for already acknowledged event, got %+v", result)
	}
}

func TestFindNext_StartedAlreadyAcknowledged(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 30, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event already started
	start := now.Add(-15 * time.Minute)
	end := now.Add(15 * time.Minute)
	event := makeEvent("evt1", "Acked Ongoing Meeting", start, end)
	events := []calendar.Event{event}

	// Mark started reminder as acknowledged
	ackStore.setAcked(AckEventKey("evt1", start), "started")

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for already acknowledged started reminder, got %+v", result)
	}
}

func TestFindNext_MultipleEvents_FirstApplies(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	events := []calendar.Event{
		makeEvent("evt1", "First Meeting", now.Add(3*time.Minute), now.Add(30*time.Minute)),
		makeEvent("evt2", "Second Meeting", now.Add(4*time.Minute), now.Add(1*time.Hour)),
	}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder, got nil")
	}
	if result.Event.ID != "evt1" {
		t.Errorf("expected first event evt1, got %s", result.Event.ID)
	}
}

func TestFindNext_MultipleEvents_FirstAcked(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	start1 := now.Add(3 * time.Minute)
	start2 := now.Add(4 * time.Minute)
	events := []calendar.Event{
		makeEvent("evt1", "First Meeting", start1, now.Add(30*time.Minute)),
		makeEvent("evt2", "Second Meeting", start2, now.Add(1*time.Hour)),
	}

	// First is already acknowledged
	ackStore.setAcked(AckEventKey("evt1", start1), "global")

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder for second event, got nil")
	}
	if result.Event.ID != "evt2" {
		t.Errorf("expected second event evt2, got %s", result.Event.ID)
	}
}

func TestFindNext_MultipleEvents_StartedTakesPriority(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 30, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Events sorted by start time
	events := []calendar.Event{
		makeEvent("evt1", "Ongoing Meeting", now.Add(-10*time.Minute), now.Add(20*time.Minute)),
		makeEvent("evt2", "Soon Meeting", now.Add(3*time.Minute), now.Add(1*time.Hour)),
	}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder, got nil")
	}
	// Started event should come first (it's earlier in the sorted list)
	if result.Event.ID != "evt1" {
		t.Errorf("expected ongoing event evt1, got %s", result.Event.ID)
	}
	if result.ReminderID != "started" {
		t.Errorf("expected started reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_InvalidStartTime(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	event := calendar.Event{
		ID:      "evt1",
		Summary: "Invalid Start",
		Start:   calendar.EventTime{DateTime: "not-a-valid-time"},
		End:     calendar.EventTime{DateTime: now.Add(1 * time.Hour).Format(time.RFC3339)},
	}
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for invalid start time, got %+v", result)
	}
}

func TestFindNext_InvalidEndTime(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	event := calendar.Event{
		ID:      "evt1",
		Summary: "Invalid End",
		Start:   calendar.EventTime{DateTime: now.Add(3 * time.Minute).Format(time.RFC3339)},
		End:     calendar.EventTime{DateTime: "not-a-valid-time"},
	}
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil for invalid end time, got %+v", result)
	}
}

func TestFindNext_ZeroWarnBefore(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 0})

	// Event 1 second away
	start := now.Add(1 * time.Second)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Imminent Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	// With WarnBefore=0, only events at or past start time should trigger
	if result != nil {
		t.Errorf("expected nil with zero warn window, got %+v", result)
	}
}

func TestFindNext_EventStartingExactlyNow(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event starting exactly now
	start := now
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Starting Now Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder for event starting now, got nil")
	}
	// timeUntil is 0, which is not < 0, so global should apply (0 <= 5min)
	if result.ReminderID != "global" {
		t.Errorf("expected global reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_CustomReminderAckedFallbackToGlobal(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 3 minutes away with 10-minute custom reminder (already acked)
	start := now.Add(3 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEventWithReminders("evt1", "Custom Then Global", start, end, 10)
	events := []calendar.Event{event}

	// Custom reminder is acked
	ackStore.setAcked(AckEventKey("evt1", start), "10m")

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected global fallback reminder, got nil")
	}
	if result.ReminderID != "global" {
		t.Errorf("expected global reminder as fallback, got %s", result.ReminderID)
	}
}

func TestFindNext_TimeUntilCalculation(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event 3 minutes 30 seconds in the future
	start := now.Add(3*time.Minute + 30*time.Second)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Time Check Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder, got nil")
	}

	expectedTimeUntil := 3*time.Minute + 30*time.Second
	if result.TimeUntil != expectedTimeUntil {
		t.Errorf("expected TimeUntil %v, got %v", expectedTimeUntil, result.TimeUntil)
	}
}

func TestFindNext_NegativeTimeUntilForStartedEvent(t *testing.T) {
	now := time.Date(2026, 2, 4, 9, 30, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event started 10 minutes ago
	start := now.Add(-10 * time.Minute)
	end := now.Add(20 * time.Minute)
	event := makeEvent("evt1", "Late Meeting", start, end)
	events := []calendar.Event{event}

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected started reminder, got nil")
	}

	expectedTimeUntil := -10 * time.Minute
	if result.TimeUntil != expectedTimeUntil {
		t.Errorf("expected TimeUntil %v, got %v", expectedTimeUntil, result.TimeUntil)
	}
}

func TestFindNext_GlobalAckedShouldSuppressStarted(t *testing.T) {
	// Scenario: User acked the global reminder before the meeting,
	// then the meeting starts. They should NOT get a "started" alert.
	now := time.Date(2026, 2, 4, 9, 5, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Event starts at 9:00, so it started 5 minutes ago
	start := now.Add(-5 * time.Minute)
	end := now.Add(25 * time.Minute)
	event := makeEvent("evt1", "Already Acked Meeting", start, end)
	events := []calendar.Event{event}

	// User already acknowledged the global reminder before the meeting started
	ackStore.setAcked(AckEventKey("evt1", start), "global")

	result := finder.FindNext(events)
	if result != nil {
		t.Errorf("expected nil (global ack should suppress started), got reminder %s", result.ReminderID)
	}
}

func TestFindNext_CustomReminderAckedShouldNotSuppressStarted(t *testing.T) {
	// Scenario: User acked a custom 10m reminder (early warning), then the meeting starts.
	// They SHOULD still get a "started" alert because custom reminders are just early warnings.
	now := time.Date(2026, 2, 4, 9, 5, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	start := now.Add(-5 * time.Minute)
	end := now.Add(25 * time.Minute)
	event := makeEventWithReminders("evt1", "Custom Acked Meeting", start, end, 10)
	events := []calendar.Event{event}

	// User acknowledged the 10m custom reminder (early warning)
	ackStore.setAcked(AckEventKey("evt1", start), "10m")

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected started reminder (custom ack should not suppress it), got nil")
	}
	if result.ReminderID != "started" {
		t.Errorf("expected started reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_StaleAckFromDifferentDateDoesNotSilence(t *testing.T) {
	// Scenario: Same event ID was acked on a previous date (recurring/rescheduled event).
	// The current instance at a different time should NOT be suppressed.
	now := time.Date(2026, 4, 15, 13, 12, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	// Current instance: April 15 at 13:15
	currentStart := time.Date(2026, 4, 15, 13, 15, 0, 0, time.UTC)
	currentEnd := time.Date(2026, 4, 15, 14, 0, 0, 0, time.UTC)
	event := makeEvent("recurring-evt", "Weekly AI Meeting", currentStart, currentEnd)
	events := []calendar.Event{event}

	// Stale ack from April 13 instance (same event ID, different start time)
	previousStart := time.Date(2026, 4, 13, 13, 15, 0, 0, time.UTC)
	ackStore.setAcked(AckEventKey("recurring-evt", previousStart), "global")

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected reminder (stale ack from different date should not suppress), got nil")
	}
	if result.ReminderID != "global" {
		t.Errorf("expected global reminder, got %s", result.ReminderID)
	}
}

func TestFindNext_AckEventKeyIncludesStartTime(t *testing.T) {
	// Verify the AckEventKey format
	startTime := time.Date(2026, 4, 15, 11, 15, 0, 0, time.UTC)
	key := AckEventKey("abc123", startTime)
	expected := "abc123_20260415T111500Z"
	if key != expected {
		t.Errorf("expected ack key %q, got %q", expected, key)
	}

	// Different start time produces different key
	otherStart := time.Date(2026, 4, 16, 11, 15, 0, 0, time.UTC)
	otherKey := AckEventKey("abc123", otherStart)
	if key == otherKey {
		t.Error("expected different ack keys for different start times")
	}
}

func TestFindNext_EventAckSuppressesGlobal(t *testing.T) {
	// Acking the whole event from the dashboard must suppress the global
	// warn-before alert entirely.
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	start := now.Add(3 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEvent("evt1", "Event-Acked Meeting", start, end)
	events := []calendar.Event{event}

	ackStore.setAcked(AckEventKey("evt1", start), EventAckID)

	if result := finder.FindNext(events); result != nil {
		t.Errorf("expected nil for event-acked meeting, got reminder %s", result.ReminderID)
	}
}

func TestFindNext_EventAckSuppressesStarted(t *testing.T) {
	// Event-level ack must also suppress the "started" alert, unlike custom
	// (10m, 30m, …) acks which only suppress that one specific reminder.
	now := time.Date(2026, 2, 4, 9, 5, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	start := now.Add(-5 * time.Minute)
	end := now.Add(25 * time.Minute)
	event := makeEvent("evt1", "Event-Acked Started", start, end)
	events := []calendar.Event{event}

	ackStore.setAcked(AckEventKey("evt1", start), EventAckID)

	if result := finder.FindNext(events); result != nil {
		t.Errorf("expected nil (event ack suppresses started), got %s", result.ReminderID)
	}
}

func TestFindNext_EventAckSuppressesCustomReminders(t *testing.T) {
	// Custom reminders (10m popup) must also be suppressed when the user
	// ack'd the whole event.
	now := time.Date(2026, 2, 4, 9, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 1 * time.Minute})

	start := now.Add(8 * time.Minute)
	end := now.Add(1 * time.Hour)
	event := makeEventWithReminders("evt1", "Custom Reminder Event-Acked", start, end, 10)
	events := []calendar.Event{event}

	ackStore.setAcked(AckEventKey("evt1", start), EventAckID)

	if result := finder.FindNext(events); result != nil {
		t.Errorf("expected nil (event ack suppresses custom), got %s", result.ReminderID)
	}
}

func TestFindNext_NoAckShouldStillShowStarted(t *testing.T) {
	// Scenario: User never acked any reminder, meeting has started.
	// They SHOULD get a "started" alert.
	now := time.Date(2026, 2, 4, 9, 5, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	ackStore := newMockAckStore()
	finder := NewFinder(ackStore, clock, Config{WarnBefore: 5 * time.Minute})

	start := now.Add(-5 * time.Minute)
	end := now.Add(25 * time.Minute)
	event := makeEvent("evt1", "Missed Meeting", start, end)
	events := []calendar.Event{event}

	// No acks - user missed all reminders

	result := finder.FindNext(events)
	if result == nil {
		t.Fatal("expected started reminder for missed meeting, got nil")
	}
	if result.ReminderID != "started" {
		t.Errorf("expected started reminder, got %s", result.ReminderID)
	}
}
