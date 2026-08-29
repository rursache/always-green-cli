package schedule

import (
	"testing"
	"time"
)

func TestParseClock(t *testing.T) {
	cases := []struct {
		in      string
		h, m    int
		wantOK  bool
	}{
		{"9am", 9, 0, true},
		{"9:30", 9, 30, true},
		{"14:00", 14, 0, true},
		{"2pm", 14, 0, true},
		{"12am", 0, 0, true},
		{"12pm", 12, 0, true},
		{"9", 9, 0, true},
		{"25:00", 0, 0, false},
		{"nope", 0, 0, false},
	}
	for _, tc := range cases {
		h, m, ok := ParseClock(tc.in)
		if ok != tc.wantOK || (ok && (h != tc.h || m != tc.m)) {
			t.Fatalf("%q -> %d:%d ok=%v, want %d:%d ok=%v", tc.in, h, m, ok, tc.h, tc.m, tc.wantOK)
		}
	}
}

func TestInWindowWeekdays(t *testing.T) {
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatal(err)
	}
	w := &Window{
		ActiveDays: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime:  "09:00",
		EndTime:    "17:00",
		Timezone:   "UTC",
	}
	// Monday 10:00 UTC (2026-08-31 is a Monday)
	mon := time.Date(2026, 8, 31, 10, 0, 0, 0, loc)
	if !InWindow(w, mon, "UTC") {
		t.Fatal("expected Monday 10:00 inside window")
	}
	sat := time.Date(2026, 8, 29, 10, 0, 0, 0, loc)
	if InWindow(w, sat, "UTC") {
		t.Fatal("expected Saturday outside window")
	}
	early := time.Date(2026, 8, 31, 8, 0, 0, 0, loc)
	if InWindow(w, early, "UTC") {
		t.Fatal("expected 08:00 outside window")
	}
}

func TestInWindowOvernight(t *testing.T) {
	loc := time.UTC
	w := &Window{
		ActiveDays: []string{"monday"},
		StartTime:  "22:00",
		EndTime:    "06:00",
		Timezone:   "UTC",
	}
	late := time.Date(2026, 8, 31, 23, 0, 0, 0, loc)
	if !InWindow(w, late, "UTC") {
		t.Fatal("expected 23:00 inside overnight window")
	}
	mid := time.Date(2026, 8, 31, 12, 0, 0, 0, loc)
	if InWindow(w, mid, "UTC") {
		t.Fatal("expected noon outside overnight window")
	}
}

func TestFormat(t *testing.T) {
	if Format(nil) != "Always on" {
		t.Fatal(Format(nil))
	}
	got := Format(&Window{
		ActiveDays: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime:  "09:00",
		EndTime:    "17:00",
	})
	if got != "09:00 to 17:00, Mon-Fri" {
		t.Fatalf("got %q", got)
	}
}

func TestNudgeClock(t *testing.T) {
	if got := NudgeClock("9:00 AM", 30); got != "9:30 AM" {
		t.Fatalf("got %q", got)
	}
	if got := NudgeClock("11:45 PM", 30); got != "12:15 AM" {
		t.Fatalf("got %q", got)
	}
}
