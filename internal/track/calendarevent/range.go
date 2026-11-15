package calendarevent

import (
	"context"
	"time"

	"github.com/ttak0422/track/internal/track/config"
)

const MaxRangeDays = 42
const MaxRangePreviews = 3

// EventPreview deliberately omits event bodies, URLs and scheduling details.
type EventPreview struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Title    string `json:"title"`
	Status   string `json:"status"`
}

type RangeDay struct {
	Date     string         `json:"date"`
	Count    int            `json:"count"`
	Previews []EventPreview `json:"previews"`
}

type RangeProjection struct {
	Since    string     `json:"since"`
	Until    string     `json:"until"`
	TimeZone string     `json:"time_zone"`
	Days     []RangeDay `json:"days"`
}

// RangeContext projects one bounded source snapshot, never repairing storage.
// Every civil day must be supported by the same interval contract as DayContext.
func RangeContext(ctx context.Context, cfg *config.Config, since, until, zone string) (RangeProjection, error) {
	fail := RangeProjection{}
	first, err := time.Parse(time.DateOnly, since)
	if err != nil || first.Year() < 1 || first.Year() > 9998 || first.Format(time.DateOnly) != since {
		return fail, invalid("since must be a real YYYY-MM-DD date in years 0001 through 9998")
	}
	last, err := time.Parse(time.DateOnly, until)
	if err != nil || last.Year() < 1 || last.Year() > 9998 || last.Format(time.DateOnly) != until {
		return fail, invalid("until must be a real YYYY-MM-DD date in years 0001 through 9998")
	}
	if last.Before(first) || !last.Before(first.AddDate(0, 0, MaxRangeDays)) {
		return fail, invalid("range must contain 1 through 42 inclusive civil days")
	}
	if len(zone) > 128 || !eventZones[zone] {
		return fail, invalid("time_zone must be a known explicit IANA identifier (maximum 128 bytes)")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return fail, invalid("unknown IANA time_zone %q", zone)
	}
	// Bound total work as well as each existing civil-day traversal.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	events, err := listContext(ctx, cfg, false, MaxDaySourceEvents)
	if err != nil {
		return fail, err
	}
	dayCount := int(last.Sub(first).Hours()/24) + 1
	starts, ends, err := civilDaysIntervalContext(ctx, first, loc, dayCount)
	if err != nil {
		return fail, err
	}
	result := RangeProjection{Since: since, Until: until, TimeZone: zone, Days: []RangeDay{}}
	for i := 0; i < dayCount; i++ {
		civil := first.AddDate(0, 0, i)
		start, end := starts[i], ends[i]
		day := RangeDay{Date: civil.Format(time.DateOnly), Previews: []EventPreview{}}
		for _, event := range events {
			if err := ctx.Err(); err != nil {
				return fail, err
			}
			matches := event.Start <= day.Date && day.Date < event.End
			if !event.AllDay {
				eventStart, _ := time.Parse(time.RFC3339Nano, event.Start)
				eventEnd, _ := time.Parse(time.RFC3339Nano, event.End)
				// listContext validated all stored fields before returning the snapshot.
				matches = eventStart.Before(end) && eventEnd.After(start)
			}
			if matches {
				day.Count++
				if len(day.Previews) < MaxRangePreviews {
					day.Previews = append(day.Previews, EventPreview{ID: event.ID, Revision: event.Revision, Title: event.Title, Status: event.Status})
				}
			}
		}
		result.Days = append(result.Days, day)
	}
	if err := ctx.Err(); err != nil {
		return fail, err
	}
	return result, nil
}
