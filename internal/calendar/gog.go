package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

type gogResponse struct {
	Events []Event `json:"events"`
}

func fetchEventsGog(from, to string) ([]Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gog", "calendar", "list",
		"--from="+from,
		"--to="+to,
		"--all",
		"--json",
	)

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("gog command failed: %w, stderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run gog command: %w", err)
	}

	var response gogResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse gog calendar response: %w", err)
	}

	return response.Events, nil
}
