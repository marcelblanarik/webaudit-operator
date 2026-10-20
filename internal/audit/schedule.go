// Package audit holds the pure scheduling and result-evaluation logic of the operator.
package audit

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// maxFireIterations bounds the catch-up loop in MostRecentFire.
const maxFireIterations = 100000

// parser accepts standard 5-field cron only: no seconds, no descriptors such as @every.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func parse(schedule string) (cron.Schedule, error) {
	// cron honours these prefixes before any parser option; schedules are UTC-only.
	if strings.HasPrefix(schedule, "TZ=") || strings.HasPrefix(schedule, "CRON_TZ=") {
		return nil, fmt.Errorf("invalid schedule %q: timezone prefixes are not supported, schedules are UTC", schedule)
	}
	s, err := parser.Parse(schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	return s, nil
}

// NextFire returns the first fire time strictly after the given time, evaluated in UTC.
func NextFire(schedule string, after time.Time) (time.Time, error) {
	s, err := parse(schedule)
	if err != nil {
		return time.Time{}, err
	}
	next := s.Next(after.UTC())
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("schedule %q never fires", schedule)
	}
	return next, nil
}

// MostRecentFire returns the latest fire time in (since, now], at most one however many were missed.
func MostRecentFire(schedule string, since, now time.Time) (fire time.Time, ok bool, err error) {
	s, err := parse(schedule)
	if err != nil {
		return time.Time{}, false, err
	}
	now = now.UTC()
	t := since.UTC()
	for range maxFireIterations {
		n := s.Next(t)
		if n.IsZero() || n.After(now) {
			return fire, ok, nil
		}
		fire, ok, t = n, true, n
	}
	return time.Time{}, false, fmt.Errorf("schedule %q fires more than %d times between %s and %s", schedule, maxFireIterations, since, now)
}
