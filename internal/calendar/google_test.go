package calendar

import (
	"slices"
	"testing"

	gcal "google.golang.org/api/calendar/v3"
)

func TestParseCalendarSpec(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want []string
	}{
		{"empty", "", nil},
		{"single", "primary", []string{"primary"}},
		{"several with padding", " primary , team@example.com ", []string{"primary", "team@example.com"}},
		{"empty entries dropped", "primary,,", []string{"primary"}},
		{"all wins over named calendars", "primary,all", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseCalendarSpec(tt.spec); !slices.Equal(got, tt.want) {
				t.Errorf("parseCalendarSpec(%q) = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestOauthScopes(t *testing.T) {
	restore := selectedCalendars
	t.Cleanup(func() { selectedCalendars = restore })

	selectedCalendars = []string{"primary"}
	if got := oauthScopes(); !slices.Equal(got, []string{gcal.CalendarEventsReadonlyScope}) {
		t.Errorf("with a selection, scopes = %v", got)
	}

	selectedCalendars = nil
	want := []string{gcal.CalendarEventsReadonlyScope, gcal.CalendarCalendarlistReadonlyScope}
	if got := oauthScopes(); !slices.Equal(got, want) {
		t.Errorf("without a selection, scopes = %v, want %v", got, want)
	}
}

func TestCalendarLabel(t *testing.T) {
	if got := calendarLabel("primary"); got != "" {
		t.Errorf("calendarLabel(\"primary\") = %q, want empty", got)
	}
	if got := calendarLabel("team@example.com"); got != "team@example.com" {
		t.Errorf("calendarLabel = %q", got)
	}
}
