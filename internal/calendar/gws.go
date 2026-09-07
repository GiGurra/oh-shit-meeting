package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gws", "calendar", "calendarList", "list", "--params", "{}")
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gws", "calendar", "events", "list", "--params", string(params))
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

func fetchEventsGWS(from, to string, result *PollResult) ([]Event, error) {
	calendarIDs := selectedCalendars
	if len(calendarIDs) == 0 {
		var err error
		calendarIDs, err = gwsListCalendars()
		if err != nil {
			return nil, err
		}
	}

	return collectCalendars(calendarIDs, func(i int) ([]Event, error) {
		return gwsFetchEventsForCalendar(calendarIDs[i], from, to)
	}, result)
}
