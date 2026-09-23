package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPillLineUsesTheOpenTurn(t *testing.T) {
	left := 100.0
	state := pillState{Today: 90 * time.Second, Turn: 4900 * time.Millisecond, Turns: 2, Plan: "Pro", Left: &left}
	if got := state.line(); got != "Time 4.9s this turn · Pro · 100% left" {
		t.Fatal(got)
	}
	state.Turn = 0
	if got := state.line(); got != "Time 1m 30s today · Pro · 100% left" {
		t.Fatal(got)
	}
}

func TestReadPillSumsTodayAndKeepsAnOpenTurn(t *testing.T) {
	home := t.TempDir()
	day := time.Date(2026, 9, 23, 1, 10, 0, 0, time.Local)
	dir := filepath.Join(home, "sessions", "2026", "09", "23")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "" +
		`{"timestamp":"2026-09-23T05:09:00Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"done","duration_ms":12000}}` + "\n" +
		`{"timestamp":"2026-09-23T05:10:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"open","started_at":` + strconv.FormatInt(day.Add(-5*time.Second).Unix(), 10) + `}}` + "\n" +
		`{"timestamp":"2026-09-23T05:10:00Z","type":"event_msg","payload":{"type":"token_count","info":{"rate_limits":{"plan_type":"pro","primary":{"used_percent":25}}}}}` + "\n" +
		`{"timestamp":"2026-09-22T05:10:00Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"old","duration_ms":999000}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	state := readPill(home, day)
	if state.Turns != 1 || state.Today != 12*time.Second {
		t.Fatalf("today %+v", state)
	}
	if state.Turn < 4*time.Second || state.Turn > 6*time.Second {
		t.Fatal(state.Turn)
	}
	if state.Plan != "Pro" || state.Left == nil || *state.Left != 75 {
		t.Fatalf("plan %+v", state)
	}
}
