package source

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type ReplaySource struct {
	SessionKey      int
	SpeedMultiplier float64
	MaxSleep        time.Duration
	drivers         map[int]Driver
	session         Session
	stopChan        chan struct{}
}

func NewReplaySource(sessionKey int, speedMultiplier float64, maxSleep time.Duration) *ReplaySource {
	if speedMultiplier <= 0 {
		speedMultiplier = 1.0
	}
	if maxSleep <= 0 {
		maxSleep = 2 * time.Second
	}
	return &ReplaySource{
		SessionKey:      sessionKey,
		SpeedMultiplier: speedMultiplier,
		MaxSleep:        maxSleep,
		stopChan:        make(chan struct{}),
	}
}

func (r *ReplaySource) GetSession() (Session, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/sessions?session_key=%d", r.SessionKey)
	resp, err := http.Get(url)
	if err != nil {
		return Session{}, err
	}
	defer resp.Body.Close()

	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return Session{}, err
	}
	if len(sessions) == 0 {
		return Session{}, fmt.Errorf("session %d not found", r.SessionKey)
	}
	r.session = sessions[0]
	return r.session, nil
}

func (r *ReplaySource) GetDrivers() (map[int]Driver, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/drivers?session_key=%d", r.SessionKey)
	resp, err := http.Get(url)
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
	r.drivers = drivers
	return drivers, nil
}

func (r *ReplaySource) Start() (<-chan Event, <-chan error, error) {
	eventChan := make(chan Event, 100)
	errChan := make(chan error, 5)

	// Fetch all datasets concurrently or sequentially
	go func() {
		defer close(eventChan)
		defer close(errChan)

		// 1. Fetch Laps
		laps, err := r.fetchLaps()
		if err != nil {
			errChan <- fmt.Errorf("failed to fetch laps: %w", err)
			return
		}

		// 2. Fetch Positions
		positions, err := r.fetchPositions()
		if err != nil {
			errChan <- fmt.Errorf("failed to fetch positions: %w", err)
			return
		}

		// 3. Fetch Race Control
		raceControl, err := r.fetchRaceControl()
		if err != nil {
			errChan <- fmt.Errorf("failed to fetch race control: %w", err)
			return
		}

		// 4. Fetch Weather
		weather, err := r.fetchWeather()
		if err != nil {
			errChan <- fmt.Errorf("failed to fetch weather: %w", err)
			return
		}

		// 5. Combine and Sort chronologically
		var events []Event

		for i := range laps {
			events = append(events, Event{
				Type:      "lap",
				Timestamp: laps[i].Date,
				Lap:       &laps[i],
			})
		}
		for i := range positions {
			events = append(events, Event{
				Type:      "position",
				Timestamp: positions[i].Date,
				Position:  &positions[i],
			})
		}
		for i := range raceControl {
			events = append(events, Event{
				Type:      "race_control",
				Timestamp: raceControl[i].Date,
				RaceControl: &raceControl[i],
			})
		}
		for i := range weather {
			events = append(events, Event{
				Type:      "weather",
				Timestamp: weather[i].Date,
				Weather:   &weather[i],
			})
		}

		// Sort events chronologically by timestamp
		sort.Slice(events, func(i, j int) bool {
			return events[i].Timestamp.Before(events[j].Timestamp)
		})

		if len(events) == 0 {
			errChan <- fmt.Errorf("no events found for session %d", r.SessionKey)
			return
		}

		// 6. Chronological Streaming Loop
		var lastTime time.Time
		for i, event := range events {
			select {
			case <-r.stopChan:
				return
			default:
			}

			if i > 0 && !lastTime.IsZero() {
				gap := event.Timestamp.Sub(lastTime)
				if gap > 0 {
					// Apply speed multiplier
					sleepDur := time.Duration(float64(gap) / r.SpeedMultiplier)
					// Cap maximum sleep duration
					if sleepDur > r.MaxSleep {
						sleepDur = r.MaxSleep
					}
					time.Sleep(sleepDur)
				}
			}

			lastTime = event.Timestamp
			eventChan <- event
		}
	}()

	return eventChan, errChan, nil
}

func (r *ReplaySource) Stop() {
	close(r.stopChan)
}

// Fetch helpers with retry logic
func (r *ReplaySource) fetchAPI(url string) ([]byte, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = err
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}

		if resp.StatusCode == 200 {
			// If it's a JSON object instead of an array (often indicating an API error string), treat it as an error to trigger a retry
			if len(body) > 0 && body[0] == '{' {
				lastErr = fmt.Errorf("API returned object instead of array (rate limit?): %s", string(body))
				time.Sleep(time.Second * time.Duration(i+1))
				continue
			}
			return body, nil
		}

		lastErr = fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		time.Sleep(time.Second * time.Duration(i+1))
	}
	return nil, fmt.Errorf("failed after retries: %v", lastErr)
}

func (r *ReplaySource) fetchLaps() ([]Lap, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/laps?session_key=%d", r.SessionKey)
	body, err := r.fetchAPI(url)
	if err != nil {
		return nil, err
	}

	var laps []Lap
	if err := json.Unmarshal(body, &laps); err != nil {
		return nil, err
	}
	return laps, nil
}

func (r *ReplaySource) fetchPositions() ([]Position, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/position?session_key=%d", r.SessionKey)
	body, err := r.fetchAPI(url)
	if err != nil {
		return nil, err
	}

	var positions []Position
	if err := json.Unmarshal(body, &positions); err != nil {
		return nil, err
	}
	return positions, nil
}

func (r *ReplaySource) fetchRaceControl() ([]RaceControl, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/race_control?session_key=%d", r.SessionKey)
	body, err := r.fetchAPI(url)
	if err != nil {
		return nil, err
	}

	var raceControl []RaceControl
	if err := json.Unmarshal(body, &raceControl); err != nil {
		return nil, err
	}
	return raceControl, nil
}

func (r *ReplaySource) fetchWeather() ([]Weather, error) {
	url := fmt.Sprintf("https://api.openf1.org/v1/weather?session_key=%d", r.SessionKey)
	body, err := r.fetchAPI(url)
	if err != nil {
		return nil, err
	}

	var weather []Weather
	if err := json.Unmarshal(body, &weather); err != nil {
		return nil, err
	}
	return weather, nil
}
