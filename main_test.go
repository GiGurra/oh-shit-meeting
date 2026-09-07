package main

import (
	"errors"
	"testing"
	"time"

	"github.com/gigurra/oh-shit-meeting/internal/calendar"
)

func TestReAuthAndRequestPollRequestsPollAfterSuccess(t *testing.T) {
	pollNow := make(chan string, 1)

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
	pollNow := make(chan string, 1)
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

func TestRequestPollCoalescesRepeatedRequests(t *testing.T) {
	pollNow := make(chan string, 1)

	requestPoll(pollNow, "manual refresh")
	requestPoll(pollNow, "manual refresh")

	if got := len(pollNow); got != 1 {
		t.Fatalf("queued poll requests = %d, want 1", got)
	}
}

func TestPollEventsPollsAgainWhenRequested(t *testing.T) {
	pollNow := make(chan string, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	polls := make(chan int, 2)
	store := &eventStore{}
	pollCount := 0

	go func() {
		defer close(done)
		pollEvents(time.Hour, pollNow, stop, store, func() calendar.PollResult {
			pollCount++
			polls <- pollCount
			if pollCount == 1 {
				return calendar.PollResult{Events: []calendar.Event{{ID: "1"}}}
			}
			return calendar.PollResult{Events: []calendar.Event{{ID: "2"}}}
		})
	}()

	awaitPoll(t, polls, 1)
	pollNow <- "manual refresh"
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
	pollNow := make(chan string, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	polls := make(chan int, 3)
	store := &eventStore{}
	pollCount := 0

	go func() {
		defer close(done)
		pollEventsOnTicks(ticks, pollNow, stop, store, func() calendar.PollResult {
			pollCount++
			polls <- pollCount
			return calendar.PollResult{}
		})
	}()

	awaitPoll(t, polls, 1)
	pollNow <- "manual refresh"
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

func TestFetchStatusPreservesSnapshotOnFailureAndClearsOnEmptySuccess(t *testing.T) {
	store := &eventStore{}
	completed := time.Now()
	store.poll("startup", func() calendar.PollResult {
		return calendar.PollResult{Events: []calendar.Event{{ID: "saved"}}, CompletedAt: completed}
	})
	for _, partial := range []bool{false, true} {
		store.poll("after re-auth", func() calendar.PollResult {
			status := store.fetchStatus()
			if status.State != "fetching" || status.Reason != "after re-auth" || !status.LastSuccessAt.Equal(completed) {
				t.Fatalf("in-flight status = %+v", status)
			}
			result := calendar.PollResult{Error: "offline", CompletedAt: time.Now()}
			if partial {
				result.Calendars = []calendar.CalendarResult{{Name: "Work"}, {Name: "Team", Error: "offline"}}
			}
			return result
		})
		want := "failed"
		if partial {
			want = "partial"
		}
		status := store.fetchStatus()
		if status.State != want || !status.LastSuccessAt.Equal(completed) || store.get()[0].ID != "saved" {
			t.Fatalf("failure status = %+v", status)
		}
	}
	store.poll("manual refresh", func() calendar.PollResult { return calendar.PollResult{CompletedAt: completed.Add(time.Second)} })
	if len(store.get()) != 0 || store.fetchStatus().State != "success" || !store.fetchStatus().LastSuccessAt.Equal(completed.Add(time.Second)) {
		t.Fatal("empty success must replace previous data and advance last success")
	}
}

func TestReAuthDrivesCompletedFetch(t *testing.T) {
	pollNow := make(chan string, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	store := &eventStore{}
	calls := make(chan int, 2)
	n := 0
	go func() {
		defer close(done)
		pollEvents(time.Hour, pollNow, stop, store, func() calendar.PollResult {
			n++
			calls <- n
			return calendar.PollResult{Events: []calendar.Event{{ID: "fresh"}}, CompletedAt: time.Now()}
		})
	}()
	defer func() { close(stop); <-done }()
	awaitPoll(t, calls, 1)
	if err := reAuthAndRequestPoll(func() error { return nil }, pollNow); err != nil {
		t.Fatal(err)
	}
	awaitPoll(t, calls, 2)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := store.fetchStatus()
		if status.Reason == "after re-auth" && status.State == "success" && !status.LastSuccessAt.IsZero() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("re-auth fetch did not complete: %+v", store.fetchStatus())
}
