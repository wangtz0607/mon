package monitor

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"mon/checker"
	"mon/duration"
)

// Status is a JSON-serializable snapshot of a monitor's state.
type Status struct {
	Name                 string          `json:"name"`
	Paused               bool            `json:"paused"`
	State                State           `json:"state"`
	Since                time.Time       `json:"since"`
	LastCheck            *checker.Result `json:"last_check"`
	ConsecutiveFailures  int             `json:"consecutive_failures"`
	ConsecutiveSuccesses int             `json:"consecutive_successes"`
}

func formatState(status *Status, now time.Time) string {
	var stateStr string
	switch status.State {
	case Up:
		stateStr = "🟢 UP"
	case UpUnnotified:
		stateStr = "🟢 UP_UNNOTIFIED"
	case Down:
		stateStr = "🔴 DOWN"
	case DownUnnotified:
		stateStr = "🔴 DOWN_UNNOTIFIED"
	default:
		stateStr = status.State.String()
	}
	stateStr += fmt.Sprintf(" %.3v", duration.Sub(now, status.Since))
	if status.Paused {
		stateStr += " (⏸️  PAUSED)"
	}
	return stateStr
}

// FormatStatus returns a human-readable status report for a service.
func FormatStatus(status *Status, now time.Time) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Name: %s\n", status.Name)
	fmt.Fprintf(&sb, "State: %s\n", formatState(status, now))

	if status.LastCheck != nil {
		fmt.Fprintf(&sb, "Last Check: %s\n", *status.LastCheck)
	}

	if status.ConsecutiveFailures > 0 {
		fmt.Fprintf(&sb, "Consecutive Failures: %d\n", status.ConsecutiveFailures)
	} else {
		fmt.Fprintf(&sb, "Consecutive Successes: %d\n", status.ConsecutiveSuccesses)
	}

	return sb.String()
}

// FormatStatusTable returns a tabwriter-formatted table of multiple services' status.
func FormatStatusTable(services []Status, now time.Time) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "NAME\tSTATE\n")
	for _, status := range services {
		fmt.Fprintf(w, "%s\t%s\n", status.Name, formatState(&status, now))
	}
	w.Flush()
	return sb.String()
}
