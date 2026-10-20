package audit

import (
	"testing"
	"time"
)

func utc(h, m int) time.Time { return time.Date(2026, 10, 19, h, m, 0, 0, time.UTC) }

func TestNextFire(t *testing.T) {
	got, err := NextFire("0 * * * *", utc(10, 30))
	if err != nil || !got.Equal(utc(11, 0)) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestNextFireInvalid(t *testing.T) {
	for _, s := range []string{"* * *", "@every 1h", "@hourly", "0 0 * * * *", "61 * * * *", ""} {
		if _, err := NextFire(s, utc(10, 0)); err == nil {
			t.Errorf("%q: expected error", s)
		}
	}
}

func TestMostRecentFire(t *testing.T) {
	got, ok, err := MostRecentFire("*/5 * * * *", utc(10, 0), utc(10, 23))
	if err != nil || !ok || !got.Equal(utc(10, 20)) {
		t.Fatalf("got %v %v %v", got, ok, err)
	}
}

func TestMostRecentFireAfterDowntime(t *testing.T) {
	now := utc(10, 30).Add(72 * time.Hour)
	got, ok, err := MostRecentFire("0 * * * *", utc(10, 0), now)
	if err != nil || !ok || !got.Equal(now.Truncate(time.Hour)) {
		t.Fatalf("got %v %v %v", got, ok, err)
	}
}

func TestMostRecentFireNone(t *testing.T) {
	_, ok, err := MostRecentFire("*/5 * * * *", utc(10, 0), utc(10, 0))
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestMostRecentFireInvalidAndCap(t *testing.T) {
	if _, _, err := MostRecentFire("@every 1h", utc(10, 0), utc(11, 0)); err == nil {
		t.Error("expected schedule error")
	}
	// every minute over ~100 days exceeds the 100000-iteration cap
	if _, _, err := MostRecentFire("* * * * *", utc(0, 0), utc(0, 0).AddDate(0, 0, 100)); err == nil {
		t.Error("expected cap error")
	}
}
