package contextbuilder

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"
)

func TestLocalLecoreAbstainsWhenNothingMatches(t *testing.T) {
	current := NewBuilder()
	if err := addUser(current, "Inspect the workspace."); err != nil {
		t.Fatal(err)
	}
	current.Commit()
	current.AddModelResponse(llm.Response{Output: []llm.Item{
		{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "noise", Name: "bash", Arguments: `{"command":"cat a"}`}},
	}})
	current.AddToolResult("noise", textResult(strings.Repeat("lorem ipsum dolor sit amet ", 800)), false)
	current.Commit()
	if err := addUser(current, "Where was zephyr-token-8841 stored?"); err != nil {
		t.Fatal(err)
	}
	result, err := current.Build()
	if err != nil {
		t.Fatal(err)
	}
	got := itemsText(result.Request.Input)
	if strings.Contains(got, "lorem ipsum") {
		t.Fatal("abstain copied unmatched history back into the request")
	}
	if !strings.Contains(got, "zephyr-token-8841") {
		t.Fatal("abstain dropped the current question")
	}
	if len(result.Report.Changes) == 0 || result.Report.Changes[0].Kind != ChangeCompacted {
		t.Fatalf("report = %#v", result.Report.Changes)
	}
}

func TestLocalLecoreLeavesShortContextUntouched(t *testing.T) {
	current := NewBuilder()
	if err := addUser(current, "hello"); err != nil {
		t.Fatal(err)
	}
	result, err := current.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Report.Changes) != 0 {
		t.Fatalf("report = %#v", result.Report.Changes)
	}
	if !strings.Contains(itemsText(result.Request.Input), "hello") {
		t.Fatalf("input lost the message: %#v", result.Request.Input)
	}
}

func TestLocalLecoreRecallsTheMatchingSlice(t *testing.T) {
	const planted = "zephyr-token-8841 lives in the north vault"
	current := NewBuilder()
	if err := addUser(current, "Inspect the workspace."); err != nil {
		t.Fatal(err)
	}
	current.Commit()
	current.AddModelResponse(llm.Response{Output: []llm.Item{
		{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "noise-1", Name: "bash", Arguments: `{"command":"cat a"}`}},
		{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "planted", Name: "bash", Arguments: `{"command":"cat secret"}`}},
		{Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "noise-2", Name: "bash", Arguments: `{"command":"cat b"}`}},
	}})
	current.AddToolResult("noise-1", textResult(strings.Repeat("lorem ipsum dolor sit amet ", 400)), false)
	current.AddToolResult("planted", textResult(planted+"\n"+strings.Repeat("vault record ", 200)), false)
	current.AddToolResult("noise-2", textResult(strings.Repeat("unrelated filler text ", 400)), false)
	current.Commit()
	if err := addUser(current, "Where was zephyr-token-8841 stored?"); err != nil {
		t.Fatal(err)
	}

	before, err := current.Build()
	if err != nil {
		t.Fatal(err)
	}
	got := itemsText(before.Request.Input)
	if !strings.Contains(got, planted) {
		t.Fatal("recalled input dropped the planted fact")
	}
	if !strings.Contains(got, "Where was zephyr-token-8841 stored?") {
		t.Fatal("recalled input dropped the current question")
	}
	if strings.Contains(got, "lorem ipsum") || strings.Contains(got, "unrelated filler") {
		t.Fatal("recalled input re-sent unrelated history")
	}
	if !strings.Contains(got, "cat secret") {
		t.Fatal("tool call was split from the result it produced")
	}
	if len(before.Report.Changes) == 0 || before.Report.Changes[0].Kind != ChangeCompacted {
		t.Fatalf("report = %#v", before.Report.Changes)
	}
	full := itemsChars(before.Request.Input)
	if full >= lecoreMinChars {
		t.Fatalf("recalled body = %d chars, want it under the spill line", full)
	}

	current.Commit()
	if err := addUser(current, "Repeat where zephyr-token-8841 was stored."); err != nil {
		t.Fatal(err)
	}
	after, err := current.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(itemsText(after.Request.Input), planted) {
		t.Fatal("a later turn could not recall the fact, so the builder must have deleted history")
	}
}

func TestFinishedReadBindsAsyncAndDedupes(t *testing.T) {
	body := strings.Repeat("alpha-file-contents ", 50)
	current := NewBuilder().(*builder)
	current.AddToolResult("call-1", textResult(body), true)
	if len(current.boundChunks()) != 0 {
		t.Fatal("a running placeholder was bound")
	}
	current.AddToolResult("call-1", textResult(body), false)
	current.AddToolResult("call-2", textResult(body), false)
	current.waitBinds()
	chunks := current.boundChunks()
	if len(chunks) != 1 {
		t.Fatalf("bound chunks = %d, want 1", len(chunks))
	}
	if chunks[0].CallID != "call-1" || chunks[0].Hash == "" || !strings.Contains(chunks[0].Text, "alpha-file-contents") {
		t.Fatalf("chunk = %#v", chunks[0])
	}
}

func addUser(current Builder, text string) error {
	payload, err := json.Marshal(text)
	if err != nil {
		return err
	}
	return current.AddExternalInput(inbox.Input{
		ID: inbox.ID(text), Kind: inbox.InputExternal, Payload: payload,
	})
}

func textResult(value string) []llm.ToolResultOutput {
	return []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: value}}
}
