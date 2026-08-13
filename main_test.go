package main

import (
	"errors"
	"testing"
	"time"

	"github.com/gigurra/oh-shit-meeting/internal/calendar"
)

func TestReAuthAndRequestPollRequestsPollAfterSuccess(t *testing.T) {
	pollNow := make(chan struct{}, 1)

	err := reAuthAndRequestPoll(func() error { return nil }, pollNow)
	if err != nil {
		t.Fatalf("reAuthAndRequestPoll() error = %v", err)
	}

	select {
	case <-pollNow:
	default:
		t.Fatal("expected successful re-auth to request an event poll")
	}
}

func TestReAuthAndRequestPollDoesNotRequestPollAfterFailure(t *testing.T) {
	pollNow := make(chan struct{}, 1)
	wantErr := errors.New("re-auth failed")

	err := reAuthAndRequestPoll(func() error { return wantErr }, pollNow)
	if !errors.Is(err, wantErr) {
		t.Fatalf("reAuthAndRequestPoll() error = %v, want %v", err, wantErr)
	}

	select {
	case <-pollNow:
		t.Fatal("failed re-auth should not request an event poll")
	default:
	}
}

func TestPollEventsPollsAgainWhenRequested(t *testing.T) {
	pollNow := make(chan struct{}, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	polls := make(chan int, 2)
	store := &eventStore{}
	pollCount := 0

	go func() {
		defer close(done)
		pollEvents(time.Hour, pollNow, stop, store, func() []calendar.Event {
			pollCount++
			polls <- pollCount
			if pollCount == 1 {
				return []calendar.Event{{ID: "1"}}
			}
			return []calendar.Event{{ID: "2"}}
		})
	}()

	awaitPoll(t, polls, 1)
	pollNow <- struct{}{}
	awaitPoll(t, polls, 2)
	awaitEventStoreID(t, store, "2")

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll loop did not stop")
	}
}

func TestPollEventsRequestedPollPreservesScheduledPoll(t *testing.T) {
	ticks := make(chan time.Time, 1)
	pollNow := make(chan struct{}, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	polls := make(chan int, 3)
	store := &eventStore{}
	pollCount := 0

	go func() {
		defer close(done)
		pollEventsOnTicks(ticks, pollNow, stop, store, func() []calendar.Event {
			pollCount++
			polls <- pollCount
			return nil
		})
	}()

	awaitPoll(t, polls, 1)
	pollNow <- struct{}{}
	awaitPoll(t, polls, 2)
	ticks <- time.Now()
	awaitPoll(t, polls, 3)

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll loop did not stop")
	}
}

func awaitPoll(t *testing.T, polls <-chan int, want int) {
	t.Helper()
	select {
	case got := <-polls:
		if got != want {
			t.Fatalf("poll number = %d, want %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for poll %d", want)
	}
}

func awaitEventStoreID(t *testing.T, store *eventStore, wantID string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		events := store.get()
		if len(events) == 1 && events[0].ID == wantID {
			return
		}

		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("event store = %#v, want event ID %q", events, wantID)
		}
	}
}
