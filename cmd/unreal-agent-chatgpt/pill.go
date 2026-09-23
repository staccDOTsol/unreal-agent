package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pillState struct {
	Today time.Duration
	Turn  time.Duration
	Turns int
	Plan  string
	Left  *float64
}

func (state pillState) line() string {
	clock, label := state.Turn, "this turn"
	if clock <= 0 {
		clock, label = state.Today, "today"
	}
	text := "Time " + formatClock(clock) + " " + label
	if state.Plan != "" {
		text += " · " + state.Plan
	}
	if state.Left != nil {
		text += fmt.Sprintf(" · %.0f%% left", *state.Left)
	}
	return text
}

func formatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	seconds := int(d.Round(time.Second).Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
}

func readPill(codexHome string, now time.Time) pillState {
	var state pillState
	root := filepath.Join(codexHome, "sessions")
	today := now.In(time.Local)
	open := map[string]time.Time{}
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		applySession(path, today, &state, open)
		return nil
	})
	var latest time.Time
	for _, started := range open {
		if started.After(latest) {
			latest = started
		}
	}
	if !latest.IsZero() && now.After(latest) {
		state.Turn = now.Sub(latest)
	}
	return state
}

type pillEvent struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string `json:"type"`
		TurnID     string `json:"turn_id"`
		StartedAt  int64  `json:"started_at"`
		DurationMS int64  `json:"duration_ms"`
		Info       struct {
			RateLimits pillLimits `json:"rate_limits"`
		} `json:"info"`
		RateLimits pillLimits `json:"rate_limits"`
	} `json:"payload"`
}

type pillLimits struct {
	PlanType string `json:"plan_type"`
	Primary  *struct {
		UsedPercent float64 `json:"used_percent"`
	} `json:"primary"`
}

func applySession(path string, today time.Time, state *pillState, open map[string]time.Time) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event pillEvent
			if json.Unmarshal(line, &event) == nil && event.Type == "event_msg" {
				applyPillEvent(event, today, state, open)
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
	}
}

func applyPillEvent(event pillEvent, today time.Time, state *pillState, open map[string]time.Time) {
	switch event.Payload.Type {
	case "task_started":
		if event.Payload.TurnID != "" && event.Payload.StartedAt > 0 {
			open[event.Payload.TurnID] = time.Unix(event.Payload.StartedAt, 0)
		}
	case "task_complete":
		delete(open, event.Payload.TurnID)
		when := eventTime(event)
		if when.IsZero() || !sameDay(when, today) {
			return
		}
		state.Turns++
		if event.Payload.DurationMS > 0 {
			state.Today += time.Duration(event.Payload.DurationMS) * time.Millisecond
		}
	case "token_count":
		limits := event.Payload.Info.RateLimits
		if limits.PlanType == "" {
			limits = event.Payload.RateLimits
		}
		if limits.PlanType != "" {
			state.Plan = planLabel(limits.PlanType)
		}
		if limits.Primary != nil {
			left := 100 - limits.Primary.UsedPercent
			if left < 0 {
				left = 0
			}
			state.Left = &left
		}
	}
}

func eventTime(event pillEvent) time.Time {
	if event.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, event.Timestamp); err == nil {
			return parsed
		}
	}
	if event.Payload.StartedAt > 0 {
		return time.Unix(event.Payload.StartedAt, 0)
	}
	return time.Time{}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.In(time.Local).Date()
	by, bm, bd := b.In(time.Local).Date()
	return ay == by && am == bm && ad == bd
}

func planLabel(plan string) string {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return ""
	}
	return strings.ToUpper(plan[:1]) + plan[1:]
}
