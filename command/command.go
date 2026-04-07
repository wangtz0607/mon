package command

import (
	"context"

	"mon/checker"
	"mon/monitor"
)

// CheckResult holds the outcome of a one-time health check.
type CheckResult struct {
	ServiceName string         `json:"service_name"`
	Result      checker.Result `json:"result"`
}

// NotifyResult holds notification results for a single service.
type NotifyResult struct {
	ServiceName string                 `json:"service_name"`
	Results     []monitor.NotifyResult `json:"results"`
}

// Check performs a one-time health check on monitors matching the service filter.
// Returns check results without modifying monitor state or triggering notifications.
func Check(monitors []*monitor.Monitor, serviceName string) []CheckResult {
	var targets []*monitor.Monitor
	for _, m := range monitors {
		if serviceName != "" && m.Name != serviceName {
			continue
		}
		targets = append(targets, m)
	}
	ch := make(chan CheckResult, len(targets))
	for _, m := range targets {
		go func(m *monitor.Monitor) {
			ch <- CheckResult{
				ServiceName: m.Name,
				Result:      m.Checker.Check(context.Background()),
			}
		}(m)
	}
	results := make([]CheckResult, 0, len(targets))
	for range targets {
		results = append(results, <-ch)
	}
	return results
}

// Notify applies the action to monitors matching the service filter and
// collects notification results.
func Notify(monitors []*monitor.Monitor, serviceName string, action func(*monitor.Monitor) []monitor.NotifyResult) []NotifyResult {
	var targets []*monitor.Monitor
	for _, m := range monitors {
		if serviceName != "" && m.Name != serviceName {
			continue
		}
		targets = append(targets, m)
	}
	ch := make(chan NotifyResult, len(targets))
	for _, m := range targets {
		go func(m *monitor.Monitor) {
			ch <- NotifyResult{
				ServiceName: m.Name,
				Results:     action(m),
			}
		}(m)
	}
	results := make([]NotifyResult, 0, len(targets))
	for range targets {
		results = append(results, <-ch)
	}
	return results
}

// PauseResume pauses or resumes monitors matching the service filter.
// Returns affected service names.
func PauseResume(monitors []*monitor.Monitor, serviceName, action string) []string {
	var affected []string
	for _, m := range monitors {
		if serviceName != "" && m.Name != serviceName {
			continue
		}
		if action == "pause" {
			m.Pause()
		} else {
			m.Resume()
		}
		affected = append(affected, m.Name)
	}
	return affected
}

// Status returns status snapshots for monitors matching the service filter.
func Status(monitors []*monitor.Monitor, serviceName string) []monitor.Status {
	var statuses []monitor.Status
	for _, m := range monitors {
		if serviceName != "" && m.Name != serviceName {
			continue
		}
		statuses = append(statuses, m.Status())
	}
	return statuses
}
