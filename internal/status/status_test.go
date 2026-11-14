package status

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadLive(t *testing.T) {
	path := write(t, `{
		"mode": "live",
		"armed": true,
		"kill": false,
		"per_order_cap_cents": 5000,
		"daily_cap_cents": 15000,
		"daily_spent_cents": 1250,
		"daily_remaining_cents": 13750,
		"judge_threshold": 0.62,
		"updated_at": "2026-11-14T09:12:04Z"
	}`)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Available || s.Mode != ModeLive || !s.Live() {
		t.Fatalf("snapshot = %+v, want an available live reading", s)
	}
	if !s.Armed || s.Kill {
		t.Errorf("flags = armed %v kill %v, want armed only", s.Armed, s.Kill)
	}
	if s.PerOrderCapCents != 5000 || s.DailyCapCents != 15000 ||
		s.DailySpentCents != 1250 || s.DailyRemainingCents != 13750 {
		t.Errorf("caps = %+v, want the file's cents verbatim", s)
	}
	if s.JudgeThreshold != 0.62 {
		t.Errorf("judge threshold = %v, want 0.62", s.JudgeThreshold)
	}
	want := time.Date(2026, 11, 14, 9, 12, 4, 0, time.UTC)
	if !s.UpdatedAt.Equal(want) {
		t.Errorf("updated_at = %v, want %v", s.UpdatedAt, want)
	}
}

func TestLoadPaper(t *testing.T) {
	path := write(t, `{"mode":"paper","armed":false,"kill":true,"daily_cap_cents":900,"daily_remaining_cents":700}`)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Mode != ModePaper || s.Live() {
		t.Errorf("snapshot = %+v, want a paper reading that is not live", s)
	}
	if s.Armed || !s.Kill {
		t.Errorf("flags = armed %v kill %v, want killed only", s.Armed, s.Kill)
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")

	_, err := Load(path)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load error = %v, want a not-exist error", err)
	}
}

func TestLoadRejectsUnusableFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"truncated json", `{"mode":"live","armed":`},
		{"not json", "live, paper\n"},
		{"empty file", ""},
		{"no mode", `{"armed":true}`},
		{"unknown mode", `{"mode":"turbo","armed":true}`},
		{"caps as text", `{"mode":"live","daily_cap_cents":"15000"}`},
		{"trailing garbage", `{"mode":"live"} {"mode":"live"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := write(t, tc.body)
			if s, err := Load(path); err == nil {
				t.Fatalf("Load = %+v, want an error so the console falls back", s)
			}
		})
	}
}

func TestLoadToleratesMissingTimestampAndExtraKeys(t *testing.T) {
	path := write(t, `{"mode":"paper","daily_cap_cents":100,"updated_at":"yesterday","added_by_a_newer_engine":true}`)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.UpdatedAt.IsZero() {
		t.Errorf("updated_at = %v, want zero for an unparsable timestamp", s.UpdatedAt)
	}
	if s.Mode != ModePaper {
		t.Errorf("mode = %q, want paper", s.Mode)
	}
}
