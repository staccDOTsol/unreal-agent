package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Savings against OpenRouter's posted list price for the same tokens.
// Direct is the list price with nothing read from cache. Spent applies
// OpenRouter's cache-read and cache-write rates. This is a price card,
// not a charge.
type pillState struct {
	Spent   float64
	Direct  float64
	Calls   int
	Input   int
	Cached  int
	Output  int
	Model   string
	OK      bool
	Priced  bool
	Receipt []byte
}

func (state pillState) line() string {
	if !state.OK || state.Direct <= 0 {
		return "Saved —"
	}
	saved := state.Direct - state.Spent
	pct := 100 * saved / state.Direct
	head := fmt.Sprintf("Saved %s · %.1f%%", money(saved), pct)
	if saved < 0 {
		head = fmt.Sprintf("Extra %s · %.1f%%", money(-saved), -pct)
	}
	if state.Priced {
		head += " · OpenRouter list"
		if state.Model != "" {
			head += " · " + state.Model
		}
	}
	return head
}

func (state pillState) text() string {
	detail := "…"
	if state.OK {
		detail = fmt.Sprintf("… %s cached · %s in · %s out", commas(state.Cached), commas(state.Input), commas(state.Output))
	}
	return state.line() + "\n" + detail
}

func money(n float64) string {
	if n < 0 {
		n = -n
	}
	digits := 2
	if n > 0 && n < 0.01 {
		digits = 4
	}
	raw := strconv.FormatFloat(n, 'f', digits, 64)
	whole, frac, _ := strings.Cut(raw, ".")
	var grouped strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(c)
	}
	if frac == "" {
		return "$" + grouped.String()
	}
	return "$" + grouped.String() + "." + frac
}

func readSavings(home, path, infoURL string) pillState {
	if path == "" && infoURL == "" {
		return pillState{}
	}
	if infoURL != "" {
		if state, ok := readSavingsURL(infoURL); ok {
			return state
		}
	}
	if path == "" {
		return pillState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return pillState{}
	}
	return savingsFrom(data)
}

func readSavingsURL(raw string) (pillState, bool) {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(raw)
	if err != nil {
		return pillState{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pillState{}, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return pillState{}, false
	}
	state := savingsFrom(data)
	return state, state.OK
}

func savingsFrom(data []byte) pillState {
	var file struct {
		SpentUsd   float64 `json:"spentUsd"`
		DirectUsd  float64 `json:"directUsd"`
		PaidCalls  int     `json:"paidCalls"`
		Comparison struct {
			SpentUsd  float64 `json:"spentUsd"`
			DirectUsd float64 `json:"directUsd"`
			Calls     int     `json:"calls"`
		} `json:"comparison"`
	}
	if json.Unmarshal(data, &file) != nil {
		return pillState{}
	}
	if file.Comparison.Calls > 0 && file.Comparison.DirectUsd > 0 {
		return pillState{Spent: file.Comparison.SpentUsd, Direct: file.Comparison.DirectUsd, Calls: file.Comparison.Calls, OK: true}
	}
	if file.DirectUsd > 0 && (file.SpentUsd > 0 || file.PaidCalls > 0) {
		return pillState{Spent: file.SpentUsd, Direct: file.DirectUsd, Calls: file.PaidCalls, OK: true}
	}
	return pillState{}
}

func commas(n int) string {
	if n < 0 {
		n = 0
	}
	raw := strconv.Itoa(n)
	var grouped strings.Builder
	for i, c := range raw {
		if i > 0 && (len(raw)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(c)
	}
	return grouped.String()
}

type tokenRates struct {
	Prompt     float64
	Completion float64
	CacheRead  float64
	CacheWrite float64
}

type modelCard struct {
	ID        string
	Base      tokenRates
	Overrides []struct {
		Min   int
		Rates tokenRates
	}
}

func (card modelCard) tier(input int) tokenRates {
	rates := card.Base
	min := -1
	for _, override := range card.Overrides {
		if input >= override.Min && override.Min >= min {
			rates = override.Rates
			min = override.Min
		}
	}
	return rates
}

func priceCall(card modelCard, input, cached, write, output int) (direct, spent float64) {
	if input < 0 {
		input = 0
	}
	if cached < 0 {
		cached = 0
	}
	if cached > input {
		cached = input
	}
	if write < 0 {
		write = 0
	}
	if output < 0 {
		output = 0
	}
	uncached := input - cached
	if write > uncached {
		write = uncached
	}
	fresh := uncached - write
	rates := card.tier(input)
	direct = float64(input)*rates.Prompt + float64(output)*rates.Completion
	spent = float64(fresh)*rates.Prompt + float64(write)*rates.CacheWrite + float64(cached)*rates.CacheRead + float64(output)*rates.Completion
	return direct, spent
}

func matchModel(catalog map[string]modelCard, selected string) (modelCard, bool) {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return modelCard{}, false
	}
	if card, ok := catalog[selected]; ok {
		return card, true
	}
	best := ""
	for id := range catalog {
		if strings.HasSuffix(id, "/"+selected) && (best == "" || len(id) < len(best)) {
			best = id
		}
	}
	if best == "" {
		return modelCard{}, false
	}
	return catalog[best], true
}

type sessionCall struct {
	Model  string
	Input  int
	Cached int
	Write  int
	Output int
}

func readSessionCalls(path string) []sessionCall {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var calls []sessionCall
	model := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for scanner.Scan() {
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Model string `json:"model"`
				Usage *struct {
					InputTokens           int `json:"input_tokens"`
					CachedInputTokens     int `json:"cached_input_tokens"`
					CacheWriteInputTokens int `json:"cache_write_input_tokens"`
					OutputTokens          int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		if row.Type == "turn_context" && row.Payload.Model != "" {
			model = row.Payload.Model
		}
		if row.Type == "token_usage_record" && row.Payload.Usage != nil {
			calls = append(calls, sessionCall{
				Model:  model,
				Input:  row.Payload.Usage.InputTokens,
				Cached: row.Payload.Usage.CachedInputTokens,
				Write:  row.Payload.Usage.CacheWriteInputTokens,
				Output: row.Payload.Usage.OutputTokens,
			})
		}
	}
	return calls
}

func newestSession(codexHome string) string {
	var newest string
	var newestTime time.Time
	_ = filepath.WalkDir(filepath.Join(codexHome, "sessions"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest = path
			newestTime = info.ModTime()
		}
		return nil
	})
	return newest
}

func priceSession(codexHome string, catalog map[string]modelCard) pillState {
	path := newestSession(codexHome)
	if path == "" {
		return pillState{}
	}
	calls := readSessionCalls(path)
	var state pillState
	var receipts bytes.Buffer
	models := map[string]struct{}{}
	var modelOrder []string
	for _, call := range calls {
		state.Input += call.Input
		state.Cached += call.Cached
		state.Output += call.Output
		state.Calls++
		card, ok := matchModel(catalog, call.Model)
		if !ok {
			continue
		}
		direct, spent := priceCall(card, call.Input, call.Cached, call.Write, call.Output)
		state.Direct += direct
		state.Spent += spent
		state.Priced = true
		if _, seen := models[call.Model]; call.Model != "" && !seen {
			models[call.Model] = struct{}{}
			modelOrder = append(modelOrder, call.Model)
		}
		row, err := json.Marshal(struct {
			Model             string  `json:"model"`
			OpenRouter        string  `json:"openrouter"`
			InputTokens       int     `json:"inputTokens"`
			CachedInputTokens int     `json:"cachedInputTokens"`
			CacheWriteTokens  int     `json:"cacheWriteTokens"`
			OutputTokens      int     `json:"outputTokens"`
			DirectUsd         float64 `json:"directUsd"`
			SpentUsd          float64 `json:"spentUsd"`
			Price             string  `json:"price"`
		}{
			Model: call.Model, OpenRouter: card.ID,
			InputTokens: call.Input, CachedInputTokens: call.Cached, CacheWriteTokens: call.Write, OutputTokens: call.Output,
			DirectUsd: direct, SpentUsd: spent, Price: "openrouter-list",
		})
		if err == nil {
			receipts.Write(row)
			receipts.WriteByte('\n')
		}
	}
	state.Model = strings.Join(modelOrder, ", ")
	state.Receipt = receipts.Bytes()
	state.OK = state.Calls > 0
	return state
}

var (
	catalogMu   sync.Mutex
	catalogAt   time.Time
	catalogCard map[string]modelCard
)

func openRouterCatalog() map[string]modelCard {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if catalogCard != nil && time.Since(catalogAt) < 10*time.Minute {
		return catalogCard
	}
	if fetched, ok := fetchOpenRouterCatalog(); ok {
		catalogCard = fetched
		catalogAt = time.Now()
	}
	return catalogCard
}

func fetchOpenRouterCatalog() (map[string]modelCard, bool) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var body struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt          string `json:"prompt"`
				Completion      string `json:"completion"`
				InputCacheRead  string `json:"input_cache_read"`
				InputCacheWrite string `json:"input_cache_write"`
				Overrides       []struct {
					MinPromptTokens int    `json:"min_prompt_tokens"`
					Prompt          string `json:"prompt"`
					Completion      string `json:"completion"`
					InputCacheRead  string `json:"input_cache_read"`
					InputCacheWrite string `json:"input_cache_write"`
				} `json:"overrides"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&body) != nil {
		return nil, false
	}
	catalog := map[string]modelCard{}
	for _, row := range body.Data {
		if row.Pricing.Prompt == "" && row.Pricing.Completion == "" {
			continue
		}
		card := modelCard{
			ID: row.ID,
			Base: tokenRates{
				Prompt:     atof(row.Pricing.Prompt),
				Completion: atof(row.Pricing.Completion),
				CacheRead:  atof(row.Pricing.InputCacheRead),
				CacheWrite: atof(row.Pricing.InputCacheWrite),
			},
		}
		for _, override := range row.Pricing.Overrides {
			card.Overrides = append(card.Overrides, struct {
				Min   int
				Rates tokenRates
			}{Min: override.MinPromptTokens, Rates: tokenRates{
				Prompt:     atof(override.Prompt),
				Completion: atof(override.Completion),
				CacheRead:  atof(override.InputCacheRead),
				CacheWrite: atof(override.InputCacheWrite),
			}})
		}
		catalog[row.ID] = card
	}
	if len(catalog) == 0 {
		return nil, false
	}
	return catalog, true
}

func atof(raw string) float64 {
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return n
}
