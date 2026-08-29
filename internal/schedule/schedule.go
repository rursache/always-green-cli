package schedule

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var DayIDs = []string{
	"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
}

var DayShort = map[string]string{
	"monday": "Mon", "tuesday": "Tue", "wednesday": "Wed",
	"thursday": "Thu", "friday": "Fri", "saturday": "Sat", "sunday": "Sun",
}

var weekdays = map[string]struct{}{
	"monday": {}, "tuesday": {}, "wednesday": {}, "thursday": {}, "friday": {},
}

type Window struct {
	ActiveDays []string `json:"active_days,omitempty"`
	StartTime  string   `json:"start_time,omitempty"`
	EndTime    string   `json:"end_time,omitempty"`
	Timezone   string   `json:"timezone,omitempty"`
}

func ParseClock(text string) (hour, minute int, ok bool) {
	raw := strings.TrimSpace(strings.ToLower(text))
	if raw == "" {
		return 0, 0, false
	}

	pm := strings.HasSuffix(raw, "pm") || strings.HasSuffix(raw, "p")
	am := strings.HasSuffix(raw, "am") || strings.HasSuffix(raw, "a")
	switch {
	case strings.HasSuffix(raw, "pm"), strings.HasSuffix(raw, "am"):
		raw = strings.TrimSpace(raw[:len(raw)-2])
	case strings.HasSuffix(raw, "p"), strings.HasSuffix(raw, "a"):
		raw = strings.TrimSpace(raw[:len(raw)-1])
	}

	var h, m int
	var err error
	if strings.Contains(raw, ":") {
		bits := strings.Split(raw, ":")
		if len(bits) != 2 {
			return 0, 0, false
		}
		h, err = strconv.Atoi(bits[0])
		if err != nil {
			return 0, 0, false
		}
		m, err = strconv.Atoi(bits[1])
		if err != nil {
			return 0, 0, false
		}
	} else {
		h, err = strconv.Atoi(raw)
		if err != nil {
			return 0, 0, false
		}
	}

	if pm && h < 12 {
		h += 12
	} else if am && h == 12 {
		h = 0
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func Clock12h(hour, minute int) string {
	suffix := "AM"
	if hour >= 12 {
		suffix = "PM"
	}
	h12 := hour % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d %s", h12, minute, suffix)
}

func Clock24h(hour, minute int) string {
	return fmt.Sprintf("%02d:%02d", hour, minute)
}

func NudgeClock(text string, deltaMinutes int) string {
	h, m, ok := ParseClock(text)
	if !ok {
		h, m = 9, 0
	}
	total := (h*60 + m + deltaMinutes) % (24 * 60)
	if total < 0 {
		total += 24 * 60
	}
	return Clock12h(total/60, total%60)
}

func DetectTimezone() string {
	name, err := time.Now().Zone()
	_ = name
	if loc := time.Local; loc != nil && loc != time.UTC {
		if loc.String() != "Local" {
			return loc.String()
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			tz := link[i+len("zoneinfo/"):]
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
	}
	_ = err
	return "UTC"
}

func InWindow(w *Window, now time.Time, tzName string) bool {
	if w == nil || (len(w.ActiveDays) == 0 && w.StartTime == "" && w.EndTime == "") {
		return true
	}
	if tzName == "" {
		tzName = w.Timezone
	}
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	now = now.In(loc)

	start := w.StartTime
	end := w.EndTime
	if start == "" {
		start = "00:00"
	}
	if end == "" {
		end = "23:59"
	}
	sh, sm, ok1 := ParseClock(start)
	eh, em, ok2 := ParseClock(end)
	if !ok1 || !ok2 {
		return dayAllowed(w.ActiveDays, now.Weekday())
	}
	cur := now.Hour()*60 + now.Minute()
	startM := sh*60 + sm
	endM := eh*60 + em
	if endM < startM {
		if cur >= startM {
			return dayAllowed(w.ActiveDays, now.Weekday())
		}
		if cur < endM {
			return dayAllowed(w.ActiveDays, now.AddDate(0, 0, -1).Weekday())
		}
		return false
	}
	if !dayAllowed(w.ActiveDays, now.Weekday()) {
		return false
	}
	return cur >= startM && cur < endM
}

func dayAllowed(days []string, wd time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	name := strings.ToLower(wd.String())
	for _, d := range days {
		if d == name {
			return true
		}
	}
	return false
}

func Format(w *Window) string {
	if w == nil || (len(w.ActiveDays) == 0 && w.StartTime == "" && w.EndTime == "") {
		return "Always on"
	}
	start := w.StartTime
	end := w.EndTime
	if start == "" {
		start = "09:00"
	}
	if end == "" {
		end = "17:00"
	}
	days := w.ActiveDays
	if len(days) == 0 {
		return "Always on"
	}
	set := map[string]struct{}{}
	for _, d := range days {
		set[d] = struct{}{}
	}
	label := ""
	switch {
	case len(set) == 7:
		label = "every day"
	case sameDaySet(set, weekdays):
		label = "Mon-Fri"
	case len(set) == 2 && has(set, "saturday") && has(set, "sunday"):
		label = "Sat-Sun"
	default:
		var parts []string
		for _, id := range DayIDs {
			if _, ok := set[id]; ok {
				parts = append(parts, DayShort[id])
			}
		}
		label = strings.Join(parts, ", ")
	}
	return fmt.Sprintf("%s to %s, %s", start, end, label)
}

func sameDaySet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func has(set map[string]struct{}, k string) bool {
	_, ok := set[k]
	return ok
}
