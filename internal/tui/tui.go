package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"f1-tui/internal/source"
	"f1-tui/internal/state"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type EventMsg source.Event
type ErrorMsg error
type LoadingFinishedMsg struct {
	Session source.Session
	Drivers map[int]source.Driver
}

type appState int

const (
	StateHub appState = iota
	StateLoading
	StateDashboard
)

type Config struct {
	SessionKey int
	Speed      float64
	MaxSleep   float64
	Live       bool
	Token      string
}

type Model struct {
	config       Config
	appState     appState
	hubCursor    int
	inputMode    bool
	focusSpeed   bool
	textInput    textinput.Model
	speedInput   textinput.Model

	source       source.Source
	state        *state.SessionState
	width        int
	height       int
	loading      bool
	loadingMsg   string
	err          error
	eventChan    <-chan source.Event
	errChan      <-chan error
	scrollOffset int
}

var curatedSessions = []struct {
	Name string
	Key  int
}{
	{"Live Timing (Current Session)", 0},
	{"2026 Japan GP (Suzuka)", 11253},
	{"2024 Belgian GP (Spa)", 9574},
	{"2024 British GP (Silverstone)", 9558},
	{"2024 Brazilian GP (Interlagos)", 9636},
	{"2024 Italian GP (Monza)", 9590},
	{"2024 Monaco GP", 9523},
	{"Custom Session Key", -1},
}

func NewModel(cfg Config) *Model {
	ti := textinput.New()
	ti.Placeholder = "Enter session key (e.g. 9159)"
	ti.CharLimit = 10
	ti.Width = 30

	si := textinput.New()
	si.Placeholder = "Enter speed (e.g. 12.5)"
	si.CharLimit = 10
	si.Width = 25

	return &Model{
		config:     cfg,
		appState:   StateHub,
		textInput:  ti,
		speedInput: si,
		state:      state.NewSessionState(),
		loading:    true,
		loadingMsg: "Connecting to OpenF1 API and fetching session details...",
	}
}

func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *Model) startSession() tea.Cmd {
	return func() tea.Msg {
		if m.config.Live {
			m.source = source.NewLiveSource(m.config.Token, m.config.SessionKey)
		} else {
			maxSleep := time.Duration(m.config.MaxSleep * float64(time.Second))
			m.source = source.NewReplaySource(m.config.SessionKey, m.config.Speed, maxSleep)
		}

		session, err := m.source.GetSession()
		if err != nil {
			return ErrorMsg(err)
		}
		drivers, err := m.source.GetDrivers()
		if err != nil {
			return ErrorMsg(err)
		}
		return LoadingFinishedMsg{
			Session: session,
			Drivers: drivers,
		}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.source != nil {
				m.source.Stop()
			}
			return m, tea.Quit
		}

		if m.appState == StateHub {
			if m.inputMode {
				switch msg.String() {
				case "enter":
					key, err := strconv.Atoi(m.textInput.Value())
					if err == nil {
						m.config.SessionKey = key
						m.config.Live = false
						m.appState = StateLoading
						return m, m.startSession()
					}
				case "esc":
					m.inputMode = false
					m.textInput.Blur()
				default:
					m.textInput, cmd = m.textInput.Update(msg)
					return m, cmd
				}
			} else if m.focusSpeed {
				switch msg.String() {
				case "enter":
					val, err := strconv.ParseFloat(m.speedInput.Value(), 64)
					if err == nil && val > 0 {
						m.config.Speed = val
					}
					m.focusSpeed = false
					m.speedInput.Blur()
				case "esc":
					m.focusSpeed = false
					m.speedInput.Blur()
				default:
					m.speedInput, cmd = m.speedInput.Update(msg)
					return m, cmd
				}
			} else {
				switch msg.String() {
				case "q":
					return m, tea.Quit
				case "s":
					m.focusSpeed = true
					m.speedInput.Focus()
					m.speedInput.SetValue(fmt.Sprintf("%v", m.config.Speed))
					return m, textinput.Blink
				case "up", "k":
					if m.hubCursor > 0 {
						m.hubCursor--
					}
				case "down", "j":
					if m.hubCursor < len(curatedSessions)-1 {
						m.hubCursor++
					}
				case "left", "h":
					if m.config.Speed > 10 {
						m.config.Speed -= 5
					} else if m.config.Speed > 1 {
						m.config.Speed -= 1
					} else if m.config.Speed > 0.1 {
						m.config.Speed -= 0.1
					}
				case "right", "l":
					if m.config.Speed >= 10 {
						m.config.Speed += 5
					} else if m.config.Speed >= 1 {
						m.config.Speed += 1
					} else {
						m.config.Speed += 0.1
					}
				case "enter":
					selected := curatedSessions[m.hubCursor]
					if selected.Key == -1 {
						m.inputMode = true
						m.textInput.Focus()
						return m, textinput.Blink
					} else if selected.Key == 0 {
						m.config.Live = true
						m.appState = StateLoading
						return m, m.startSession()
					} else {
						m.config.SessionKey = selected.Key
						m.config.Live = false
						m.appState = StateLoading
						return m, m.startSession()
					}
				}
			}
			return m, nil
		} else if m.appState == StateDashboard {
			switch msg.String() {
			case "q":
				if m.source != nil {
					m.source.Stop()
				}
				return m, tea.Quit
			case "up":
				if m.scrollOffset > 0 {
					m.scrollOffset--
				}
			case "down":
				if m.scrollOffset < len(m.state.RaceMessages) {
					m.scrollOffset++
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case LoadingFinishedMsg:
		m.loading = false
		m.appState = StateDashboard
		m.state.SetSession(msg.Session)
		m.state.SetDrivers(msg.Drivers)

		// Start streaming channel events
		var err error
		m.eventChan, m.errChan, err = m.source.Start()
		if err != nil {
			m.err = err
			return m, nil
		}

		// Bubble Tea command to pipe channel events into program loop
		return m, m.waitForEvent()

	case EventMsg:
		m.state.ApplyEvent(source.Event(msg))
		return m, m.waitForEvent()

	case ErrorMsg:
		m.err = msg
		return m, nil
	}

	return m, nil
}

// Asynchronously waits for next channel event and returns a command
func (m *Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-m.eventChan:
			if !ok {
				return nil
			}
			return EventMsg(ev)
		case err, ok := <-m.errChan:
			if !ok || err == nil {
				return nil
			}
			return ErrorMsg(err)
		}
	}
}

// styling elements
var (
	colorBlack   = lipgloss.Color("16")
	colorWhite   = lipgloss.Color("255")
	colorGreen   = lipgloss.Color("2")
	colorYellow  = lipgloss.Color("3")
	colorRed     = lipgloss.Color("1")
	colorOrange  = lipgloss.Color("208")
	colorGrey    = lipgloss.Color("240")
	colorDarkBg  = lipgloss.Color("233")
	colorGridBorder = lipgloss.Color("236")

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Background(colorGrey).
			Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGridBorder).
			Background(colorDarkBg)

	cellHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("248")).
			Underline(true)
)

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "🏎️  F1 TUI: Initializing layout..."
	}

	if m.err != nil {
		return lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true).
			Padding(2).
			Render(fmt.Sprintf("❌ Error: %v\n\nPress 'q' to quit.", m.err))
	}

	if m.appState == StateHub {
		return m.renderHub()
	}

	if m.appState == StateLoading {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("39")).
				Bold(true).
				Render("🏎️  F1 TUI Leaderboard Loading\n\n"+m.loadingMsg),
		)
	}


	// 1. Build Header
	header := m.renderHeader()

	// 2. Build Content Pane (Leaderboard & Logs)
	contentHeight := m.height - lipgloss.Height(header) - 3
	if contentHeight < 10 {
		contentHeight = 10
	}

	// Split screen horizontally
	leftWidth := int(float64(m.width) * 0.65)
	if leftWidth < 50 {
		leftWidth = 50
	}
	rightWidth := m.width - leftWidth - 2

	leftBox := boxStyle.
		Width(leftWidth).
		Height(contentHeight).
		Render(m.renderTimingTower(leftWidth - 2, contentHeight - 2))

	rightBox := boxStyle.
		Width(rightWidth).
		Height(contentHeight).
		Render(m.renderRaceControl(rightWidth - 2, contentHeight - 2))

	mainLayout := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	// 3. Build Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true)
	footer := footerStyle.Render(" Keys: [q] Quit | [↑/↓] Scroll Logs | Powered by OpenF1 API")

	return lipgloss.JoinVertical(lipgloss.Left, header, mainLayout, footer)
}

func (m *Model) renderHeader() string {
	// Flags & Safety Car styles
	var flagStyle lipgloss.Style
	flagText := m.state.TrackFlag

	switch flagText {
	case "GREEN", "CLEAR":
		flagStyle = lipgloss.NewStyle().Bold(true).Background(colorGreen).Foreground(colorBlack).Padding(0, 1)
		flagText = "🟢 GREEN FLAG"
	case "YELLOW", "DOUBLE YELLOW", "SC", "SAFETY CAR", "VSC":
		flagStyle = lipgloss.NewStyle().Bold(true).Background(colorYellow).Foreground(colorBlack).Padding(0, 1)
		flagText = "🟡 YELLOW FLAG"
	case "RED":
		flagStyle = lipgloss.NewStyle().Bold(true).Background(colorRed).Foreground(colorWhite).Padding(0, 1)
		flagText = "🔴 RED FLAG"
	default:
		flagStyle = lipgloss.NewStyle().Bold(true).Background(colorGreen).Foreground(colorBlack).Padding(0, 1)
		flagText = "🟢 GREEN FLAG"
	}

	weatherText := fmt.Sprintf("☀️ Air: %.1f°C | Track: %.1f°C", m.state.AirTemp, m.state.TrackTemp)
	if m.state.Rain {
		weatherText = fmt.Sprintf("🌧️ RAIN | Air: %.1f°C | Track: %.1f°C", m.state.AirTemp, m.state.TrackTemp)
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("202")). // Orange Red
		Render("🏎️  FORMULA 1 TIMING TOWER")

	location := lipgloss.NewStyle().
		Foreground(lipgloss.Color("251")).
		Render(fmt.Sprintf("%s - %s", m.state.Location, m.state.SessionName))

	flagBlock := flagStyle.Render(flagText)
	weatherBlock := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Render(weatherText)

	headerContent := fmt.Sprintf("%s | %s\n%s | %s", title, location, flagBlock, weatherBlock)

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorGridBorder).
		Padding(0, 1).
		Width(m.width - 2).
		Render(headerContent)
}

func (m *Model) renderTimingTower(width, height int) string {
	// Determine which columns to show based on available width
	showSectors := width >= 90
	showInterval := width >= 70
	showTeam := width >= 60

	// Column settings: name -> width mapping
	headers := []string{"POS", "DRV"}
	colWidths := []int{4, 5}

	if showTeam {
		headers = append(headers, "TEAM")
		colWidths = append(colWidths, 14)
	}

	headers = append(headers, "LAPS", "LAST LAP")
	colWidths = append(colWidths, 5, 9)

	if showSectors {
		headers = append(headers, "S1", "S2", "S3")
		colWidths = append(colWidths, 8, 8, 8)
	}

	headers = append(headers, "GAP")
	colWidths = append(colWidths, 10)

	if showInterval {
		headers = append(headers, "INTERVAL")
		colWidths = append(colWidths, 10)
	}

	// Render Header Row
	var headerRow strings.Builder
	for i, h := range headers {
		headerRow.WriteString(cellHeaderStyle.Width(colWidths[i]).Render(h))
		headerRow.WriteString(" ")
	}

	var rows []string
	rows = append(rows, headerRow.String())

	// Divider
	divider := lipgloss.NewStyle().Foreground(colorGridBorder).Render(strings.Repeat("─", width))
	rows = append(rows, divider)

	// Add driver rows
	for idx, ds := range m.state.Standings {
		if idx >= height-2 {
			break // Viewport clipping
		}

		// Create dynamic driver label using their F1 team color code
		teamColorHex := "#" + ds.TeamColour
		driverStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(teamColorHex))

		posStr := fmt.Sprintf("%2d", ds.Position)
		if ds.Position == 1 {
			posStr = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).Render("🥇")
		}

		drvAcronym := driverStyle.Render(ds.Acronym)

		var row strings.Builder
		row.WriteString(lipgloss.NewStyle().Width(colWidths[0]).Render(posStr))
		row.WriteString(" ")
		row.WriteString(lipgloss.NewStyle().Width(colWidths[1]).Render(drvAcronym))
		row.WriteString(" ")

		colIdx := 2

		if showTeam {
			teamName := ds.TeamName
			w := colWidths[colIdx]
			if len(teamName) > w {
				teamName = teamName[:w-3] + "..."
			}
			teamStr := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(teamName)
			row.WriteString(lipgloss.NewStyle().Width(w).Render(teamStr))
			row.WriteString(" ")
			colIdx++
		}

		lapsStr := fmt.Sprintf("%d", ds.Laps)
		row.WriteString(lipgloss.NewStyle().Width(colWidths[colIdx]).Render(lapsStr))
		row.WriteString(" ")
		colIdx++

		lastLapStr := state.FormatDuration(ds.LastLapTime)
		row.WriteString(lipgloss.NewStyle().Width(colWidths[colIdx]).Render(lastLapStr))
		row.WriteString(" ")
		colIdx++

		if showSectors {
			s1Str := state.FormatDuration(ds.S1)
			row.WriteString(lipgloss.NewStyle().Width(colWidths[colIdx]).Render(s1Str))
			row.WriteString(" ")
			colIdx++

			s2Str := state.FormatDuration(ds.S2)
			row.WriteString(lipgloss.NewStyle().Width(colWidths[colIdx]).Render(s2Str))
			row.WriteString(" ")
			colIdx++

			s3Str := state.FormatDuration(ds.S3)
			row.WriteString(lipgloss.NewStyle().Width(colWidths[colIdx]).Render(s3Str))
			row.WriteString(" ")
			colIdx++
		}

		gapStr := ds.GapToLeader
		row.WriteString(lipgloss.NewStyle().Width(colWidths[colIdx]).Render(gapStr))
		row.WriteString(" ")
		colIdx++

		if showInterval {
			intStr := ds.Interval
			row.WriteString(lipgloss.NewStyle().Width(colWidths[colIdx]).Render(intStr))
			row.WriteString(" ")
			colIdx++
		}

		rows = append(rows, row.String())
	}

	if len(m.state.Standings) == 0 {
		rows = append(rows, "\n  ⚠️ Waiting for session sequence data...")
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) renderRaceControl(width, height int) string {
	title := cellHeaderStyle.Render("📜 RACE CONTROL & EVENTS")
	divider := lipgloss.NewStyle().Foreground(colorGridBorder).Render(strings.Repeat("─", width))

	var list []string
	list = append(list, title, divider)

	visibleLines := height - 3
	if visibleLines < 1 {
		visibleLines = 1
	}

	msgs := m.state.RaceMessages
	if len(msgs) == 0 {
		list = append(list, "\n  🟢 Track status is normal. Waiting for race incidents...")
	} else {
		// Apply scrolling offset bounds safely
		if m.scrollOffset > len(msgs)-visibleLines {
			m.scrollOffset = len(msgs) - visibleLines
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}

		end := m.scrollOffset + visibleLines
		if end > len(msgs) {
			end = len(msgs)
		}

		for i := m.scrollOffset; i < end; i++ {
			msgLine := msgs[i]

			// Prepend matching emoji based on tags
			emoji := "💬 "
			var style lipgloss.Style

			if strings.Contains(msgLine, "SAFETY CAR") {
				emoji = "🚨 "
				style = lipgloss.NewStyle().Foreground(colorOrange).Bold(true)
			} else if strings.Contains(msgLine, "YELLOW") {
				emoji = "⚠️ "
				style = lipgloss.NewStyle().Foreground(colorYellow)
			} else if strings.Contains(msgLine, "RED FLAG") {
				emoji = "🟥 "
				style = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
			} else if strings.Contains(msgLine, "BLUE FLAG") || strings.Contains(msgLine, "BLUE") {
				emoji = "🟦 "
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
			} else if strings.Contains(msgLine, "BLACK AND WHITE") {
				emoji = "🏁 "
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
			} else if strings.Contains(msgLine, "CHEQUERED") {
				emoji = "🏁 "
				style = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
			} else if strings.Contains(msgLine, "PENALTY") || strings.Contains(msgLine, "INVESTIGATION") {
				emoji = "🛑 "
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("197"))
			} else {
				style = lipgloss.NewStyle().Foreground(colorWhite)
			}

			fullMsg := emoji + msgLine
			// Truncate safely to width before applying ANSI styles to prevent line wrapping
			if len(fullMsg) > width && width > 5 {
				fullMsg = fullMsg[:width-3] + "..."
			}

			list = append(list, style.Render(fullMsg))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, list...)
}

func (m *Model) renderHub() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("202")).Bold(true).MarginBottom(1)
	title := titleStyle.Render("🏎️  FORMULA 1 TIMING TOWER - SESSION HUB")

	var s strings.Builder
	s.WriteString(title + "\n\n")

	for i, session := range curatedSessions {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
		if m.hubCursor == i {
			cursor = "> "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
		}

		s.WriteString(cursor + style.Render(session.Name) + "\n")

		if session.Key == -1 && m.inputMode {
			s.WriteString("    " + m.textInput.View() + "\n")
		}
	}

	s.WriteString("\n")
	s.WriteString(lipgloss.NewStyle().Foreground(colorGridBorder).Render(strings.Repeat("─", 40)) + "\n")

	speedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	if m.focusSpeed {
		s.WriteString(speedStyle.Render("Replay Speed: ") + m.speedInput.View() + "\n\n")
	} else {
		s.WriteString(speedStyle.Render(fmt.Sprintf("Replay Speed: < %.1fx >  (Left/Right to toggle, 's' to type)", m.config.Speed)) + "\n\n")
	}

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	s.WriteString(helpStyle.Render("↑/↓: Select  •  Enter: Confirm  •  q: Quit"))

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGridBorder).
			Padding(1, 4).
			Render(s.String()),
	)
}
