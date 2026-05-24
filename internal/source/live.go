package source

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

type LiveSource struct {
	Token      string
	SessionKey int
	drivers    map[int]Driver
	session    Session
	stopChan   chan struct{}
}

func NewLiveSource(token string, sessionKey int) *LiveSource {
	return &LiveSource{
		Token:      token,
		SessionKey: sessionKey,
		stopChan:   make(chan struct{}),
	}
}

func (l *LiveSource) newRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	if l.Token != "" {
		req.Header.Set("Authorization", "Bearer "+l.Token)
	}
	return req, nil
}

func (l *LiveSource) GetSession() (Session, error) {
	var url string
	if l.SessionKey > 0 {
		url = fmt.Sprintf("https://api.openf1.org/v1/sessions?session_key=%d", l.SessionKey)
	} else {
		url = "https://api.openf1.org/v1/sessions?session_key=latest"
	}

	req, err := l.newRequest("GET", url)
	if err != nil {
		return Session{}, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Session{}, err
	}
	defer resp.Body.Close()

	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return Session{}, err
	}
	if len(sessions) == 0 {
		return Session{}, fmt.Errorf("no session found")
	}
	l.session = sessions[0]
	l.SessionKey = l.session.SessionKey
	return l.session, nil
}

func (l *LiveSource) GetDrivers() (map[int]Driver, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/drivers?session_key=%d", l.SessionKey)
	req, err := l.newRequest("GET", url)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var driversList []Driver
	if err := json.NewDecoder(resp.Body).Decode(&driversList); err != nil {
		return nil, err
	}

	drivers := make(map[int]Driver)
	for _, d := range driversList {
		drivers[d.DriverNumber] = d
	}
	l.drivers = drivers
	return drivers, nil
}

func (l *LiveSource) Start() (<-chan Event, <-chan error, error) {
	eventChan := make(chan Event, 50)
	errChan := make(chan error, 5)

	go func() {
		defer close(eventChan)
		defer close(errChan)

		// Start polling from the current time minus 5 minutes to capture initial state,
		// or use the current time.
		lastTimestamp := time.Now().UTC().Add(-5 * time.Minute)
		client := &http.Client{Timeout: 5 * time.Second}

		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-l.stopChan:
				return
			case <-ticker.C:
			}

			// Format timestamp for OpenF1 query format: "YYYY-MM-DDTHH:MM:SS.mmm"
			tsStr := lastTimestamp.Format("2006-01-02T15:04:05.000")

			// Fetch dynamic updates
			var rawEvents []Event

			// Laps
			laps, err := l.fetchLaps(client, tsStr)
			if err == nil {
				for i := range laps {
					rawEvents = append(rawEvents, Event{
						Type:      "lap",
						Timestamp: laps[i].Date,
						Lap:       &laps[i],
					})
				}
			}

			// Positions
			positions, err := l.fetchPositions(client, tsStr)
			if err == nil {
				for i := range positions {
					rawEvents = append(rawEvents, Event{
						Type:      "position",
						Timestamp: positions[i].Date,
						Position:  &positions[i],
					})
				}
			}

			// Race Control
			raceControl, err := l.fetchRaceControl(client, tsStr)
			if err == nil {
				for i := range raceControl {
					rawEvents = append(rawEvents, Event{
						Type:      "race_control",
						Timestamp: raceControl[i].Date,
						RaceControl: &raceControl[i],
					})
				}
			}

			// Weather
			weather, err := l.fetchWeather(client, tsStr)
			if err == nil {
				for i := range weather {
					rawEvents = append(rawEvents, Event{
						Type:      "weather",
						Timestamp: weather[i].Date,
						Weather:   &weather[i],
					})
				}
			}

			// Sort chronological updates
			sort.Slice(rawEvents, func(i, j int) bool {
				return rawEvents[i].Timestamp.Before(rawEvents[j].Timestamp)
			})

			for _, ev := range rawEvents {
				eventChan <- ev
				if ev.Timestamp.After(lastTimestamp) {
					lastTimestamp = ev.Timestamp
				}
			}
		}
	}()

	return eventChan, errChan, nil
}

func (l *LiveSource) Stop() {
	close(l.stopChan)
}

func (l *LiveSource) fetchLaps(client *http.Client, lastTime string) ([]Lap, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/laps?session_key=%d&date>%s", l.SessionKey, lastTime)
	req, err := l.newRequest("GET", url)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var laps []Lap
	if err := json.NewDecoder(resp.Body).Decode(&laps); err != nil {
		return nil, err
	}
	return laps, nil
}

func (l *LiveSource) fetchPositions(client *http.Client, lastTime string) ([]Position, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/position?session_key=%d&date>%s", l.SessionKey, lastTime)
	req, err := l.newRequest("GET", url)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var positions []Position
	if err := json.NewDecoder(resp.Body).Decode(&positions); err != nil {
		return nil, err
	}
	return positions, nil
}

func (l *LiveSource) fetchRaceControl(client *http.Client, lastTime string) ([]RaceControl, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/race_control?session_key=%d&date>%s", l.SessionKey, lastTime)
	req, err := l.newRequest("GET", url)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raceControl []RaceControl
	if err := json.NewDecoder(resp.Body).Decode(&raceControl); err != nil {
		return nil, err
	}
	return raceControl, nil
}

func (l *LiveSource) fetchWeather(client *http.Client, lastTime string) ([]Weather, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/weather?session_key=%d&date>%s", l.SessionKey, lastTime)
	req, err := l.newRequest("GET", url)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var weather []Weather
	if err := json.NewDecoder(resp.Body).Decode(&weather); err != nil {
		return nil, err
	}
	return weather, nil
}
