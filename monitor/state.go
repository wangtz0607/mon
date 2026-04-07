package monitor

import (
	"encoding/json"
	"fmt"
)

// State represents the state of a service.
type State int

const (
	Up             State = iota // Service is up and notification was sent
	UpUnnotified                // Service is up but notification failed
	Down                        // Service is down and notification was sent
	DownUnnotified              // Service is down but notification failed
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case Up:
		return "UP"
	case UpUnnotified:
		return "UP_UNNOTIFIED"
	case Down:
		return "DOWN"
	case DownUnnotified:
		return "DOWN_UNNOTIFIED"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON implements json.Marshaler.
func (s State) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *State) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("state must be a string: %w", err)
	}
	switch str {
	case "UP":
		*s = Up
	case "UP_UNNOTIFIED":
		*s = UpUnnotified
	case "DOWN":
		*s = Down
	case "DOWN_UNNOTIFIED":
		*s = DownUnnotified
	default:
		return fmt.Errorf("invalid state %s", str)
	}
	return nil
}
