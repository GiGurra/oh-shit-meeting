package calendar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

func TestGoogleFetchReportsEveryCalendarAndPage(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial"}[fail], func(t *testing.T) {
			eventPages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/users/me/calendarList":
					if r.URL.Query().Get("pageToken") == "more-calendars" {
						w.Write([]byte(`{"items":[{"id":"team","summary":"Team"},{"id":"empty","summary":"Empty"}]}`))
					} else {
						w.Write([]byte(`{"items":[{"id":"work","summary":"Work"}],"nextPageToken":"more-calendars"}`))
					}
				case "/calendars/work/events":
					eventPages++
					if r.URL.Query().Get("timeMin") != "2026-09-07T00:00:00Z" || r.URL.Query().Get("timeMax") != "2026-09-10T00:00:00Z" {
						t.Error("pagination changed the configured fetch range")
					}
					if r.URL.Query().Get("pageToken") == "more-events" {
						w.Write([]byte(`{"items":[{"id":"second","start":{"dateTime":"2026-09-07T11:00:00Z"}}]}`))
					} else {
						w.Write([]byte(`{"items":[{"id":"first","start":{"dateTime":"2026-09-07T10:00:00Z"}}],"nextPageToken":"more-events"}`))
					}
				case "/calendars/team/events":
					if fail {
						w.WriteHeader(503)
						w.Write([]byte(`{"error":{"code":503,"message":"unavailable"}}`))
					} else {
						w.Write([]byte(`{"items":[]}`))
					}
				case "/calendars/empty/events":
					w.Write([]byte(`{"items":[]}`))
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			svc, err := gcal.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			var result PollResult
			events, err := fetchGoogleCalendars(svc, "2026-09-07T00:00:00Z", "2026-09-10T00:00:00Z", nil, &result)
			if (err != nil) != fail {
				t.Fatalf("error = %v, fail = %v", err, fail)
			}
			if len(events) != 2 || eventPages != 2 || len(result.Calendars) != 3 {
				t.Fatalf("events=%d, pages=%d, report=%+v", len(events), eventPages, result)
			}
			if (result.Calendars[1].Error != "") != fail || result.Calendars[2].Received != 0 || result.Calendars[2].Error != "" {
				t.Fatalf("calendar reports = %+v", result.Calendars)
			}
		})
	}
}

func TestPollResultDistinguishesEmptyFailureAndFilteredResults(t *testing.T) {
	for _, mode := range []string{"empty", "failed", "filtered"} {
		t.Run(mode, func(t *testing.T) {
			result := pollWithFetcher("google", 3, func(from, to, backend string, r *PollResult) ([]Event, string, error) {
				if from == "" || to == "" || backend != "google" {
					t.Fatal("missing request context")
				}
				switch mode {
				case "failed":
					return nil, "gcal-native", errors.New("offline")
				case "filtered":
					return []Event{
						{Start: EventTime{DateTime: "2026-09-07T12:00:00Z"}},
						{Start: EventTime{Date: "2026-09-07"}},
						{Start: EventTime{DateTime: "invalid"}},
						{Start: EventTime{DateTime: "2026-09-07T11:00:00Z"}, EventType: "workingLocation"},
					}, "gcal-native", nil
				}
				return nil, "gcal-native", nil
			})
			if result.StartedAt.IsZero() || result.CompletedAt.Before(result.StartedAt) || time.Since(result.CompletedAt) > time.Second {
				t.Fatalf("timestamps = %+v", result)
			}
			if (result.Error != "") != (mode == "failed") {
				t.Fatalf("error = %q", result.Error)
			}
			if mode == "filtered" && (result.Received != 4 || result.Included != 1 || len(result.Events) != 1) {
				t.Fatalf("counts = %+v", result)
			}
		})
	}
}

func TestPollRespectsLookaheadLimit(t *testing.T) {
	for _, days := range []int{0, 1, 7} {
		result := pollWithFetcher("google", days, func(from, to, backend string, result *PollResult) ([]Event, string, error) {
			limit, err := time.Parse(time.RFC3339, to)
			if err != nil {
				t.Fatal(err)
			}
			wantDays := days
			if wantDays == 0 {
				wantDays = 3
			}
			want := result.StartedAt.Truncate(time.Second).Add(time.Duration(wantDays) * 24 * time.Hour)
			if !limit.Equal(want) {
				t.Fatalf("lookahead %d: requested upper bound %v, want %v", days, limit, want)
			}
			return nil, "gcal-native", nil
		})
		if result.Error != "" {
			t.Fatal(result.Error)
		}
	}
}
