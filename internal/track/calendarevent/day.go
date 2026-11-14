package calendarevent

import (
	"context"
	"fmt"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

const MaxDaySourceEvents = 4096

// DayProjection preserves stored event labels. Timed membership is in the selected display zone;
// all-day membership remains an independent, exclusive-end civil date range.
type DayProjection struct {
	Date         string  `json:"date"`
	TimeZone     string  `json:"time_zone"`
	DayStart     string  `json:"day_start"`
	DayEnd       string  `json:"day_end"`
	DayStartUnix int64   `json:"day_start_unix"`
	DayEndUnix   int64   `json:"day_end_unix"`
	Events       []Event `json:"events"`
}

// Day reads a bounded snapshot without changing event storage.
func Day(cfg *config.Config, date, zone string) (DayProjection, error) {
	return DayContext(context.Background(), cfg, date, zone)
}

// DayContext allows request cancellation during bounded zone traversal.
func DayContext(ctx context.Context, cfg *config.Config, date, zone string) (DayProjection, error) {
	var result DayProjection
	start, err := time.Parse(time.DateOnly, date)
	if err != nil || start.Year() < 1 || start.Year() > 9998 || start.Format(time.DateOnly) != date {
		return result, invalid("date must be a real YYYY-MM-DD date in years 0001 through 9998")
	}
	if len(zone) > 128 || !eventZones[zone] {
		return result, invalid("time_zone must be a known explicit IANA identifier (maximum 128 bytes)")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return result, invalid("unknown IANA time_zone %q", zone)
	}
	start, end, err := civilDayIntervalContext(ctx, start, loc)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	events, err := list(cfg, false, MaxDaySourceEvents)
	if err != nil {
		return result, err
	}
	result = DayProjection{Date: date, TimeZone: zone, DayStart: start.Format(time.RFC3339), DayEnd: end.Format(time.RFC3339), DayStartUnix: start.Unix(), DayEndUnix: end.Unix(), Events: []Event{}}
	for _, event := range events {
		matches := event.Start <= date && date < event.End
		if !event.AllDay {
			eventStart, startErr := time.Parse(time.RFC3339Nano, event.Start)
			eventEnd, endErr := time.Parse(time.RFC3339Nano, event.End)
			if startErr != nil || endErr != nil {
				return DayProjection{}, fmt.Errorf("%w: invalid timed event %s", ErrCorrupt, event.ID)
			}
			matches = eventStart.Before(end) && eventEnd.After(start)
		}
		if matches {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}

// civilDayInterval intersects every constant-offset interval with the naive civil
// date range. TZif offsets are signed 32-bit seconds, so every possible matching
// instant lies within this window, even for adversarial histories. The transition
// budget is a resource limit, not an approximation: exceeding it fails closed.
func civilDayInterval(civil time.Time, loc *time.Location) (time.Time, time.Time, error) {
	return civilDayIntervalContext(context.Background(), civil, loc)
}

func civilDayIntervalContext(ctx context.Context, civil time.Time, loc *time.Location) (time.Time, time.Time, error) {
	deadline := time.Now().Add(2 * time.Second)
	const offsetBound = time.Duration(1<<31) * time.Second
	const maxTransitions = 65536
	limit := civil.AddDate(0, 0, 1)
	cursor, stop := civil.Add(-offsetBound), limit.Add(offsetBound)
	var start, end time.Time
	found := false
	fallbackSeconds := 0
	steps := 0
	for count := 0; cursor.Before(stop); count++ {
		if steps%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return time.Time{}, time.Time{}, err
			}
			if time.Now().After(deadline) {
				return time.Time{}, time.Time{}, invalid("unsupported civil day: time-zone traversal time budget exceeded")
			}
		}
		steps++
		if count >= maxTransitions {
			return time.Time{}, time.Time{}, invalid("unsupported civil day: too many time-zone transitions")
		}
		local := cursor.In(loc)
		_, offset := local.Zone()
		if int64(offset) < -1<<31 || int64(offset) > 1<<31-1 {
			return time.Time{}, time.Time{}, invalid("unsupported civil day: time-zone offset outside TZif bounds")
		}
		_, next := local.ZoneBounds()
		if next.IsZero() || next.After(stop) {
			next = stop
		}
		if !next.After(cursor) {
			// Some Go POSIX-footer bounds end early in a leap year. TZif and
			// POSIX transition times have whole-second precision, so inspecting
			// each second is exact, not a sampling approximation. Fail closed
			// if this exceptional path exceeds its explicit resource budget.
			fallbackSeconds++
			if fallbackSeconds > 4_000_000 {
				return time.Time{}, time.Time{}, invalid("unsupported civil day: time-zone bounds budget exceeded")
			}
			count-- // The separate fallback budget bounds these iterations.
			next = cursor.Add(time.Second)
		}
		lo := civil.Add(-time.Duration(offset) * time.Second)
		hi := limit.Add(-time.Duration(offset) * time.Second)
		if lo.Before(cursor) {
			lo = cursor
		}
		if hi.After(next) {
			hi = next
		}
		if lo.Before(hi) {
			if found && !lo.Equal(end) {
				return time.Time{}, time.Time{}, invalid("unsupported civil day: disconnected time-zone intervals")
			}
			if !found {
				start = lo
				found = true
			}
			end = hi
		}
		cursor = next
	}
	if !found {
		return time.Time{}, time.Time{}, invalid("nonexistent civil day in %s", loc)
	}
	start, end = start.UTC(), end.UTC()
	if start.Year() < 1 || end.Year() > 9999 {
		return time.Time{}, time.Time{}, invalid("unsupported civil day: boundaries outside RFC3339 years")
	}
	return start, end, nil
}
