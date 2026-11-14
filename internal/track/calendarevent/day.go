package calendarevent

import (
	"fmt"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

const MaxDaySourceEvents = 4096

// DayProjection preserves stored event labels. Timed membership is in UTC;
// all-day membership remains an independent, exclusive-end civil date range.
type DayProjection struct {
	Date     string  `json:"date"`
	TimeZone string  `json:"time_zone"`
	DayStart string  `json:"day_start"`
	DayEnd   string  `json:"day_end"`
	Events   []Event `json:"events"`
}

// Day reads a bounded snapshot without changing event storage.
func Day(cfg *config.Config, date, zone string) (DayProjection, error) {
	var result DayProjection
	start, err := time.Parse(time.DateOnly, date)
	if err != nil || start.Year() < 1 || start.Year() > 9998 || start.Format(time.DateOnly) != date {
		return result, invalid("date must be a real YYYY-MM-DD date in years 0001 through 9998")
	}
	if zone != "UTC" {
		return result, invalid("time_zone must be UTC")
	}
	end := start.AddDate(0, 0, 1)
	events, err := list(cfg, false, MaxDaySourceEvents)
	if err != nil {
		return result, err
	}
	result = DayProjection{Date: date, TimeZone: zone, DayStart: start.Format(time.RFC3339), DayEnd: end.Format(time.RFC3339), Events: []Event{}}
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
