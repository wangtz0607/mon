package cron

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Schedule represents a parsed cron expression with 5 fields:
// minute (0-59), hour (0-23), day-of-month (1-31), month (1-12), day-of-week (0-6, 0=Sunday).
type Schedule struct {
	minute []bool // [0..59]
	hour   []bool // [0..23]
	dom    []bool // [0..31] index 0 unused, 1-31 used
	month  []bool // [0..12] index 0 unused, 1-12 used
	dow    []bool // [0..6]  0=Sunday
}

// Parse parses a standard 5-field cron expression.
// Supported syntax per field: *, N, N-M, */N, N-M/S, and comma-separated combinations.
func Parse(expr string) (*Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	minute, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month: %w", err)
	}
	month, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	dow, err := parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week: %w", err)
	}

	s := &Schedule{
		minute: make([]bool, 60),
		hour:   make([]bool, 24),
		dom:    make([]bool, 32),
		month:  make([]bool, 13),
		dow:    make([]bool, 7),
	}
	for _, v := range minute {
		s.minute[v] = true
	}
	for _, v := range hour {
		s.hour[v] = true
	}
	for _, v := range dom {
		s.dom[v] = true
	}
	for _, v := range month {
		s.month[v] = true
	}
	for _, v := range dow {
		s.dow[v] = true
	}
	return s, nil
}

// Match reports whether t matches this schedule.
func (s *Schedule) Match(t time.Time) bool {
	return s.minute[t.Minute()] &&
		s.hour[t.Hour()] &&
		s.dom[t.Day()] &&
		s.month[int(t.Month())] &&
		s.dow[int(t.Weekday())]
}

// parseField parses a single cron field with the given min/max bounds.
func parseField(field string, min, max int) ([]int, error) {
	var result []int
	for _, part := range strings.Split(field, ",") {
		vals, err := parsePart(part, min, max)
		if err != nil {
			return nil, err
		}
		result = append(result, vals...)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return result, nil
}

// parsePart parses a single element: *, N, N-M, */S, N-M/S.
func parsePart(part string, min, max int) ([]int, error) {
	// Split on '/' first for step handling
	var rangeStr string
	step := 1
	if idx := strings.Index(part, "/"); idx != -1 {
		rangeStr = part[:idx]
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("invalid step in %s", part)
		}
		step = s
	} else {
		rangeStr = part
	}

	var lo, hi int
	if rangeStr == "*" {
		lo, hi = min, max
	} else if idx := strings.Index(rangeStr, "-"); idx != -1 {
		var err error
		lo, err = strconv.Atoi(rangeStr[:idx])
		if err != nil {
			return nil, fmt.Errorf("invalid range in %s", part)
		}
		hi, err = strconv.Atoi(rangeStr[idx+1:])
		if err != nil {
			return nil, fmt.Errorf("invalid range in %s", part)
		}
	} else {
		v, err := strconv.Atoi(rangeStr)
		if err != nil {
			return nil, fmt.Errorf("invalid value %s", part)
		}
		lo, hi = v, v
	}

	if lo < min || hi > max || lo > hi {
		return nil, fmt.Errorf("value out of range in %s (allowed %d-%d)", part, min, max)
	}

	var vals []int
	for i := lo; i <= hi; i += step {
		vals = append(vals, i)
	}
	return vals, nil
}

type job struct {
	schedule *Schedule
	fn       func()
}

// Scheduler runs cron jobs on a 1-minute tick.
type Scheduler struct {
	loc  *time.Location
	mu   sync.Mutex
	jobs []job
}

// New creates a new Scheduler. Times are matched in the given location.
// If loc is nil, time.Local is used.
func New(loc *time.Location) *Scheduler {
	if loc == nil {
		loc = time.Local
	}
	return &Scheduler{loc: loc}
}

// Add registers a job to run whenever the schedule matches.
func (s *Scheduler) Add(schedule *Schedule, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job{schedule: schedule, fn: fn})
}

// HasJobs reports whether any jobs have been registered.
func (s *Scheduler) HasJobs() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs) > 0
}

// Start runs the scheduler loop. It blocks until ctx is cancelled,
// then waits for any in-flight jobs to finish before returning.
func (s *Scheduler) Start(ctx context.Context) {
	// Align to the start of the next minute
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()

	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-timer.C:
			now := time.Now().In(s.loc)
			t := now.Truncate(time.Minute)
			s.mu.Lock()
			for _, j := range s.jobs {
				if j.schedule.Match(t) {
					wg.Add(1)
					go func(fn func()) {
						defer wg.Done()
						fn()
					}(j.fn)
				}
			}
			s.mu.Unlock()
			next = t.Add(time.Minute)
			timer.Reset(time.Until(next))
		}
	}
}
