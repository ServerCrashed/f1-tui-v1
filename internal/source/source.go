package source

import "time"

type Driver struct {
	DriverNumber int    `json:"driver_number"`
	FullName     string `json:"full_name"`
	Acronym      string `json:"name_acronym"`
	TeamName     string `json:"team_name"`
	TeamColour   string `json:"team_colour"`
}

type Lap struct {
	DriverNumber int       `json:"driver_number"`
	LapNumber    int       `json:"lap_number"`
	LapDuration  float64   `json:"lap_duration"`
	Sector1      float64   `json:"duration_sector_1"`
	Sector2      float64   `json:"duration_sector_2"`
	Sector3      float64   `json:"duration_sector_3"`
	Date         time.Time `json:"date_start"`
}

type Position struct {
	DriverNumber int       `json:"driver_number"`
	Position     int       `json:"position"`
	Date         time.Time `json:"date"`
}

type RaceControl struct {
	Flag     string    `json:"flag"`
	Message  string    `json:"message"`
	Category string    `json:"category"`
	Scope    string    `json:"scope"`
	Date     time.Time `json:"date"`
}

type Weather struct {
	AirTemp   float64   `json:"air_temperature"`
	TrackTemp float64   `json:"track_temperature"`
	Rainfall  int       `json:"rainfall"` // 0 or 1
	Date      time.Time `json:"date"`
}

type Session struct {
	SessionKey  int       `json:"session_key"`
	SessionName string    `json:"session_name"`
	MeetingKey  int       `json:"meeting_key"`
	Location    string    `json:"location"`
	CountryName string    `json:"country_name"`
	DateStart   time.Time `json:"date_start"`
}

// Unified Event for the channel
type Event struct {
	Type        string // "lap", "position", "race_control", "weather"
	Timestamp   time.Time
	Lap         *Lap
	Position    *Position
	RaceControl *RaceControl
	Weather     *Weather
}

type Source interface {
	Start() (<-chan Event, <-chan error, error)
	GetDrivers() (map[int]Driver, error)
	GetSession() (Session, error)
	Stop()
}
