package sound

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/generators"
	"github.com/gopxl/beep/v2/speaker"
)

var (
	stopSound          chan struct{}
	mu                 sync.Mutex
	speakerInitialized bool
)

func StartLoop(sound string) {
	if sound == "" || sound == "none" {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	stopSound = make(chan struct{})

	if runtime.GOOS == "darwin" {
		go startMacOSSound(sound, stopSound)
	} else {
		go startGeneratedTone(stopSound)
	}
}

func StopLoop() {
	mu.Lock()
	defer mu.Unlock()

	if stopSound != nil {
		close(stopSound)
		stopSound = nil
	}
}

func startMacOSSound(sound string, stop chan struct{}) {
	soundPath := fmt.Sprintf("/System/Library/Sounds/%s.aiff", sound)

	for {
		select {
		case <-stop:
			return
		default:
			cmd := exec.Command("afplay", soundPath)
			if err := cmd.Start(); err != nil {
				slog.Error("Failed to play sound", "error", err)
				return
			}

			done := make(chan error)
			go func() {
				done <- cmd.Wait()
			}()

			select {
			case <-stop:
				err := cmd.Process.Kill()
				if err != nil {
					slog.Warn("Failed to stop sound", "error", err)
				}
				return
			case <-done:
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

func startGeneratedTone(stop chan struct{}) {
	sampleRate := beep.SampleRate(44100)

	if !speakerInitialized {
		if err := speaker.Init(sampleRate, sampleRate.N(time.Second/10)); err != nil {
			slog.Error("Failed to initialize speaker", "error", err)
			return
		}
		speakerInitialized = true
	}

	for {
		select {
		case <-stop:
			return
		default:
			for i := 0; i < 3; i++ {
				select {
				case <-stop:
					return
				default:
				}
				playBeep(sampleRate, 880, 150*time.Millisecond)
				time.Sleep(100 * time.Millisecond)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func playBeep(sr beep.SampleRate, freq float64, duration time.Duration) {
	sine, err := generators.SineTone(sr, freq)
	if err != nil {
		slog.Error("Failed to create tone", "error", err)
		return
	}

	samples := sr.N(duration)
	limited := beep.Take(samples, sine)

	faded := &fadeOut{
		Streamer:   limited,
		total:      samples,
		remaining:  samples,
		fadeLength: sr.N(20 * time.Millisecond),
	}

	done := make(chan struct{})
	speaker.Play(beep.Seq(faded, beep.Callback(func() {
		close(done)
	})))
	<-done
}

type fadeOut struct {
	beep.Streamer
	total      int
	remaining  int
	fadeLength int
}

func (f *fadeOut) Stream(samples [][2]float64) (n int, ok bool) {
	n, ok = f.Streamer.Stream(samples)
	for i := range samples[:n] {
		pos := f.total - f.remaining + i
		fadeStart := f.total - f.fadeLength
		if pos >= fadeStart {
			fade := float64(f.total-pos) / float64(f.fadeLength)
			samples[i][0] *= fade
			samples[i][1] *= fade
		}
	}
	f.remaining -= n
	return n, ok
}
