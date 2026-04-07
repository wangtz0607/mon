package duration

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration represents a duration with custom serialization support.
// It provides both YAML and JSON marshaling/unmarshaling with human-readable format.
type Duration time.Duration

// Common duration constants
const (
	Nanosecond  Duration = Duration(time.Nanosecond)
	Microsecond Duration = Duration(time.Microsecond)
	Millisecond Duration = Duration(time.Millisecond)
	Second      Duration = Duration(time.Second)
	Minute      Duration = Duration(time.Minute)
	Hour        Duration = Duration(time.Hour)
)

// String returns the formatted duration string.
func (d Duration) String() string {
	return Format(time.Duration(d), -1)
}

// MarshalJSON implements json.Marshaler.
// It marshals the duration as a formatted string (e.g., "5m 30s").
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := Parse(s)
	if err != nil {
		return fmt.Errorf("invalid duration string %s: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := Parse(s)
	if err != nil {
		return fmt.Errorf("invalid duration %s: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Hours returns the duration as a floating point number of hours.
func (d Duration) Hours() float64 {
	return time.Duration(d).Hours()
}

// Minutes returns the duration as a floating point number of minutes.
func (d Duration) Minutes() float64 {
	return time.Duration(d).Minutes()
}

// Seconds returns the duration as a floating point number of seconds.
func (d Duration) Seconds() float64 {
	return time.Duration(d).Seconds()
}

// Milliseconds returns the duration as an integer millisecond count.
func (d Duration) Milliseconds() int64 {
	return time.Duration(d).Milliseconds()
}

// Microseconds returns the duration as an integer microsecond count.
func (d Duration) Microseconds() int64 {
	return time.Duration(d).Microseconds()
}

// Nanoseconds returns the duration as an integer nanosecond count.
func (d Duration) Nanoseconds() int64 {
	return time.Duration(d).Nanoseconds()
}

// Truncate returns the result of rounding d toward zero to a multiple of m.
// If m <= 0, Truncate returns d unchanged.
func (d Duration) Truncate(m Duration) Duration {
	if m <= 0 {
		return d
	}
	return Duration((time.Duration(d) / time.Duration(m)) * time.Duration(m))
}

// Sub returns the duration t - u.
func Sub(t, u time.Time) Duration {
	return Duration(t.Sub(u))
}

// Since returns the duration elapsed since t.
func Since(t time.Time) Duration {
	return Duration(time.Since(t))
}

// Format formats a duration as "Xd Xh Xm Xs" for human consumption.
// If the seconds component is not a whole number, it is formatted with decimals (e.g. "1.5s").
// If precision is negative, trailing zeros are removed (default behavior).
func Format(d time.Duration, precision int) string {
	if d < 0 {
		return "-(" + Format(-d, precision) + ")"
	}
	days := d / (24 * time.Hour)
	hours := d / time.Hour % 24
	minutes := d / time.Minute % 60

	var secondsStr string
	if d%time.Second == 0 {
		secondsStr = strconv.FormatInt(int64(d/time.Second%60), 10)
	} else {
		secondsStr = strconv.FormatFloat((d % time.Minute).Seconds(), 'f', precision, 64)
	}

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ss", days, hours, minutes, secondsStr)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ss", hours, minutes, secondsStr)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ss", minutes, secondsStr)
	}
	return fmt.Sprintf("%ss", secondsStr)
}

// Format implements fmt.Formatter.
// It supports formatting with precision using the 'v' verb.
// Precision specifies the number of decimal places for fractional seconds.
func (d Duration) Format(f fmt.State, verb rune) {
	// Get precision from state, default to -1 (remove trailing zeros)
	precision, ok := f.Precision()
	if !ok {
		precision = -1
	}
	fmt.Fprint(f, Format(time.Duration(d), precision))
}

// Parse parses a duration string in the format "Xd Xh Xm Xs".
func Parse(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("duration: empty string")
	}

	// Handle negative: -(inner)
	if strings.HasPrefix(s, "-(") && strings.HasSuffix(s, ")") {
		d, err := Parse(s[2 : len(s)-1])
		if err != nil {
			return 0, err
		}
		return -d, nil
	}

	// Any subset of Xd, Xh, Xm, Xs; seconds may be fractional (e.g. "1.5s")
	var total time.Duration
	for _, part := range strings.Fields(s) {
		var n int64
		var f float64
		var err error
		switch {
		case strings.HasSuffix(part, "d"):
			n, err = strconv.ParseInt(strings.TrimSuffix(part, "d"), 10, 64)
			total += time.Duration(n) * 24 * time.Hour
		case strings.HasSuffix(part, "h"):
			n, err = strconv.ParseInt(strings.TrimSuffix(part, "h"), 10, 64)
			total += time.Duration(n) * time.Hour
		case strings.HasSuffix(part, "m"):
			n, err = strconv.ParseInt(strings.TrimSuffix(part, "m"), 10, 64)
			total += time.Duration(n) * time.Minute
		case strings.HasSuffix(part, "s"):
			f, err = strconv.ParseFloat(strings.TrimSuffix(part, "s"), 64)
			total += time.Duration(f * float64(time.Second))
		default:
			return 0, fmt.Errorf("duration: unknown component %s in %s", part, s)
		}
		if err != nil {
			return 0, fmt.Errorf("duration: invalid component %s in %s", part, s)
		}
	}
	return total, nil
}
