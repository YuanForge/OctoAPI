package handler

import (
	"strconv"
	"time"
)

const creditsPerCNY = 1_000_000.0

func creditsToCNY(credits int64) float64 {
	return float64(credits) / creditsPerCNY
}

func shanghaiDayRange(t time.Time) (time.Time, time.Time) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	local := t.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 0, 1)
}

func parseDateTime(value string, endOfDay bool) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	localLayouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"}
	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			if layout == "2006-01-02" && endOfDay {
				return t.Add(24*time.Hour - time.Nanosecond), nil
			}
			return t, nil
		}
	}
	return time.Time{}, strconv.ErrSyntax
}
