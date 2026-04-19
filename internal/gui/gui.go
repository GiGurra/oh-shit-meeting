package gui

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/gigurra/oh-shit-meeting/internal/calendar"
	"github.com/gigurra/oh-shit-meeting/internal/sound"
)

type Config struct {
	Port     int
	EventsFn func() []calendar.Event
}

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

var (
	mu         sync.Mutex
	active     *ReminderInfo
	activeDone chan struct{}
	cfg        Config
	greenIcon  []byte
	redIcon    []byte
)

// Init prepares icons and starts the local HTTP server.
// Safe to call once before Run.
func Init(c Config) error {
	cfg = c
	greenIcon = makeTrayIcon(color.RGBA{R: 30, G: 180, B: 30, A: 255})
	redIcon = makeTrayIcon(color.RGBA{R: 200, G: 30, B: 30, A: 255})

	mux := http.NewServeMux()
	mux.HandleFunc("/", guardLocal(handleIndex))
	mux.HandleFunc("/state", guardLocal(handleState))
	mux.HandleFunc("/ack", guardLocal(handleAck))

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	slog.Info("dashboard listening", "url", "http://"+addr)
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			slog.Error("http serve failed", "error", err)
		}
	}()
	return nil
}

// Run blocks on the systray main loop. Must be called from the main goroutine.
func Run() {
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetIcon(greenIcon)
	systray.SetTitle("")
	systray.SetTooltip("oh-shit-meeting — " + dashURL())

	mTitle := systray.AddMenuItem("oh-shit-meeting", "")
	mTitle.Disable()
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("Open dashboard", dashURL())
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit oh-shit-meeting")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				if err := openBrowser(dashURL()); err != nil {
					slog.Warn("failed to open browser", "error", err)
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func dashURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port)
}

// ShowPopupBlocking sets the active alert, pops the dashboard to front, and
// blocks until /ack is called for this reminder.
func ShowPopupBlocking(info ReminderInfo) {
	done := make(chan struct{})
	mu.Lock()
	active = &info
	activeDone = done
	mu.Unlock()

	sound.StartLoop(info.Sound)
	go flashTray(done)
	if err := openBrowser(dashURL()); err != nil {
		slog.Warn("failed to open browser", "error", err)
	}

	<-done
	sound.StopLoop()

	mu.Lock()
	active = nil
	activeDone = nil
	mu.Unlock()

	// Let UI settle before a potential next alert
	time.Sleep(100 * time.Millisecond)
}

func flashTray(done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	red := false
	for {
		select {
		case <-done:
			systray.SetIcon(greenIcon)
			return
		case <-ticker.C:
			if red {
				systray.SetIcon(greenIcon)
			} else {
				systray.SetIcon(redIcon)
			}
			red = !red
		}
	}
}

// ---------------- HTTP handlers ----------------

type alertDTO struct {
	Summary        string    `json:"summary"`
	StartTime      time.Time `json:"startTime"`
	ReminderID     string    `json:"reminderId"`
	Location       string    `json:"location,omitempty"`
	OrganizerName  string    `json:"organizerName,omitempty"`
	OrganizerEmail string    `json:"organizerEmail,omitempty"`
	Fullscreen     bool      `json:"fullscreen"`
}

type eventDTO struct {
	ID          string             `json:"id"`
	Summary     string             `json:"summary"`
	StartTime   time.Time          `json:"startTime"`
	EndTime     time.Time          `json:"endTime,omitempty"`
	Location    string             `json:"location,omitempty"`
	Organizer   string             `json:"organizer,omitempty"`
	Calendar    string             `json:"calendar,omitempty"`
	Description string             `json:"description,omitempty"`
	HangoutLink string             `json:"hangoutLink,omitempty"`
	HtmlLink    string             `json:"htmlLink,omitempty"`
	Attendees   []attendeeDTO      `json:"attendees,omitempty"`
	Status      string             `json:"status,omitempty"`
}

type attendeeDTO struct {
	Email          string `json:"email,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Self           bool   `json:"self,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
}

type stateDTO struct {
	Alert    *alertDTO  `json:"alert,omitempty"`
	Upcoming []eventDTO `json:"upcoming"`
	Now      time.Time  `json:"now"`
}

// guardLocal rejects requests with a non-loopback Host header (blocking
// DNS-rebinding attacks) and requires a same-origin Origin header for
// state-changing methods (blocking cross-origin POSTs to /ack).
func guardLocal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		switch host {
		case "127.0.0.1", "::1", "localhost":
			// ok
		default:
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		// Non-idempotent methods must come from our own origin (or no origin,
		// which includes direct CLI/curl invocations).
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				if !isOwnOrigin(origin) {
					http.Error(w, "cross-origin forbidden", http.StatusForbidden)
					return
				}
			}
		}
		next(w, r)
	}
}

func isOwnOrigin(origin string) bool {
	origin = strings.TrimSuffix(origin, "/")
	for _, host := range []string{"127.0.0.1", "[::1]", "localhost"} {
		if origin == fmt.Sprintf("http://%s:%d", host, cfg.Port) {
			return true
		}
	}
	return false
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func handleState(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	var al *alertDTO
	if active != nil {
		al = &alertDTO{
			Summary:        active.Summary,
			StartTime:      active.StartTime,
			ReminderID:     active.ReminderID,
			Location:       active.Location,
			OrganizerName:  active.OrganizerName,
			OrganizerEmail: active.OrganizerEmail,
			Fullscreen:     active.Fullscreen,
		}
	}
	mu.Unlock()

	resp := stateDTO{
		Alert:    al,
		Upcoming: upcomingEvents(),
		Now:      time.Now(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if active == nil || activeDone == nil {
		http.Error(w, "no active alert", http.StatusConflict)
		return
	}
	if id != active.ReminderID {
		http.Error(w, "alert id mismatch", http.StatusConflict)
		return
	}
	close(activeDone)
	activeDone = nil
	w.WriteHeader(http.StatusNoContent)
}

func upcomingEvents() []eventDTO {
	if cfg.EventsFn == nil {
		return nil
	}
	events := cfg.EventsFn()
	out := make([]eventDTO, 0, len(events))
	cutoff := time.Now().Add(-15 * time.Minute)
	for _, e := range events {
		if e.Start.DateTime == "" {
			continue
		}
		st, err := time.Parse(time.RFC3339, e.Start.DateTime)
		if err != nil {
			continue
		}
		if st.Before(cutoff) {
			continue
		}
		org := e.Organizer.DisplayName
		if org == "" {
			org = e.Organizer.Email
		}
		var end time.Time
		if e.End.DateTime != "" {
			end, _ = time.Parse(time.RFC3339, e.End.DateTime)
		}
		attendees := make([]attendeeDTO, 0, len(e.Attendees))
		for _, a := range e.Attendees {
			attendees = append(attendees, attendeeDTO{
				Email:          a.Email,
				DisplayName:    a.DisplayName,
				ResponseStatus: a.ResponseStatus,
				Self:           a.Self,
				Organizer:      a.Organizer,
			})
		}
		out = append(out, eventDTO{
			ID:          e.ID,
			Summary:     e.Summary,
			StartTime:   st,
			EndTime:     end,
			Location:    e.Location,
			Organizer:   org,
			Calendar:    e.Calendar,
			Description: e.Description,
			HangoutLink: e.HangoutLink,
			HtmlLink:    e.HtmlLink,
			Attendees:   attendees,
			Status:      e.Status,
		})
	}
	return out
}

// ---------------- helpers ----------------

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// makeTrayIcon returns platform-appropriate icon bytes: ICO on Windows
// (which Shell_NotifyIcon requires), PNG elsewhere.
func makeTrayIcon(c color.RGBA) []byte {
	pngBytes := makeIconPNG(c)
	if runtime.GOOS == "windows" {
		return pngToICO(pngBytes, 22)
	}
	return pngBytes
}

func makeIconPNG(c color.RGBA) []byte {
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
	if err := png.Encode(&buf, img); err != nil {
		panic(fmt.Sprintf("failed to encode icon: %v", err))
	}
	return buf.Bytes()
}

// pngToICO wraps a PNG in a single-image ICO container. Windows Vista+
// accepts PNG-inside-ICO for icons ≥ 48×48 and works fine for smaller too.
// size is the pixel dimension of the PNG (use 0 to mean 256).
func pngToICO(pngBytes []byte, size int) []byte {
	var b uint8
	if size >= 256 {
		b = 0
	} else {
		b = uint8(size)
	}
	var buf bytes.Buffer
	// ICONDIR
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // type = 1 (ICO)
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // image count
	// ICONDIRENTRY
	buf.WriteByte(b)                                            // width
	buf.WriteByte(b)                                            // height
	buf.WriteByte(0)                                            // color palette
	buf.WriteByte(0)                                            // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1))          // color planes
	binary.Write(&buf, binary.LittleEndian, uint16(32))         // bits per pixel
	binary.Write(&buf, binary.LittleEndian, uint32(len(pngBytes))) // image size
	binary.Write(&buf, binary.LittleEndian, uint32(22))         // image offset
	buf.Write(pngBytes)
	return buf.Bytes()
}

// ---------------- embedded HTML ----------------

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>oh-shit-meeting</title>
<style>
  :root { color-scheme: light dark; }
  html, body { margin: 0; padding: 0; font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif; }
  body { min-height: 100vh; }
  .dashboard { padding: 2rem; max-width: 900px; margin: 0 auto; }
  .dashboard h1 { margin-top: 0; }
  .status { color: #1a7f1a; font-weight: 600; }
  .events { list-style: none; padding: 0; }
  .event { border: 1px solid #ccc5; border-radius: 0.5rem; margin-bottom: 0.5rem; overflow: hidden; }
  .event > summary { padding: 0.75rem 1rem; cursor: pointer; list-style: none; }
  .event > summary::-webkit-details-marker { display: none; }
  .event > summary::before { content: "▸"; display: inline-block; width: 1em; transition: transform 0.15s; opacity: 0.6; }
  .event[open] > summary::before { transform: rotate(90deg); }
  .event .title { font-weight: 600; font-size: 1.1rem; }
  .event .meta { font-size: 0.9rem; opacity: 0.8; margin-top: 0.25rem; padding-left: 1em; }
  .event .body { padding: 0.25rem 1rem 1rem 2rem; border-top: 1px solid #ccc3; }
  .event .body section { margin-top: 0.75rem; }
  .event .body h3 { font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.05em; opacity: 0.6; margin: 0 0 0.25rem; }
  .event .description { white-space: pre-wrap; word-wrap: break-word; font-size: 0.95rem; }
  .event .description a { color: #2a6fdb; }
  .meet-btn {
    display: inline-block; padding: 0.5rem 1rem; background: #1a73e8; color: white !important;
    text-decoration: none; border-radius: 0.25rem; font-weight: 600; font-size: 0.95rem;
  }
  .meet-btn:hover { background: #1557b0; }
  .cal-link { font-size: 0.85rem; opacity: 0.8; }
  .attendees { list-style: none; padding: 0; margin: 0; font-size: 0.9rem; }
  .attendees li { padding: 0.15rem 0; }
  .rs-accepted  { color: #1a7f1a; }
  .rs-declined  { color: #c82828; }
  .rs-tentative { color: #b88a1a; }
  .rs-needsAction { opacity: 0.6; }
  .countdown { font-variant-numeric: tabular-nums; }
  .empty { opacity: 0.6; font-style: italic; }
  .meet-badge { color: #1a73e8; font-weight: 600; }

  .panic {
    position: fixed; inset: 0;
    display: flex; align-items: center; justify-content: center;
    background: #c81e1e;
    color: white;
    animation: flash 1s infinite;
    text-align: center;
    padding: 2rem;
    box-sizing: border-box;
  }
  .panic .inner { max-width: 90vw; }
  .panic h1 { font-size: clamp(2rem, 8vw, 5rem); margin: 0 0 1rem; }
  .panic .when { font-size: clamp(1.25rem, 4vw, 2.5rem); margin-bottom: 1rem; }
  .panic .sub { font-size: clamp(1rem, 2.5vw, 1.5rem); margin: 0.25rem 0; }
  .panic button {
    margin-top: 2rem;
    font-size: clamp(1rem, 3vw, 2rem);
    padding: 0.75rem 2rem;
    font-weight: 700;
    background: white; color: #c81e1e;
    border: none; border-radius: 0.5rem; cursor: pointer;
  }
  .panic button:hover { background: #eee; }
  @keyframes flash {
    0%, 49% { background: #c81e1e; }
    50%, 100% { background: #961414; }
  }
</style>
</head>
<body>
<div id="root"></div>
<script>
const root = document.getElementById("root");
let lastRendered = "";
let wasFullscreen = false;

function fmtDuration(ms) {
  const abs = Math.abs(ms);
  const sign = ms < 0 ? "-" : "";
  const totalSec = Math.floor(abs / 1000);
  const d = Math.floor(totalSec / 86400);
  const h = Math.floor((totalSec % 86400) / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  const pad = n => String(n).padStart(2, "0");
  if (d > 0) return sign + d + "d " + h + "h " + pad(m) + "m";
  if (h > 0) return sign + h + "h " + pad(m) + "m " + pad(s) + "s";
  return sign + m + "m " + pad(s) + "s";
}

function fmtTime(d) {
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function fmtDateTime(d) {
  return d.toLocaleString([], {
    weekday: "short",
    day: "2-digit",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function relPhrase(startMs, nowMs) {
  const diff = startMs - nowMs;
  if (diff >= 0) return "starts in " + fmtDuration(diff);
  return "started " + fmtDuration(-diff) + " ago";
}

function renderEventBody(e) {
  let html = '<div class="body">';
  if (e.hangoutLink) {
    html += '<section><a class="meet-btn" href="' + escapeAttr(e.hangoutLink) + '" target="_blank" rel="noopener">📹 Join Google Meet</a></section>';
  }
  if (e.description) {
    html += '<section><h3>Description</h3><div class="description">' + linkify(e.description) + '</div></section>';
  }
  if (e.location) {
    html += '<section><h3>Location</h3><div>' + linkify(e.location) + '</div></section>';
  }
  if (e.attendees && e.attendees.length) {
    html += '<section><h3>Attendees (' + e.attendees.length + ')</h3><ul class="attendees">';
    for (const a of e.attendees) {
      const name = a.displayName || a.email || "(unknown)";
      const rs = a.responseStatus || "needsAction";
      const marker = a.self ? " (you)" : a.organizer ? " (organizer)" : "";
      html += '<li class="rs-' + escapeAttr(rs) + '">' + escapeHtml(name) + marker;
      if (a.email && a.email !== name) html += ' <span style="opacity:0.6">&lt;' + escapeHtml(a.email) + '&gt;</span>';
      html += ' — ' + escapeHtml(rs);
      html += '</li>';
    }
    html += '</ul></section>';
  }
  if (e.endTime) {
    html += '<section><h3>Ends</h3><div>' + escapeHtml(fmtDateTime(new Date(e.endTime))) + '</div></section>';
  }
  if (e.htmlLink) {
    html += '<section><a class="cal-link" href="' + escapeAttr(e.htmlLink) + '" target="_blank" rel="noopener">Open in Google Calendar ↗</a></section>';
  }
  html += '</div>';
  return html;
}

function eventKey(e) {
  return (e.id || "") + "|" + (e.summary || "") + "|" + (e.startTime || "") + "|" + (e.hangoutLink || "") + "|" + ((e.attendees || []).length) + "|" + (e.description || "").length + "|" + (e.location || "");
}

function renderDashboard(state) {
  const upcoming = state.upcoming || [];
  const now = new Date(state.now).getTime();
  const key = "dash:" + upcoming.map(eventKey).join(";;");
  if (key !== lastRendered) {
    let html = '<div class="dashboard">';
    html += '<h1>oh-shit-meeting <span class="status">running</span></h1>';
    html += '<h2>Upcoming</h2>';
    if (upcoming.length === 0) {
      html += '<p class="empty">No upcoming events.</p>';
    } else {
      html += '<ul class="events" style="list-style:none;padding:0">';
      for (const e of upcoming) {
        const start = new Date(e.startTime);
        html += '<li><details class="event"><summary>';
        html += '<span class="title">' + escapeHtml(e.summary || "(no title)") + '</span>';
        if (e.hangoutLink) html += ' <span class="meet-badge" title="Has Google Meet">📹</span>';
        html += '<div class="meta">';
        html += '<span class="countdown">' + fmtDateTime(start) + ' — ' + relPhrase(start.getTime(), now) + '</span>';
        if (e.calendar)  html += ' · 📅 ' + escapeHtml(e.calendar);
        if (e.organizer) html += ' · ' + escapeHtml(e.organizer);
        if (e.location)  html += ' · ' + escapeHtml(e.location);
        html += '</div>';
        html += '</summary>';
        html += renderEventBody(e);
        html += '</details></li>';
      }
      html += '</ul>';
    }
    html += '</div>';
    root.innerHTML = html;
    lastRendered = key;
  } else {
    // live-update countdowns without collapsing any open accordion
    const spans = root.querySelectorAll(".countdown");
    upcoming.forEach((e, i) => {
      if (!spans[i]) return;
      const start = new Date(e.startTime);
      spans[i].textContent = fmtDateTime(start) + ' — ' + relPhrase(start.getTime(), now);
    });
  }
}

function renderPanic(state) {
  const a = state.alert;
  const now = new Date(state.now).getTime();
  const startMs = new Date(a.startTime).getTime();
  const key = "panic:" + a.reminderId + ":" + a.summary;
  if (key !== lastRendered) {
    const org = a.organizerName || a.organizerEmail || "";
    let html = '<div class="panic"><div class="inner">';
    html += '<h1>' + escapeHtml(a.summary || "Meeting") + '</h1>';
    html += '<div class="when" id="when"></div>';
    if (org)        html += '<div class="sub">Calendar: ' + escapeHtml(org) + '</div>';
    if (a.location) html += '<div class="sub">Location: ' + escapeHtml(a.location) + '</div>';
    html += '<div class="sub">Reminder: ' + escapeHtml(a.reminderId) + '</div>';
    html += '<button id="ackBtn">ACKNOWLEDGE</button>';
    html += '</div></div>';
    root.innerHTML = html;
    lastRendered = key;
    document.getElementById("ackBtn").addEventListener("click", () => ack(a.reminderId));
    if (a.fullscreen && !wasFullscreen) {
      wasFullscreen = true;
      // best-effort — browsers may reject without a user gesture
      document.documentElement.requestFullscreen?.().catch(() => {});
    }
  }
  const whenEl = document.getElementById("when");
  if (whenEl) {
    whenEl.textContent = fmtTime(new Date(a.startTime)) + " — " + relPhrase(startMs, now);
  }
}

async function ack(id) {
  try {
    await fetch("/ack?id=" + encodeURIComponent(id), { method: "POST" });
  } catch (e) { /* ignore */ }
  if (document.fullscreenElement) {
    document.exitFullscreen?.().catch(() => {});
  }
  wasFullscreen = false;
  // Force-refresh so the view flips away from panic mode immediately,
  // without waiting for the next 1-second poll.
  tick();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
  }[c]));
}
function escapeAttr(s) { return escapeHtml(s); }

// linkify escapes html then turns bare URLs into clickable links.
function linkify(s) {
  const escaped = escapeHtml(s);
  return escaped.replace(/(https?:\/\/[^\s<]+)/g, url => {
    return '<a href="' + url + '" target="_blank" rel="noopener">' + url + '</a>';
  });
}

async function tick() {
  try {
    const r = await fetch("/state", { cache: "no-store" });
    const s = await r.json();
    if (s.alert) renderPanic(s);
    else renderDashboard(s);
  } catch (e) { /* ignore */ }
}

tick();
setInterval(tick, 1000);
</script>
</body>
</html>
`
