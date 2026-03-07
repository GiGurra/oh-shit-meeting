package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/gofrs/flock"
	"github.com/gigurra/oh-shit-meeting/internal/ack"
	"github.com/gigurra/oh-shit-meeting/internal/calendar"
	"github.com/gigurra/oh-shit-meeting/internal/format"
	"github.com/gigurra/oh-shit-meeting/internal/gui"
	"github.com/gigurra/oh-shit-meeting/internal/reminder"
	"github.com/spf13/cobra"
)

type Params struct {
	PollInterval time.Duration `descr:"How often to poll Google Calendar for events" default:"5m"`
	WarnBefore   time.Duration `descr:"Global alert time before meeting" default:"5m"`
	Sound        string        `descr:"Alert sound (none, or system sound name like Glass, Hero, Funk)" default:"Hero"`
	Fullscreen   bool          `descr:"Show alerts in fullscreen mode for maximum obnoxiousness" default:"false"`
	Backend      string        `descr:"Calendar backend to use" default:"auto" alts:"auto,gws,gog"`
}

type ListEventsParams struct {
	Backend string `descr:"Calendar backend to use" default:"auto" alts:"auto,gws,gog"`
}

func main() {
	boa.CmdT[Params]{
		Use:   "oh-shit-meeting",
		Short: "Calendar reminder daemon",
		Long:  "Monitors your calendar and displays warnings when meetings are about to start",
		RunFunc: func(params *Params, cmd *cobra.Command, args []string) {
			run(params)
		},
		SubCmds: boa.SubCmds(
			boa.CmdT[ListEventsParams]{
				Use:   "list-events",
				Short: "List upcoming calendar events (live integration test)",
				RunFunc: func(params *ListEventsParams, cmd *cobra.Command, args []string) {
					events := calendar.Poll(params.Backend)
					if len(events) == 0 {
						fmt.Println("No upcoming events found.")
						return
					}
					for _, e := range events {
						start, _ := time.Parse(time.RFC3339, e.Start.DateTime)
						fmt.Printf("  %s  %-40s  %s\n",
							start.Local().Format("Mon 02 Jan 15:04"),
							e.Summary,
							e.Location,
						)
					}
				},
			},
		),
	}.Run()
}

func run(params *Params) {
	lockPath := filepath.Join(os.TempDir(), "oh-shit-meeting.lock")
	fileLock := flock.New(lockPath)

	locked, err := fileLock.TryLock()
	if err != nil {
		slog.Error("Failed to acquire lock", "error", err)
		os.Exit(1)
	}
	if !locked {
		slog.Error("Another instance is already running")
		os.Exit(1)
	}
	defer fileLock.Unlock()

	slog.Info("Starting calendar reminder",
		"pollInterval", params.PollInterval,
		"warnBefore", params.WarnBefore,
	)

	gui.Init()
	go runLoop(params)
	gui.Run()
}

func runLoop(params *Params) {
	var events []calendar.Event
	var lastPoll time.Time

	ackStore := &ack.FileStore{}
	clock := &reminder.RealClock{}
	finder := reminder.NewFinder(ackStore, clock, reminder.Config{
		WarnBefore: params.WarnBefore,
		Sound:      params.Sound,
	})

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Poll Google if needed
		if time.Since(lastPoll) >= params.PollInterval {
			events = calendar.Poll(params.Backend)
			lastPoll = time.Now()
		}

		// Check for reminders to fire
		info := finder.FindNext(events)
		if info != nil {
			slog.Warn("MEETING STARTING SOON",
				"event", info.Event.Summary,
				"startsIn", format.Duration(info.TimeUntil),
				"startTime", info.StartTime.Local().Format("15:04"),
				"location", info.Event.Location,
				"source", info.ReminderID,
			)

			gui.ShowPopupBlocking(gui.ReminderInfo{
				Summary:        info.Event.Summary,
				StartTime:      info.StartTime,
				TimeUntil:      info.TimeUntil,
				ReminderID:     info.ReminderID,
				Sound:          info.Sound,
				Location:       info.Event.Location,
				OrganizerName:  info.Event.Organizer.DisplayName,
				OrganizerEmail: info.Event.Organizer.Email,
				Fullscreen:     params.Fullscreen,
			})

			if err := ackStore.MarkAcked(info.Event.ID, info.ReminderID); err != nil {
				slog.Error("Failed to mark reminder as acknowledged", "error", err)
			}
		}
	}
}
