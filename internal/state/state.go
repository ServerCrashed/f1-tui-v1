package state

import (
	"fmt"
	"sort"
	"strings"

	"f1-tui/internal/source"
)

type DriverState struct {
	Number       int
	Acronym      string
	FullName     string
	TeamName     string
	TeamColour   string
	Position     int
	Laps         int
	LastLapTime  float64
	S1           float64
	S2           float64
	S3           float64
	PitCount     int
	TireCompound string
	TotalTime    float64 // Sum of all lap durations
	Retired      bool
	InPit        bool
	GapToLeader  string
	Interval     string
}

type SessionState struct {
	SessionName   string
	Location      string
	TrackFlag     string
	FlagMessage   string
	AirTemp       float64
	TrackTemp     float64
	Rain          bool
	RaceMessages  []string
	Drivers       map[int]source.Driver
	DriverStates  map[int]*DriverState
	Standings     []*DriverState
	LastPositions map[int]int // Keeps track of last position changes
}

func NewSessionState() *SessionState {
	return &SessionState{
		TrackFlag:     "GREEN",
		Drivers:       make(map[int]source.Driver),
		DriverStates:  make(map[int]*DriverState),
		Standings:     make([]*DriverState, 0),
		LastPositions: make(map[int]int),
	}
}

func (s *SessionState) SetDrivers(drivers map[int]source.Driver) {
	s.Drivers = drivers
	for num, d := range drivers {
		// Clean up team color if empty
		color := d.TeamColour
		if color == "" {
			color = "FFFFFF" // Default white
		}
		s.DriverStates[num] = &DriverState{
			Number:       num,
			Acronym:      d.Acronym,
			FullName:     d.FullName,
			TeamName:     d.TeamName,
			TeamColour:   color,
			TireCompound: "-",
			GapToLeader:  "-",
			Interval:     "-",
		}
	}
}

func (s *SessionState) SetSession(session source.Session) {
	s.SessionName = session.SessionName
	s.Location = fmt.Sprintf("%s (%s)", session.Location, session.CountryName)
}

func (s *SessionState) ApplyEvent(ev source.Event) {
	switch ev.Type {
	case "lap":
		if ev.Lap == nil {
			return
		}
		lap := ev.Lap
		ds, exists := s.DriverStates[lap.DriverNumber]
		if !exists {
			return
		}

		// Update lap stats
		ds.Laps = lap.LapNumber
		if lap.LapDuration > 0 {
			ds.LastLapTime = lap.LapDuration
			ds.TotalTime += lap.LapDuration
		}
		if lap.Sector1 > 0 {
			ds.S1 = lap.Sector1
		}
		if lap.Sector2 > 0 {
			ds.S2 = lap.Sector2
		}
		if lap.Sector3 > 0 {
			ds.S3 = lap.Sector3
		}

	case "position":
		if ev.Position == nil {
			return
		}
		pos := ev.Position
		ds, exists := s.DriverStates[pos.DriverNumber]
		if !exists {
			return
		}

		// Update position
		ds.Position = pos.Position
		s.rebuildStandings()

	case "race_control":
		if ev.RaceControl == nil {
			return
		}
		rc := ev.RaceControl
		
		// Update flags if it is a global track flag event (Green, Yellow, Red, SC, VSC)
		if rc.Flag != "" {
			flag := strings.ToUpper(rc.Flag)
			if flag == "GREEN" || flag == "CLEAR" || flag == "YELLOW" || flag == "DOUBLE YELLOW" || flag == "RED" || flag == "SC" || flag == "VSC" || flag == "SAFETY CAR" {
				s.TrackFlag = flag
				s.FlagMessage = rc.Message
			}
		}

		// Save scrolling messages
		timeStr := rc.Date.Format("15:04:05")
		msg := fmt.Sprintf("[%s] %s", timeStr, rc.Message)
		// Prepend to show latest on top or append and keep list short
		s.RaceMessages = append([]string{msg}, s.RaceMessages...)
		if len(s.RaceMessages) > 50 {
			s.RaceMessages = s.RaceMessages[:50]
		}

	case "weather":
		if ev.Weather == nil {
			return
		}
		w := ev.Weather
		s.AirTemp = w.AirTemp
		s.TrackTemp = w.TrackTemp
		s.Rain = w.Rainfall > 0
	}

	// Recalculate Gaps and Intervals after any state update
	s.calculateGaps()
}

func (s *SessionState) rebuildStandings() {
	var active []*DriverState
	for _, ds := range s.DriverStates {
		if ds.Position > 0 {
			active = append(active, ds)
		}
	}

	// Sort by Position ascending (1st place first)
	sort.Slice(active, func(i, j int) bool {
		return active[i].Position < active[j].Position
	})

	s.Standings = active
}

func (s *SessionState) calculateGaps() {
	if len(s.Standings) == 0 {
		return
	}

	leader := s.Standings[0]
	leader.GapToLeader = "LEADER"
	leader.Interval = "LEADER"

	for i := 1; i < len(s.Standings); i++ {
		curr := s.Standings[i]
		prev := s.Standings[i-1]

		// Gap to Leader
		if curr.Laps < leader.Laps {
			diff := leader.Laps - curr.Laps
			if diff == 1 {
				curr.GapToLeader = "+1 Lap"
			} else {
				curr.GapToLeader = fmt.Sprintf("+%d Laps", diff)
			}
		} else if curr.TotalTime > 0 && leader.TotalTime > 0 {
			diff := curr.TotalTime - leader.TotalTime
			if diff > 0 {
				curr.GapToLeader = fmt.Sprintf("+%.3fs", diff)
			} else {
				curr.GapToLeader = "-"
			}
		} else {
			curr.GapToLeader = "-"
		}

		// Interval to car ahead
		if curr.Laps < prev.Laps {
			diff := prev.Laps - curr.Laps
			if diff == 1 {
				curr.Interval = "+1 Lap"
			} else {
				curr.Interval = fmt.Sprintf("+%d Laps", diff)
			}
		} else if curr.TotalTime > 0 && prev.TotalTime > 0 {
			diff := curr.TotalTime - prev.TotalTime
			if diff > 0 {
				curr.Interval = fmt.Sprintf("+%.3fs", diff)
			} else {
				curr.Interval = "-"
			}
		} else {
			curr.Interval = "-"
		}
	}
}

// Format duration helper e.g. 75.342 -> 1:15.342
func FormatDuration(sec float64) string {
	if sec <= 0 {
		return "-"
	}
	if sec < 60 {
		return fmt.Sprintf("%.3f", sec)
	}
	m := int(sec) / 60
	s := sec - float64(m*60)
	return fmt.Sprintf("%d:%06.3f", m, s)
}
