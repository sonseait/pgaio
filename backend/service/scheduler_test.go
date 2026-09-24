package service

import (
	"testing"
	"time"
)

func TestNextWeeklyRun(t *testing.T) {
	utc := time.UTC
	now := time.Date(2026, time.September, 22, 3, 30, 0, 0, utc) // Tuesday, 10:30 GMT+7

	got := nextWeeklyRun(now, int(time.Sunday), 2)
	want := time.Date(2026, time.September, 27, 2, 0, 0, 0, time.FixedZone("UTC+7", 7*60*60))
	if !got.Equal(want) {
		t.Fatalf("nextWeeklyRun() = %s, want %s", got, want)
	}

	got = nextWeeklyRun(time.Date(2026, time.September, 27, 3, 0, 0, 0, utc), int(time.Sunday), 2)
	want = time.Date(2026, time.October, 4, 2, 0, 0, 0, time.FixedZone("UTC+7", 7*60*60))
	if !got.Equal(want) {
		t.Fatalf("nextWeeklyRun() after slot = %s, want %s", got, want)
	}
}

func TestNextWeeklyRunUsesGMTPlus7(t *testing.T) {
	now := time.Date(2026, time.September, 25, 20, 0, 0, 0, time.UTC) // Saturday 03:00 GMT+7
	got := nextWeeklyRun(now, int(time.Sunday), 4)
	want := time.Date(2026, time.September, 27, 4, 0, 0, 0, time.FixedZone("UTC+7", 7*60*60))
	if !got.Equal(want) {
		t.Fatalf("nextWeeklyRun() = %s, want %s", got, want)
	}
}
