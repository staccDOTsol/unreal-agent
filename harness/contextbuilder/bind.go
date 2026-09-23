package contextbuilder

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// boundChunk is one finished read, content-addressed. The same bytes bind
// once. A later read of those bytes keeps the first call as provenance.
type boundChunk struct {
	Hash   string
	CallID string
	Text   string
}

// scheduleBind hashes and stores a finished tool result off the caller
// goroutine. A still-running placeholder is not a body, so AddToolResult
// does not call this while running is set. The model turn that just received
// the read does not wait; the next recall does.
func (current *builder) scheduleBind(callID string, payload []llm.ToolResultOutput) {
	text := strings.TrimSpace(resultText(payload))
	if text == "" || text == ToolCallRunningPayload {
		return
	}
	current.mu.Lock()
	if current.seen == nil {
		current.seen = map[string]string{}
	}
	if _, exists := current.seen[text]; exists {
		current.mu.Unlock()
		return
	}
	// Claim on the caller goroutine so the first read keeps provenance.
	// The hash itself runs afterward and does not block the turn.
	current.seen[text] = callID
	current.mu.Unlock()

	current.binds.Add(1)
	go func() {
		defer current.binds.Done()
		sum := sha256.Sum256([]byte(text))
		hash := hex.EncodeToString(sum[:])
		current.mu.Lock()
		defer current.mu.Unlock()
		if current.bound == nil {
			current.bound = map[string]boundChunk{}
		}
		current.bound[hash] = boundChunk{Hash: hash, CallID: callID, Text: text}
	}()
}

func (current *builder) waitBinds() {
	current.binds.Wait()
}

func (current *builder) boundChunks() []boundChunk {
	current.mu.Lock()
	defer current.mu.Unlock()
	out := make([]boundChunk, 0, len(current.bound))
	for _, chunk := range current.bound {
		out = append(out, chunk)
	}
	return out
}

func resultText(payload []llm.ToolResultOutput) string {
	var b strings.Builder
	for _, output := range payload {
		if output.Kind != llm.ToolResultText {
			continue
		}
		b.WriteString(output.Value)
		b.WriteByte('\n')
	}
	return b.String()
}
