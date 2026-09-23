package contextbuilder

import (
	"math"
	"strings"
	"unicode"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// Local leCore recall. The context builder cannot do I/O, so this is the
// in-memory slice of CAPABILITIES.md that can sit in front of a model request:
// causal recall (earlier items only), retrieval_dispatch's exact-token
// short-circuit then Okapi BM25, and "answer, return the set, or abstain".
// Tool call and result are one chunk. The session keeps the full history.
// Geometry, rendering, simulation, and the HTTP service are not this job.
const (
	lecoreMinChars    = 16384
	lecoreRecallChars = 16384
	lecoreK1          = 1.5
	lecoreB           = 0.75
)

type lecoreDoc struct {
	indexes []int
	at      int
	text    string
	chars   int
}

// recallLocal copies input down to what this turn needs. committedLen is the
// first staged item. The system preamble, the staged turn, and the latest user
// message always stay, and a tool call stays with its result. Older items are
// documents. The highest-scoring ones that fit in lecoreRecallChars are copied
// back in their original order.
func recallLocal(input []llm.Item, committedLen int) ([]llm.Item, Report) {
	if itemsChars(input) <= lecoreMinChars {
		return input, Report{}
	}
	protected := protectedIndexes(input, committedLen)
	var docs []lecoreDoc
	for _, indexes := range recallDocs(input, protected) {
		items := make([]llm.Item, len(indexes))
		chars := 0
		for n, index := range indexes {
			items[n] = input[index]
			chars += itemChars(input[index])
		}
		docs = append(docs, lecoreDoc{
			indexes: indexes,
			at:      indexes[0],
			text:    strings.TrimSpace(itemsText(items)),
			chars:   chars,
		})
	}
	if len(docs) == 0 {
		return input, Report{}
	}

	chosen := chooseDocs(docs, queryText(input, protected), lecoreRecallChars)
	keep := append([]bool(nil), protected...)
	for i, doc := range docs {
		if !chosen[i] {
			continue
		}
		for _, index := range doc.indexes {
			keep[index] = true
		}
	}

	var report Report
	out := make([]llm.Item, 0, len(input))
	omitted := 0
	for i, item := range input {
		if keep[i] {
			out = append(out, item)
			continue
		}
		omitted++
		report.Changes = append(report.Changes, Change{
			Kind:   ChangeOmitted,
			Source: itemSource(item),
			Reason: "local lecore recall left this earlier item out of the model request",
		})
	}
	if omitted == 0 {
		return input, Report{}
	}
	report.Changes = append([]Change{{
		Kind:   ChangeCompacted,
		Source: "local-lecore",
		Reason: "earlier turns were recalled with in-memory BM25 instead of re-sent whole",
	}}, report.Changes...)
	return out, report
}

// recallDocs groups earlier items that have to travel together. A tool call
// and every result with its id are one document even when other calls sit
// between them. A reasoning item stays with the message that follows it.
func recallDocs(input []llm.Item, protected []bool) [][]int {
	resultsByCall := map[string][]int{}
	for i, item := range input {
		if item.Type == llm.ItemToolResult {
			if id := toolResultID(item); id != "" {
				resultsByCall[id] = append(resultsByCall[id], i)
			}
		}
	}
	used := make([]bool, len(input))
	var docs [][]int
	for i, item := range input {
		if used[i] || protected[i] {
			continue
		}
		switch item.Type {
		case llm.ItemToolCall:
			indexes := []int{i}
			used[i] = true
			for _, result := range resultsByCall[toolCallID(item)] {
				if !used[result] && !protected[result] {
					indexes = append(indexes, result)
					used[result] = true
				}
			}
			docs = append(docs, indexes)
		case llm.ItemToolResult:
			docs = append(docs, []int{i})
			used[i] = true
		case llm.ItemReasoning:
			indexes := []int{i}
			used[i] = true
			if i+1 < len(input) && !protected[i+1] && input[i+1].Type == llm.ItemMessage {
				indexes = append(indexes, i+1)
				used[i+1] = true
			}
			docs = append(docs, indexes)
		default:
			docs = append(docs, []int{i})
			used[i] = true
		}
	}
	return docs
}

func protectedIndexes(input []llm.Item, committedLen int) []bool {
	protected := make([]bool, len(input))
	if len(input) > 0 && isSystem(input[0]) {
		protected[0] = true
	}
	if committedLen < 0 {
		committedLen = 0
	}
	if committedLen > len(input) {
		committedLen = len(input)
	}
	for i := committedLen; i < len(input); i++ {
		protected[i] = true
	}
	for i := len(input) - 1; i >= 0; i-- {
		message, ok := input[i].Data.(llm.Message)
		if ok && message.Role == llm.RoleUser {
			protected[i] = true
			break
		}
	}
	calls := map[string]int{}
	for i, item := range input {
		if item.Type == llm.ItemToolCall {
			if id := toolCallID(item); id != "" {
				calls[id] = i
			}
		}
	}
	for i, item := range input {
		if item.Type != llm.ItemToolResult {
			continue
		}
		call, ok := calls[toolResultID(item)]
		if !ok {
			continue
		}
		if protected[i] || protected[call] {
			protected[i] = true
			protected[call] = true
		}
	}
	for i := 1; i < len(input); i++ {
		if protected[i] && input[i].Type == llm.ItemMessage && input[i-1].Type == llm.ItemReasoning {
			protected[i-1] = true
		}
	}
	return protected
}

// chooseDocs follows leCore's retrieval shape, not a forced ranking.
// An exact long token (retrieval_dispatch's phrase short-circuit) wins.
// Several documents that share the query are the set, kept until the budget.
// No overlap means abstain: nothing older is copied back in. Recency is not
// a guess. Causal by construction: protected items, including this turn, are
// not documents.
func chooseDocs(docs []lecoreDoc, query string, budget int) []bool {
	chosen := make([]bool, len(docs))
	order := exactTokenOrder(docs, query)
	if len(order) == 0 {
		order = bm25Order(docs, query)
	}
	if len(order) == 0 {
		return chosen
	}
	used := 0
	for _, index := range order {
		if docs[index].chars == 0 {
			continue
		}
		if used > 0 && used+docs[index].chars > budget {
			continue
		}
		chosen[index] = true
		used += docs[index].chars
		if used >= budget {
			break
		}
	}
	return chosen
}

// exactTokenOrder is the phrase short-circuit. A query token of 6 or more
// characters that actually occurs in the corpus is specific enough to skip
// scoring. Documents that contain every such token are the answer set.
func exactTokenOrder(docs []lecoreDoc, query string) []int {
	var needles []string
	seen := map[string]bool{}
	for _, term := range tokenize(query) {
		if len(term) < 6 || seen[term] {
			continue
		}
		seen[term] = true
		needles = append(needles, term)
	}
	if len(needles) == 0 {
		return nil
	}
	var order []int
	for i, doc := range docs {
		have := map[string]bool{}
		for _, term := range tokenize(doc.text) {
			have[term] = true
		}
		ok := true
		for _, needle := range needles {
			if !have[needle] {
				ok = false
				break
			}
		}
		if ok {
			order = append(order, i)
		}
	}
	return order
}

func bm25Order(docs []lecoreDoc, query string) []int {
	terms := tokenize(query)
	if len(terms) == 0 || len(docs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var unique []string
	for _, term := range terms {
		if !seen[term] {
			seen[term] = true
			unique = append(unique, term)
		}
	}
	tfs := make([]map[string]float64, len(docs))
	df := map[string]float64{}
	lengths := make([]float64, len(docs))
	for i, doc := range docs {
		counts := map[string]float64{}
		for _, term := range tokenize(doc.text) {
			counts[term]++
			lengths[i]++
		}
		tfs[i] = counts
		for term := range counts {
			df[term]++
		}
	}
	n := float64(len(docs))
	avgdl := 0.0
	for _, length := range lengths {
		avgdl += length
	}
	avgdl /= n
	idf := map[string]float64{}
	for term, freq := range df {
		idf[term] = math.Log(1 + (n-freq+0.5)/(freq+0.5))
	}
	scores := make([]float64, len(docs))
	for i, counts := range tfs {
		for _, term := range unique {
			freq := counts[term]
			if freq == 0 {
				continue
			}
			denom := freq + lecoreK1*(1-lecoreB+lecoreB*lengths[i]/(avgdl+1e-12))
			scores[i] += idf[term] * (freq * (lecoreK1 + 1) / (denom + 1e-12))
		}
	}
	order := make([]int, 0, len(docs))
	for i, score := range scores {
		if score > 0 {
			order = append(order, i)
		}
	}
	for i := 1; i < len(order); i++ {
		j := i
		for j > 0 && (scores[order[j]] > scores[order[j-1]] ||
			(scores[order[j]] == scores[order[j-1]] && docs[order[j]].at > docs[order[j-1]].at)) {
			order[j], order[j-1] = order[j-1], order[j]
			j--
		}
	}
	return order
}

func queryText(input []llm.Item, protected []bool) string {
	var b strings.Builder
	for i, item := range input {
		if !protected[i] || isSystem(item) {
			continue
		}
		b.WriteString(itemsText([]llm.Item{item}))
		b.WriteByte('\n')
	}
	return b.String()
}

func itemsText(items []llm.Item) string {
	var b strings.Builder
	for _, item := range items {
		if text := itemText(item); text != "" {
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func itemText(item llm.Item) string {
	switch item.Type {
	case llm.ItemMessage:
		message, _ := item.Data.(llm.Message)
		return message.Text
	case llm.ItemToolCall:
		call, _ := item.Data.(llm.ToolCall)
		return call.Name + " " + call.Arguments
	case llm.ItemToolResult:
		result, _ := item.Data.(llm.ToolResult)
		var b strings.Builder
		for _, output := range result.Output {
			if output.Kind == llm.ToolResultText {
				b.WriteString(output.Value)
				b.WriteByte('\n')
			}
		}
		return b.String()
	case llm.ItemReasoning:
		reasoning, _ := item.Data.(llm.Reasoning)
		return strings.Join(reasoning.Summary, "\n")
	default:
		return ""
	}
}

func itemsChars(items []llm.Item) int {
	n := 0
	for _, item := range items {
		n += itemChars(item)
	}
	return n
}

func itemChars(item llm.Item) int {
	switch item.Type {
	case llm.ItemToolResult:
		result, _ := item.Data.(llm.ToolResult)
		n := 0
		for _, output := range result.Output {
			n += len(output.Value)
		}
		return n
	default:
		return len(itemText(item))
	}
}

func itemSource(item llm.Item) string {
	switch item.Type {
	case llm.ItemToolCall:
		return "tool_call:" + toolCallID(item)
	case llm.ItemToolResult:
		return "tool_result:" + toolResultID(item)
	case llm.ItemMessage:
		message, _ := item.Data.(llm.Message)
		return "message:" + string(message.Role)
	case llm.ItemReasoning:
		return "reasoning"
	default:
		return string(item.Type)
	}
}

func isSystem(item llm.Item) bool {
	message, ok := item.Data.(llm.Message)
	return ok && message.Role == llm.RoleSystem
}

func toolCallID(item llm.Item) string {
	call, _ := item.Data.(llm.ToolCall)
	return call.CallID
}

func toolResultID(item llm.Item) string {
	result, _ := item.Data.(llm.ToolResult)
	return result.CallID
}

func tokenize(text string) []string {
	var terms []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		term := current.String()
		current.Reset()
		if len(term) <= 1 || stopwords[term] {
			return
		}
		terms = append(terms, normalize(term))
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return terms
}

func normalize(term string) string {
	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if strings.HasSuffix(term, suffix) && len(term)-len(suffix) >= 3 {
			return strings.TrimSuffix(term, suffix)
		}
	}
	return term
}

var stopwords = map[string]bool{}

func init() {
	for _, word := range strings.Fields(
		"a an the of to in on at for and or is are be by with from as it this that these those " +
			"into over under out up down off no not do does did can could would should will " +
			"your my our their its his her you we they i he she them us me",
	) {
		stopwords[word] = true
	}
}
