package store

import (
	"testing"
	"time"

	"github.com/rursache/always-green/internal/schedule"
)

func TestEligibleRejectsInvalidTokens(t *testing.T) {
	now := time.Now()
	ws := Workspace{}
	if !ws.Eligible(now, "UTC") {
		t.Fatal("a plain workspace with no schedule should be eligible")
	}
	ws.TokenInvalid = true
	if ws.Eligible(now, "UTC") {
		t.Fatal("expired tokens must not be scheduled")
	}
}

func TestKeepOnlineDoesNotOverrideInvalidTokens(t *testing.T) {
	now := time.Now()
	ws := Workspace{
		TokenInvalid:    true,
		KeepOnlineUntil: now.Add(time.Hour).UTC().Format(time.RFC3339),
	}
	if ws.Eligible(now, "UTC") {
		t.Fatal("stay-online must not resurrect a workspace with dead tokens")
	}
}

func TestEligibleStillHonoursSchedule(t *testing.T) {
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 31, 3, 0, 0, 0, loc) // Monday 03:00
	ws := Workspace{Schedule: &schedule.Window{
		ActiveDays: []string{"monday"},
		StartTime:  "09:00",
		EndTime:    "17:00",
	}}
	if ws.Eligible(now, "UTC") {
		t.Fatal("03:00 is outside a 09:00-17:00 window")
	}
}
