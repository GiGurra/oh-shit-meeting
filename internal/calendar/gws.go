package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
)

type gwsResponse struct {
	Items []Event `json:"items"`
}

type gwsCalendarListResponse struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
}

func gwsListCalendars() ([]string, error) {
	cmd := exec.Command("gws", "calendar", "calendarList", "list", "--params", "{}")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("gws calendarList failed: %w, stderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run gws calendarList: %w", err)
	}

	var response gwsCalendarListResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse calendar list response: %w", err)
	}

	ids := make([]string, len(response.Items))
	for i, item := range response.Items {
		ids[i] = item.ID
	}
	return ids, nil
}

func gwsFetchEventsForCalendar(calendarID, from, to string) ([]Event, error) {
	params, err := json.Marshal(map[string]interface{}{
		"calendarId":   calendarID,
		"timeMin":      from,
		"timeMax":      to,
		"singleEvents": true,
		"orderBy":      "startTime",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	cmd := exec.Command("gws", "calendar", "events", "list", "--params", string(params))
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("gws events list failed for %s: %w, stderr: %s", calendarID, err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run gws events list for %s: %w", calendarID, err)
	}

	var response gwsResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse events response for %s: %w", calendarID, err)
	}

	return response.Items, nil
}

func fetchEventsGWS(from, to string) ([]Event, error) {
	calendarIDs, err := gwsListCalendars()
	if err != nil {
		return nil, err
	}

	var allEvents []Event
	for _, id := range calendarIDs {
		events, err := gwsFetchEventsForCalendar(id, from, to)
		if err != nil {
			slog.Warn("Failed to fetch events for calendar, skipping", "calendarID", id, "error", err)
			continue
		}
		allEvents = append(allEvents, events...)
	}

	return allEvents, nil
}
