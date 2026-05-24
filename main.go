package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"f1-tui/internal/source"
	"f1-tui/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// 1. Define custom --help output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🏎️  FORMULA 1 TIMING TOWER TUI\n")
		fmt.Fprintf(os.Stderr, "==================================\n\n")
		fmt.Fprintf(os.Stderr, "A beautiful, keyboard-driven terminal dashboard for Formula 1 races.\n")
		fmt.Fprintf(os.Stderr, "Supports both live polling and simulated replay modes using the OpenF1 API.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  ./f1-tui [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fmt.Fprintf(os.Stderr, "  -session int       OpenF1 Session Key (e.g. 11253 for JPN 2026 GP) (default 9159)\n")
		fmt.Fprintf(os.Stderr, "  -speed float       Replay speed multiplier (e.g. 1.0, 10.0, 50.0) (default 15.0)\n")
		fmt.Fprintf(os.Stderr, "  -max-sleep float   Maximum gap delay in seconds in replay mode (default 1.5)\n")
		fmt.Fprintf(os.Stderr, "  -live              Connect to active live timing\n")
		fmt.Fprintf(os.Stderr, "  -token string      OpenF1 API OAuth2 Token for authenticated live sessions\n\n")
		fmt.Fprintf(os.Stderr, "Keybindings:\n")
		fmt.Fprintf(os.Stderr, "  q, ctrl+c          Quit the AltScreen dashboard gracefully\n")
		fmt.Fprintf(os.Stderr, "  up arrow           Scroll the scrolling incidents feed up\n")
		fmt.Fprintf(os.Stderr, "  down arrow         Scroll the scrolling incidents feed down\n\n")
		fmt.Fprintf(os.Stderr, "Curated Session Keys:\n")
		fmt.Fprintf(os.Stderr, "  11253              2026 Japan GP (Suzuka)\n")
		fmt.Fprintf(os.Stderr, "  9574               2024 Belgian GP (Spa)\n")
		fmt.Fprintf(os.Stderr, "  9558               2024 British GP (Silverstone)\n")
		fmt.Fprintf(os.Stderr, "  9636               2024 Brazilian GP (Interlagos)\n")
		fmt.Fprintf(os.Stderr, "  9590               2024 Italian GP (Monza)\n")
		fmt.Fprintf(os.Stderr, "  9523               2024 Monaco GP\n\n")
	}

	sessionKey := flag.Int("session", 9159, "OpenF1 Session Key")
	speedMultiplier := flag.Float64("speed", 15.0, "Replay speed multiplier")
	maxSleepSec := flag.Float64("max-sleep", 1.5, "Maximum gap delay in seconds")
	liveMode := flag.Bool("live", false, "Connect to active live timing")
	token := flag.String("token", "", "OpenF1 API OAuth2 Token")

	flag.Parse()

	// 2. Select and initialize the data source
	var src source.Source

	if *liveMode {
		fmt.Printf("🏎️  Initializing Live Polling Mode for Session %d...\n", *sessionKey)
		src = source.NewLiveSource(*token, *sessionKey)
	} else {
		maxSleep := time.Duration(*maxSleepSec * float64(time.Second))
		fmt.Printf("🎬 Initializing Replay Mode for Session %d (Speed: %.1fx, Max Delay: %v)...\n", *sessionKey, *speedMultiplier, maxSleep)
		src = source.NewReplaySource(*sessionKey, *speedMultiplier, maxSleep)
	}

	// 3. Initialize Bubble Tea Program
	model := tui.NewModel(src)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start active goroutine to pipe raw channel updates to program
	go func() {
		// Wait a small buffer for bubbletea to start
		time.Sleep(300 * time.Millisecond)
	}()

	// 4. Run the program
	if _, err := p.Run(); err != nil {
		fmt.Printf("❌ Critical TUI crash: %v\n", err)
		os.Exit(1)
	}
}
