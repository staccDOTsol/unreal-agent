package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavingsLine(t *testing.T) {
	state := pillState{Spent: 480.075457, Direct: 1632.661519, Calls: 1994, OK: true}
	if got := state.line(); got != "Saved $1,152.59 · 70.6%" {
		t.Fatal(got)
	}
	if got := (pillState{}).line(); got != "Saved —" {
		t.Fatal(got)
	}
	if got := (pillState{Spent: 5, Direct: 4, Calls: 1, OK: true}).line(); got != "Extra $1.00 · 25.0%" {
		t.Fatal(got)
	}
}

func TestReadSavingsPrefersTheVerifiedComparison(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "session.json")
	body := `{"spentUsd":1,"directUsd":2,"paidCalls":3,"comparison":{"spentUsd":10,"directUsd":40,"calls":4}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	state := readSavings(home, path, "")
	if state.Spent != 10 || state.Direct != 40 || state.Calls != 4 || state.line() != "Saved $30.00 · 75.0%" {
		t.Fatalf("%+v %s", state, state.line())
	}
}

func TestPriceFollowsTheSelectedModel(t *testing.T) {
	astra := modelCard{ID: "openai/gpt-6-astra", Base: tokenRates{Prompt: 0.00001, Completion: 0.00005, CacheRead: 0.000001, CacheWrite: 0.0000125}}
	pro := modelCard{ID: "openai/gpt-6-astra-pro", Base: tokenRates{Prompt: 0.00002, Completion: 0.00005, CacheRead: 0.000002, CacheWrite: 0.000025}}
	sol := modelCard{ID: "openai/gpt-6-sol", Base: tokenRates{Prompt: 0.000004, Completion: 0.00002, CacheRead: 0.0000004, CacheWrite: 0.000005}, Overrides: []struct {
		Min   int
		Rates tokenRates
	}{{Min: 272000, Rates: tokenRates{Prompt: 0.000008, Completion: 0.00003, CacheRead: 0.0000008, CacheWrite: 0.00001}}}}
	catalog := map[string]modelCard{astra.ID: astra, pro.ID: pro, sol.ID: sol}
	card, ok := matchModel(catalog, "gpt-6-astra")
	if !ok || card.ID != astra.ID {
		t.Fatal(card.ID, ok)
	}
	direct, spent := priceCall(card, 1000, 800, 0, 10)
	if direct != 0.0105 || spent != 0.0033 {
		t.Fatalf("direct %v spent %v", direct, spent)
	}
	longDirect, _ := priceCall(sol, 272000, 0, 0, 0)
	if longDirect < 2.175 || longDirect > 2.177 {
		t.Fatal(longDirect)
	}
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "09", "23")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "" +
		`{"type":"turn_context","payload":{"model":"gpt-6-sol"}}` + "\n" +
		`{"type":"token_usage_record","payload":{"usage":{"input_tokens":1000,"cached_input_tokens":800,"cache_write_input_tokens":0,"output_tokens":10}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	state := priceSession(home, catalog)
	if state.Model != "gpt-6-sol" || !strings.Contains(state.line(), "OpenRouter list · gpt-6-sol") {
		t.Fatal(state.line(), state.Model)
	}
	if state.text() != state.line()+"\n… 800 cached · 1,000 in · 10 out" {
		t.Fatal(state.text())
	}
	if !strings.Contains(string(state.Receipt), `"openrouter":"openai/gpt-6-sol"`) || !strings.Contains(string(state.Receipt), `"price":"openrouter-list"`) {
		t.Fatal(string(state.Receipt))
	}
}

func TestReadSavingsPrefersTheLiveRunner(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "session.json")
	if err := os.WriteFile(path, []byte(`{"comparison":{"spentUsd":1,"directUsd":2,"calls":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/info" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"comparison":{"spentUsd":8,"directUsd":20,"calls":2}}`))
	}))
	defer srv.Close()
	state := readSavings(home, path, srv.URL+"/v1/info")
	if state.line() != "Saved $12.00 · 60.0%" {
		t.Fatal(state.line())
	}
}
