package monitor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"mon/checker"
	"mon/duration"
	"mon/notifier"
)

// Monitor runs periodic health checks for a single service.
type Monitor struct {
	Name             string
	Checker          checker.Checker
	Notifiers        []notifier.Notifier
	Interval         duration.Duration
	FailureThreshold int
	SuccessThreshold int
	Logger           *zap.SugaredLogger

	mu                   sync.Mutex
	paused               bool
	state                State
	since                time.Time
	lastCheck            *checker.Result
	consecutiveFailures  int
	consecutiveSuccesses int
	failureHistory       []checker.Result
	successHistory       []checker.Result
}

// Pause stops health checks for this monitor without stopping the goroutine.
func (m *Monitor) Pause() {
	m.mu.Lock()
	m.paused = true
	m.mu.Unlock()
	m.Logger.Infof("%s: Monitor paused", m.Name)
}

// Resume resumes health checks for this monitor.
func (m *Monitor) Resume() {
	m.mu.Lock()
	m.paused = false
	m.mu.Unlock()
	m.Logger.Infof("%s: Monitor resumed", m.Name)
}

// Status returns a thread-safe snapshot of the monitor's current state.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := Status{
		Name:                 m.Name,
		Paused:               m.paused,
		State:                m.state,
		Since:                m.since,
		LastCheck:            m.lastCheck,
		ConsecutiveFailures:  m.consecutiveFailures,
		ConsecutiveSuccesses: m.consecutiveSuccesses,
	}
	return s
}

// Run starts the monitoring loop. It blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	m.mu.Lock()
	now := time.Now()
	m.since = now
	m.mu.Unlock()

	m.Logger.Infof("%s: Monitor started", m.Name)
	ticker := time.NewTicker(time.Duration(m.Interval))
	defer ticker.Stop()

	// Run first check immediately
	m.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			m.Logger.Infof("%s: Monitor stopped", m.Name)
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

const (
	actionNone = iota
	actionAlert
	actionRecovery
)

func (m *Monitor) tick(ctx context.Context) {
	m.mu.Lock()
	paused := m.paused
	m.mu.Unlock()
	if paused {
		m.Logger.Debugf("%s: Skipping check (paused)", m.Name)
		return
	}

	result := m.Checker.Check(ctx) // outside lock

	m.mu.Lock()
	// Update last check fields
	m.lastCheck = &result

	var action int
	if result.OK {
		m.consecutiveFailures = 0
		m.failureHistory = m.failureHistory[:0]
		m.consecutiveSuccesses++
		m.successHistory = append(m.successHistory, result)
		if len(m.successHistory) > m.SuccessThreshold {
			m.successHistory = m.successHistory[len(m.successHistory)-m.SuccessThreshold:]
		}
		if m.state != Up && m.consecutiveSuccesses >= m.SuccessThreshold {
			action = actionRecovery
		}
	} else {
		m.consecutiveSuccesses = 0
		m.successHistory = m.successHistory[:0]
		m.consecutiveFailures++
		m.failureHistory = append(m.failureHistory, result)
		if len(m.failureHistory) > m.FailureThreshold {
			m.failureHistory = m.failureHistory[len(m.failureHistory)-m.FailureThreshold:]
		}
		if m.state != Down && m.consecutiveFailures >= m.FailureThreshold {
			action = actionAlert
		}
	}
	var failureHistoryCopy []checker.Result
	if action == actionAlert {
		failureHistoryCopy = append([]checker.Result(nil), m.failureHistory...)
	}
	var successHistoryCopy []checker.Result
	if action == actionRecovery {
		successHistoryCopy = append([]checker.Result(nil), m.successHistory...)
	}
	m.mu.Unlock()

	// Logging outside lock
	if result.OK {
		m.Logger.Infof("%s: %s", m.Name, result)
	} else {
		m.Logger.Warnf("%s: %s", m.Name, result)
	}

	switch action {
	case actionAlert:
		changed := false
		m.mu.Lock()
		if m.state != DownUnnotified {
			m.state = DownUnnotified
			m.since = time.Now()
			changed = true
		}
		m.mu.Unlock()
		if changed {
			m.Logger.Errorf("%s: State changed to DOWN_UNNOTIFIED", m.Name)
		}
		if m.sendAlert(ctx, failureHistoryCopy) {
			m.mu.Lock()
			m.state = Down
			m.mu.Unlock()
			m.Logger.Errorf("%s: State changed to DOWN", m.Name)
		}
	case actionRecovery:
		changed := false
		m.mu.Lock()
		if m.state != UpUnnotified {
			m.state = UpUnnotified
			m.since = time.Now()
			changed = true
		}
		m.mu.Unlock()
		if changed {
			m.Logger.Infof("%s: State changed to UP_UNNOTIFIED", m.Name)
		}
		if m.sendRecovery(ctx, successHistoryCopy) {
			m.mu.Lock()
			m.state = Up
			m.mu.Unlock()
			m.Logger.Infof("%s: State changed to UP", m.Name)
		}
	}
}

// NotifyResult holds the outcome of a notification for a single notifier.
type NotifyResult struct {
	NotifierName string `json:"notifier_name"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
}

// Notify sends a notification with the given subject and body through each of the monitor's notifiers.
// If notifierName is non-empty, only notifiers matching that name are used.
func (m *Monitor) Notify(ctx context.Context, notifierName, subject, body string) []NotifyResult {
	var targets []notifier.Notifier
	for _, n := range m.Notifiers {
		if notifierName != "" && n.Name() != notifierName {
			continue
		}
		targets = append(targets, n)
	}
	ch := make(chan NotifyResult, len(targets))
	for _, n := range targets {
		go func(n notifier.Notifier) {
			r := NotifyResult{NotifierName: n.Name()}
			if err := n.Notify(ctx, subject, body); err != nil {
				r.Error = err.Error()
			} else {
				r.Success = true
			}
			ch <- r
		}(n)
	}
	results := make([]NotifyResult, 0, len(targets))
	for range targets {
		results = append(results, <-ch)
	}
	return results
}

// TestNotify sends a test notification through each of the monitor's notifiers.
// If notifierName is non-empty, only notifiers matching that name are tested.
func (m *Monitor) TestNotify(ctx context.Context, notifierName string) []NotifyResult {
	return m.Notify(ctx, notifierName, fmt.Sprintf("🔔 %s", m.Name), "It works!")
}

// sendAlert sends alert notifications. Returns true if at least one notifier succeeded.
func (m *Monitor) sendAlert(ctx context.Context, history []checker.Result) bool {
	subject := fmt.Sprintf("🔴 %s is DOWN", m.Name)
	var results []string
	for _, r := range history {
		results = append(results, r.String())
	}
	body := strings.Join(results, "\n\n")
	m.Logger.Infof("%s: Sending alert", m.Name)
	type result struct {
		name string
		err  error
	}
	ch := make(chan result, len(m.Notifiers))
	for _, n := range m.Notifiers {
		go func(n notifier.Notifier) {
			ch <- result{name: n.Name(), err: n.Notify(ctx, subject, body)}
		}(n)
	}
	anyOK := false
	for range m.Notifiers {
		r := <-ch
		if r.err != nil {
			m.Logger.Errorf("%s: %s: Failed to send alert: %v", m.Name, r.name, r.err)
		} else {
			anyOK = true
		}
	}
	return anyOK
}

// sendRecovery sends recovery notifications. Returns true if at least one notifier succeeded.
func (m *Monitor) sendRecovery(ctx context.Context, history []checker.Result) bool {
	subject := fmt.Sprintf("🟢 %s is UP", m.Name)
	var results []string
	for _, r := range history {
		results = append(results, r.String())
	}
	body := strings.Join(results, "\n\n")
	m.Logger.Infof("%s: Sending recovery", m.Name)
	type result struct {
		name string
		err  error
	}
	ch := make(chan result, len(m.Notifiers))
	for _, n := range m.Notifiers {
		go func(n notifier.Notifier) {
			ch <- result{name: n.Name(), err: n.Notify(ctx, subject, body)}
		}(n)
	}
	anyOK := false
	for range m.Notifiers {
		r := <-ch
		if r.err != nil {
			m.Logger.Errorf("%s: %s: Failed to send recovery: %v", m.Name, r.name, r.err)
		} else {
			anyOK = true
		}
	}
	return anyOK
}
