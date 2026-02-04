package gui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/gigurra/oh-shit-meeting/internal/format"
	"github.com/gigurra/oh-shit-meeting/internal/sound"
)

var (
	fyneApp   fyne.App
	deskApp   desktop.App
	greenIcon fyne.Resource
	redIcon   fyne.Resource
)

// Init initializes the Fyne app with a systray icon (no hidden window needed)
func Init() {
	fyneApp = app.NewWithID("com.gigurra.oh-shit-meeting")

	greenIcon = createCircleIcon(color.RGBA{R: 30, G: 180, B: 30, A: 255})
	redIcon = createCircleIcon(color.RGBA{R: 200, G: 30, B: 30, A: 255})

	fyneApp.SetIcon(greenIcon)

	if desk, ok := fyneApp.(desktop.App); ok {
		deskApp = desk

		titleItem := fyne.NewMenuItem("oh-shit-meeting", nil)
		titleItem.Disabled = true

		menu := fyne.NewMenu("",
			titleItem,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				fyneApp.Quit()
			}),
		)
		desk.SetSystemTrayMenu(menu)
		desk.SetSystemTrayIcon(greenIcon)
	}
}

// Run starts the Fyne event loop (blocks)
func Run() {
	fyneApp.Run()
}


// createCircleIcon creates a circle icon with the given color
func createCircleIcon(c color.RGBA) fyne.Resource {
	size := 22
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	center := float64(size) / 2
	radius := float64(size)/2 - 1

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center + 0.5
			dy := float64(y) - center + 0.5
			if dx*dx+dy*dy <= radius*radius {
				img.Set(x, y, c)
			}
		}
	}

	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		panic(fmt.Sprintf("failed to encode icon: %v", err))
	}

	return &fyne.StaticResource{
		StaticName:    "tray-icon.png",
		StaticContent: buf.Bytes(),
	}
}

// ReminderInfo contains info needed to display a reminder popup
type ReminderInfo struct {
	Summary        string
	StartTime      time.Time
	TimeUntil      time.Duration
	ReminderID     string
	Sound          string
	Location       string
	OrganizerName  string
	OrganizerEmail string
	Fullscreen     bool
}

// ShowPopupBlocking displays a popup and blocks until it's acknowledged
func ShowPopupBlocking(info ReminderInfo) {
	done := make(chan struct{})

	fyne.Do(func() {
		sound.StartLoop(info.Sound)

		w := fyneApp.NewWindow("MEETING REMINDER")

		// Red background
		bg := canvas.NewRectangle(color.RGBA{R: 200, G: 30, B: 30, A: 255})

		// Event title
		title := canvas.NewText(info.Summary, color.White)
		title.TextSize = 48
		title.TextStyle = fyne.TextStyle{Bold: true}
		title.Alignment = fyne.TextAlignCenter

		// Time info
		timeText := canvas.NewText(
			formatTimeText(info.TimeUntil, info.StartTime),
			color.White,
		)
		timeText.TextSize = 32
		timeText.Alignment = fyne.TextAlignCenter

		// Calendar/organizer info
		calendarName := info.OrganizerName
		if calendarName == "" {
			calendarName = info.OrganizerEmail
		}
		calendarText := canvas.NewText(fmt.Sprintf("Calendar: %s", calendarName), color.White)
		calendarText.TextSize = 24
		calendarText.Alignment = fyne.TextAlignCenter

		// Location if present
		var locationText *canvas.Text
		if info.Location != "" {
			locationText = canvas.NewText(fmt.Sprintf("Location: %s", info.Location), color.White)
			locationText.TextSize = 20
			locationText.Alignment = fyne.TextAlignCenter
		}

		// Reminder source
		sourceText := canvas.NewText(fmt.Sprintf("Reminder: %s", info.ReminderID), color.White)
		sourceText.TextSize = 18
		sourceText.Alignment = fyne.TextAlignCenter

		// ACK button - closes popup and unblocks
		ackBtn := widget.NewButton("ACKNOWLEDGE", func() {
			sound.StopLoop()
			w.Close()
			close(done)
		})

		// Build content
		var content *fyne.Container
		if locationText != nil {
			content = container.NewVBox(
				title,
				widget.NewSeparator(),
				timeText,
				calendarText,
				locationText,
				sourceText,
				widget.NewSeparator(),
				ackBtn,
			)
		} else {
			content = container.NewVBox(
				title,
				widget.NewSeparator(),
				timeText,
				calendarText,
				sourceText,
				widget.NewSeparator(),
				ackBtn,
			)
		}

		// Center the content
		centered := container.NewCenter(content)

		// Stack background and content
		stack := container.NewStack(bg, centered)

		w.SetContent(stack)
		if info.Fullscreen {
			w.SetFullScreen(true)
		} else {
			w.Resize(fyne.NewSize(800, 400))
			w.CenterOnScreen()
		}
		w.Show()

		// Start flashing effects (background and tray icon)
		go flashAlertEffects(bg, done)

		// Start countdown timer update
		go updateCountdown(timeText, info.StartTime, done)
	})

	// Block until popup is acknowledged
	<-done

	// Delay to let UI settle before potentially showing another popup.
	// Fullscreen needs longer because macOS animates back to the previous desktop.
	if info.Fullscreen {
		time.Sleep(3 * time.Second)
	} else {
		time.Sleep(100 * time.Millisecond)
	}
}

func updateCountdown(timeText *canvas.Text, startTime time.Time, done <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			remaining := time.Until(startTime)
			fyne.Do(func() {
				timeText.Text = formatTimeText(remaining, startTime)
				timeText.Refresh()
			})
		}
	}
}

func formatTimeText(timeUntil time.Duration, startTime time.Time) string {
	if timeUntil < 0 {
		return fmt.Sprintf("Started %s ago (at %s)", format.Duration(timeUntil), startTime.Local().Format("15:04"))
	}
	return fmt.Sprintf("Starts in %s (at %s)", format.Duration(timeUntil), startTime.Local().Format("15:04"))
}

func flashAlertEffects(bg *canvas.Rectangle, done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	bright := true
	for {
		select {
		case <-done:
			if deskApp != nil {
				deskApp.SetSystemTrayIcon(greenIcon)
			}
			return
		case <-ticker.C:
			// Flash background color
			bgColor := color.RGBA{R: 200, G: 30, B: 30, A: 255}
			if bright {
				bgColor = color.RGBA{R: 150, G: 20, B: 20, A: 255}
			}
			fyne.Do(func() {
				bg.FillColor = bgColor
				bg.Refresh()
			})

			// Flash tray icon (red when background is bright)
			if deskApp != nil {
				if bright {
					deskApp.SetSystemTrayIcon(greenIcon)
				} else {
					deskApp.SetSystemTrayIcon(redIcon)
				}
			}

			bright = !bright
		}
	}
}
