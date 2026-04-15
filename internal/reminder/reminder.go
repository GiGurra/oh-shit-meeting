package reminder

import (
	"fmt"
	"time"

	"github.com/gigurra/oh-shit-meeting/internal/calendar"
)

// AckStore defines the interface for acknowledgment storage
type AckStore interface {
	IsAcked(eventID, reminderID string) bool
	MarkAcked(eventID, reminderID string) error
}

// Clock provides the current time (mockable for tests)
type Clock interface {
	Now() time.Time
}

// RealClock uses the system time
type RealClock struct{}

func (c *RealClock) Now() time.Time {
	return time.Now()
}

// Info contains information about a reminder that should fire
type Info struct {
	Event       calendar.Event
	StartTime   time.Time
	EndTime     time.Time
	TimeUntil   time.Duration
	ReminderID  string
	Sound       string
	AckEventKey string // composite key: eventID + start time (handles rescheduled/recurring events)
}

// Config holds configuration for the reminder finder
type Config struct {
	WarnBefore time.Duration
	Sound      string
}

// Finder finds reminders that should fire
type Finder struct {
	ackStore AckStore
	clock    Clock
	config   Config
}

// NewFinder creates a new Finder with the given dependencies
func NewFinder(ackStore AckStore, clock Clock, config Config) *Finder {
	return &Finder{
		ackStore: ackStore,
		clock:    clock,
		config:   config,
	}
}

// AckEventKey computes a unique ack key from event ID and start time.
// This ensures rescheduled or recurring events (same ID, different time)
// get fresh ack state instead of being silenced by stale ack files.
func AckEventKey(eventID string, startTime time.Time) string {
	return eventID + "_" + startTime.UTC().Format("20060102T150405Z")
}

// hasGlobalAck checks if the global or started reminder was acknowledged.
// Used to suppress "started" alerts when user already saw the main reminder.
// Custom reminders (10m, 30m, etc.) are early warnings and don't count.
func (f *Finder) hasGlobalAck(ackKey string) bool {
	return f.ackStore.IsAcked(ackKey, "global") || f.ackStore.IsAcked(ackKey, "started")
}

// FindNext returns the next reminder that should fire, or nil if none.
// Events must be pre-sorted by start time.
func (f *Finder) FindNext(events []calendar.Event) *Info {
	now := f.clock.Now()

	for _, event := range events {
		startTime, err := time.Parse(time.RFC3339, event.Start.DateTime)
		if err != nil {
			continue
		}
		endTime, err := time.Parse(time.RFC3339, event.End.DateTime)
		if err != nil {
			continue
		}

		timeUntil := startTime.Sub(now)
		ackKey := AckEventKey(event.ID, startTime)

		// Skip events that have already ended
		if now.After(endTime) {
			continue
		}

		// Check if event has already started (you're late!)
		if timeUntil < 0 {
			// Don't show "started" if user already acked the global reminder
			if f.hasGlobalAck(ackKey) {
				continue
			}
			return &Info{
				Event:       event,
				StartTime:   startTime,
				EndTime:     endTime,
				TimeUntil:   timeUntil,
				ReminderID:  "started",
				Sound:       f.config.Sound,
				AckEventKey: ackKey,
			}
		}

		// Check custom reminder overrides
		for _, reminder := range event.Reminders.Overrides {
			if reminder.Method != "popup" {
				continue
			}

			reminderID := fmt.Sprintf("%dm", reminder.Minutes)
			reminderTime := time.Duration(reminder.Minutes) * time.Minute
			if timeUntil <= reminderTime && !f.ackStore.IsAcked(ackKey, reminderID) {
				return &Info{
					Event:       event,
					StartTime:   startTime,
					EndTime:     endTime,
					TimeUntil:   timeUntil,
					ReminderID:  reminderID,
					Sound:       f.config.Sound,
					AckEventKey: ackKey,
				}
			}
		}

		// Check global warn-before threshold
		reminderID := "global"
		if timeUntil <= f.config.WarnBefore && !f.ackStore.IsAcked(ackKey, reminderID) {
			return &Info{
				Event:       event,
				StartTime:   startTime,
				EndTime:     endTime,
				TimeUntil:   timeUntil,
				ReminderID:  reminderID,
				Sound:       f.config.Sound,
				AckEventKey: ackKey,
			}
		}
	}

	return nil
}
