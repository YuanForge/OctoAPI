package protocol

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// SSEConverter converts SSE lines from one protocol format to another.
// Convert is called for each line read from the upstream response body.
// Flush is called once after the scanner reaches EOF to emit any trailing lines.
// Both methods return zero or more output lines; each will be written followed by "\n".
type SSEConverter interface {
	Convert(line string) []string
	Flush() []string
}

// NewSSEConverter returns an SSEConverter for the given (sourceProto → clientProto) pair.
// Returns nil when no conversion is needed (same format, or unsupported pair).
func NewSSEConverter(sourceProto, clientProto string) SSEConverter {
	if sourceProto == clientProto {
		return nil
	}
	switch {
	case sourceProto == ProtocolClaude && clientProto == ProtocolOpenAI:
		return &claudeToOpenAISSE{}
	case sourceProto == ProtocolGemini && clientProto == ProtocolOpenAI:
		return &geminiToOpenAISSE{}
	case sourceProto == ProtocolResponses && clientProto == ProtocolOpenAI:
		return &responsesToOpenAISSE{}
	case sourceProto == ProtocolOpenAI && clientProto == ProtocolClaude:
		return &openAIToClaudeSSE{}
	case sourceProto == ProtocolOpenAI && clientProto == ProtocolResponses:
		return &openAIToResponsesSSE{}
	default:
		// Unsupported pair: pass lines through unchanged so the client at least gets something.
		return nil
	}
}

// ─────────────────────────────────────────────
// Claude SSE → OpenAI SSE
// ─────────────────────────────────────────────

func intFromJSON(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

type claudeToOpenAISSE struct {
	msgID            string
	model            string
	lastEvent        string
	inputTokens      int64
	sentRole         bool
	doneSent         bool
	nextToolIndex    int
	toolIndexByBlock map[int]int
}

func (c *claudeToOpenAISSE) Convert(line string) []string {
	if line == "" {
		return nil // skip Claude's blank event delimiters; we emit our own
	}
	if strings.HasPrefix(line, "event: ") {
		c.lastEvent = strings.TrimPrefix(line, "event: ")
		return nil
	}
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}
	payload := strings.TrimPrefix(line, "data: ")

	var chunk map[string]interface{}
	if json.Unmarshal([]byte(payload), &chunk) != nil {
		return nil
	}

	switch c.lastEvent {
	case "message_start":
		if msg, ok := chunk["message"].(map[string]interface{}); ok {
			c.msgID, _ = msg["id"].(string)
			c.model, _ = msg["model"].(string)
			if usg, ok := msg["usage"].(map[string]interface{}); ok {
				if n, _ := usg["input_tokens"].(float64); n > 0 {
					c.inputTokens = int64(n)
				}
			}
		}
		return c.emitRoleChunk()

	case "content_block_delta":
		if delta, ok := chunk["delta"].(map[string]interface{}); ok {
			if text, _ := delta["text"].(string); text != "" {
				return c.emitTextChunk(text)
			}
			if partial, _ := delta["partial_json"].(string); partial != "" {
				blockIndex := intFromJSON(chunk["index"])
				return c.emitToolArgsChunk(blockIndex, partial)
			}
		}

	case "content_block_start":
		if block, ok := chunk["content_block"].(map[string]interface{}); ok {
			if blockType, _ := block["type"].(string); blockType == "tool_use" {
				blockIndex := intFromJSON(chunk["index"])
				id, _ := block["id"].(string)
				name, _ := block["name"].(string)
				return c.emitToolStartChunk(blockIndex, id, name)
			}
		}

	case "message_delta":
		stopReason := "stop"
		var outputTokens int64
		if delta, ok := chunk["delta"].(map[string]interface{}); ok {
			if sr, _ := delta["stop_reason"].(string); sr != "" {
				switch sr {
				case "max_tokens":
					stopReason = "length"
				case "tool_use":
					stopReason = "tool_calls"
				}
			}
		}
		if usg, ok := chunk["usage"].(map[string]interface{}); ok {
			if n, _ := usg["output_tokens"].(float64); n > 0 {
				outputTokens = int64(n)
			}
		}
		return c.emitFinishChunk(stopReason, outputTokens)

	case "message_stop":
		if !c.doneSent {
			c.doneSent = true
			return []string{"data: [DONE]", ""}
		}
	}
	return nil
}

func (c *claudeToOpenAISSE) openAIToolIndex(blockIndex int) int {
	if c.toolIndexByBlock == nil {
		c.toolIndexByBlock = make(map[int]int)
	}
	if idx, ok := c.toolIndexByBlock[blockIndex]; ok {
		return idx
	}
	idx := c.nextToolIndex
	c.nextToolIndex++
	c.toolIndexByBlock[blockIndex] = idx
	return idx
}

func (c *claudeToOpenAISSE) Flush() []string {
	if !c.doneSent {
		c.doneSent = true
		return []string{"data: [DONE]", ""}
	}
	return nil
}

func (c *claudeToOpenAISSE) emitRoleChunk() []string {
	if c.sentRole {
		return nil
	}
	c.sentRole = true
	out := map[string]interface{}{
		"id":     c.msgID,
		"object": "chat.completion.chunk",
		"model":  c.model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{"role": "assistant", "content": ""},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), ""}
}

func (c *claudeToOpenAISSE) emitTextChunk(text string) []string {
	out := map[string]interface{}{
		"id":     c.msgID,
		"object": "chat.completion.chunk",
		"model":  c.model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{"content": text},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), ""}
}

func (c *claudeToOpenAISSE) emitToolStartChunk(blockIndex int, id, name string) []string {
	idx := c.openAIToolIndex(blockIndex)
	out := map[string]interface{}{
		"id":     c.msgID,
		"object": "chat.completion.chunk",
		"model":  c.model,
		"choices": []interface{}{map[string]interface{}{
			"index": 0,
			"delta": map[string]interface{}{
				"tool_calls": []interface{}{map[string]interface{}{
					"index": idx,
					"id":    id,
					"type":  "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": "",
					},
				}},
			},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), ""}
}

func (c *claudeToOpenAISSE) emitToolArgsChunk(blockIndex int, partial string) []string {
	idx := c.openAIToolIndex(blockIndex)
	out := map[string]interface{}{
		"id":     c.msgID,
		"object": "chat.completion.chunk",
		"model":  c.model,
		"choices": []interface{}{map[string]interface{}{
			"index": 0,
			"delta": map[string]interface{}{
				"tool_calls": []interface{}{map[string]interface{}{
					"index": idx,
					"function": map[string]interface{}{
						"arguments": partial,
					},
				}},
			},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), ""}
}

func (c *claudeToOpenAISSE) emitFinishChunk(reason string, outputTokens int64) []string {
	out := map[string]interface{}{
		"id":     c.msgID,
		"object": "chat.completion.chunk",
		"model":  c.model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{},
			"finish_reason": reason,
		}},
		"usage": map[string]interface{}{
			"prompt_tokens":     c.inputTokens,
			"completion_tokens": outputTokens,
			"total_tokens":      c.inputTokens + outputTokens,
		},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), ""}
}

// ─────────────────────────────────────────────
// Gemini SSE → OpenAI SSE
// ─────────────────────────────────────────────


type geminiToOpenAISSE struct {
	doneSent bool
}

func (g *geminiToOpenAISSE) Convert(line string) []string {
	if line == "" || !strings.HasPrefix(line, "data: ") {
		return nil
	}
	payload := strings.TrimPrefix(line, "data: ")

	var chunk map[string]interface{}
	if json.Unmarshal([]byte(payload), &chunk) != nil {
		return nil
	}

	var text string
	var finishReason interface{} = nil
	isFinish := false

	if candidates, ok := chunk["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]interface{}); ok {
			if contentObj, ok := cand["content"].(map[string]interface{}); ok {
				if parts, ok := contentObj["parts"].([]interface{}); ok {
					for _, p := range parts {
						if pm, ok := p.(map[string]interface{}); ok {
							// 跳过思考链 part（thought=true 或含 thoughtSignature 字段）
							if isGeminiThoughtPart(pm) {
								continue
							}
							if t, ok := pm["text"].(string); ok {
								text += t
							}
						}
					}
				}
			}
			if fr, ok := cand["finishReason"].(string); ok && fr != "" && fr != "FINISH_REASON_UNSPECIFIED" {
				isFinish = true
				if fr == "MAX_TOKENS" {
					finishReason = "length"
				} else {
					finishReason = "stop"
				}
			}
		}
	}

	deltaChunk := map[string]interface{}{
		"id":     "chatcmpl-gemini",
		"object": "chat.completion.chunk",
		"model":  "",
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{"content": text},
			"finish_reason": finishReason,
		}},
	}

	if meta, ok := chunk["usageMetadata"].(map[string]interface{}); ok {
		in, _ := meta["promptTokenCount"].(float64)
		out, _ := meta["candidatesTokenCount"].(float64)
		thoughts, _ := meta["thoughtsTokenCount"].(float64)
		deltaChunk["usage"] = map[string]interface{}{
			"prompt_tokens":     int64(in),
			"completion_tokens": int64(out + thoughts),
			"total_tokens":      int64(in + out + thoughts),
		}
	}

	b, _ := json.Marshal(deltaChunk)
	result := []string{"data: " + string(b), ""}

	if isFinish && !g.doneSent {
		g.doneSent = true
		result = append(result, "data: [DONE]", "")
	}

	if text == "" && !isFinish {
		return nil // 跳过没有内容且非结束的中间块（如纯 usageMetadata chunk）
	}

	return result
}

func (g *geminiToOpenAISSE) Flush() []string {
	if !g.doneSent {
		g.doneSent = true
		return []string{"data: [DONE]", ""}
	}
	return nil
}

// ─────────────────────────────────────────────
// Responses API SSE → OpenAI SSE
// ─────────────────────────────────────────────

type responsesToOpenAISSE struct {
	lastEvent string
	respID    string
	model     string
	doneSent  bool
	roleSent  bool
	buffer    strings.Builder
}

func (r *responsesToOpenAISSE) Convert(line string) []string {
	if line == "" {
		return nil
	}
	if strings.HasPrefix(line, "event: ") {
		r.lastEvent = strings.TrimPrefix(line, "event: ")
		return nil
	}
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}

	payload := strings.TrimPrefix(line, "data: ")
	var chunk map[string]interface{}
	if json.Unmarshal([]byte(payload), &chunk) != nil {
		return nil
	}

	if evType, _ := chunk["type"].(string); evType != "" {
		r.lastEvent = evType
	}

	if responseObj, ok := chunk["response"].(map[string]interface{}); ok {
		if id, ok := responseObj["id"].(string); ok && id != "" {
			r.respID = id
		}
		if m, ok := responseObj["model"].(string); ok && m != "" {
			r.model = m
		}
	}

	switch r.lastEvent {
	case "response.output_text.delta":
		delta, _ := chunk["delta"].(string)
		if delta == "" {
			return nil
		}
		r.buffer.WriteString(delta)
		if !r.roleSent {
			r.roleSent = true
			return r.emitDeltaWithRole(delta)
		}
		return r.emitDelta(delta)

	case "response.completed":
		if r.doneSent {
			return nil
		}
		r.doneSent = true

		prompt := int64(0)
		completion := int64(0)
		if responseObj, ok := chunk["response"].(map[string]interface{}); ok {
			if usg, ok := responseObj["usage"].(map[string]interface{}); ok {
				if n, _ := usg["input_tokens"].(float64); n > 0 {
					prompt = int64(n)
				}
				if n, _ := usg["output_tokens"].(float64); n > 0 {
					completion = int64(n)
				}
			}
		}

		return r.emitFinish(prompt, completion)
	}

	return nil
}

func (r *responsesToOpenAISSE) Flush() []string {
	if r.doneSent {
		return nil
	}
	r.doneSent = true
	return []string{"data: [DONE]", ""}
}

func (r *responsesToOpenAISSE) emitDeltaWithRole(delta string) []string {
	out := map[string]interface{}{
		"id":     r.chunkID(),
		"object": "chat.completion.chunk",
		"model":  r.model,
		"choices": []interface{}{map[string]interface{}{
			"index": 0,
			"delta": map[string]interface{}{
				"role":    "assistant",
				"content": delta,
			},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), ""}
}

func (r *responsesToOpenAISSE) emitDelta(delta string) []string {
	out := map[string]interface{}{
		"id":     r.chunkID(),
		"object": "chat.completion.chunk",
		"model":  r.model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{"content": delta},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), ""}
}

func (r *responsesToOpenAISSE) emitFinish(prompt, completion int64) []string {
	out := map[string]interface{}{
		"id":     r.chunkID(),
		"object": "chat.completion.chunk",
		"model":  r.model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{},
			"finish_reason": "stop",
		}},
		"usage": map[string]interface{}{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      prompt + completion,
		},
	}
	b, _ := json.Marshal(out)
	return []string{"data: " + string(b), "", "data: [DONE]", ""}
}

func (r *responsesToOpenAISSE) chunkID() string {
	if r.respID != "" {
		return r.respID
	}
	return "chatcmpl-" + newShortID()
}

// ─────────────────────────────────────────────
// OpenAI SSE → Claude SSE
// ─────────────────────────────────────────────

type openAIToClaudeSSE struct {
	msgID            string
	model            string
	inputTokens      int64
	outputTokens     int64
	sentStart        bool
	doneSent         bool
	nextBlockIndex   int
	activeBlockIndex int
	activeBlockKind  string
	stopReason       string
	toolBlocks       map[int]openAIToClaudeToolBlock
}

type openAIToClaudeToolBlock struct {
	blockIndex int
	id         string
	name       string
	args       string
}

func (o *openAIToClaudeSSE) Convert(line string) []string {
	if line == "" {
		return nil
	}
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}
	payload := strings.TrimPrefix(line, "data: ")
	if payload == "[DONE]" {
		o.doneSent = true
		return o.stopEvents()
	}

	var chunk map[string]interface{}
	if json.Unmarshal([]byte(payload), &chunk) != nil {
		return nil
	}

	if o.msgID == "" {
		o.msgID, _ = chunk["id"].(string)
		o.model, _ = chunk["model"].(string)
	}
	if usg, ok := chunk["usage"].(map[string]interface{}); ok {
		if pt, _ := usg["prompt_tokens"].(float64); pt > 0 {
			o.inputTokens = int64(pt)
		}
		if ct, _ := usg["completion_tokens"].(float64); ct > 0 {
			o.outputTokens = int64(ct)
		}
	}

	choices, _ := chunk["choices"].([]interface{})
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]interface{})
	if choice == nil {
		return nil
	}

	var result []string

	if !o.sentStart {
		o.sentStart = true
		result = append(result, o.messageStartLines()...)
		result = append(result, o.pingLines()...)
	}

	delta, _ := choice["delta"].(map[string]interface{})
	if content, _ := delta["content"].(string); content != "" {
		result = append(result, o.ensureTextBlock()...)
		result = append(result, o.contentDeltaLines(content)...)
	}
	if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
		result = append(result, o.toolCallDeltaLines(toolCalls)...)
	}
	if finish, _ := choice["finish_reason"].(string); finish != "" {
		if finish == "tool_calls" {
			o.stopReason = "tool_use"
		} else if finish == "length" {
			o.stopReason = "max_tokens"
		} else {
			o.stopReason = "end_turn"
		}
	}

	return result
}

func (o *openAIToClaudeSSE) Flush() []string {
	if !o.doneSent {
		return o.stopEvents()
	}
	return nil
}

func (o *openAIToClaudeSSE) messageStartLines() []string {
	msg := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":    o.msgID,
			"type":  "message",
			"role":  "assistant",
			"model": o.model,
			"usage": map[string]interface{}{"input_tokens": o.inputTokens, "output_tokens": 0},
		},
	}
	b, _ := json.Marshal(msg)
	return []string{"event: message_start", "data: " + string(b), ""}
}

func (o *openAIToClaudeSSE) pingLines() []string {
	return []string{"event: ping", `data: {"type":"ping"}`, ""}
}

func (o *openAIToClaudeSSE) ensureTextBlock() []string {
	if o.activeBlockKind == "text" {
		return nil
	}
	var out []string
	out = append(out, o.closeActiveBlock()...)
	index := o.nextBlockIndex
	o.nextBlockIndex++
	o.activeBlockKind = "text"
	o.activeBlockIndex = index
	data := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	}
	b, _ := json.Marshal(data)
	return append(out, "event: content_block_start", "data: "+string(b), "")
}

func (o *openAIToClaudeSSE) contentDeltaLines(text string) []string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": o.activeBlockIndex,
		"delta": map[string]interface{}{"type": "text_delta", "text": text},
	}
	b, _ := json.Marshal(data)
	return []string{"event: content_block_delta", "data: " + string(b), ""}
}

func (o *openAIToClaudeSSE) toolCallDeltaLines(toolCalls []interface{}) []string {
	for _, raw := range toolCalls {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		idx := intFromJSON(tc["index"])
		fn, _ := tc["function"].(map[string]interface{})
		id, _ := tc["id"].(string)
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)

		block, _ := o.openAIToolBlock(idx)
		if id != "" {
			block.id = id
		}
		if name != "" {
			block.name = name
		}
		if block.id == "" {
			block.id = "toolu_" + newShortID()
		}
		if args != "" {
			block.args += args
		}
		o.toolBlocks[idx] = block
	}
	return nil
}

func (o *openAIToClaudeSSE) openAIToolBlock(index int) (openAIToClaudeToolBlock, bool) {
	if o.toolBlocks == nil {
		o.toolBlocks = make(map[int]openAIToClaudeToolBlock)
	}
	block, exists := o.toolBlocks[index]
	if !exists {
		block = openAIToClaudeToolBlock{
			blockIndex: o.nextBlockIndex,
		}
		o.nextBlockIndex++
	}
	return block, exists
}

func (o *openAIToClaudeSSE) toolBlockStartLines(block openAIToClaudeToolBlock) []string {
	data := map[string]interface{}{
		"type":  "content_block_start",
		"index": block.blockIndex,
		"content_block": map[string]interface{}{
			"type":  "tool_use",
			"id":    block.id,
			"name":  block.name,
			"input": map[string]interface{}{},
		},
	}
	b, _ := json.Marshal(data)
	return []string{"event: content_block_start", "data: " + string(b), ""}
}

func (o *openAIToClaudeSSE) toolArgsDeltaLines(blockIndex int, args string) []string {
	data := map[string]interface{}{
		"type":  "content_block_delta",
		"index": blockIndex,
		"delta": map[string]interface{}{
			"type":         "input_json_delta",
			"partial_json": args,
		},
	}
	b, _ := json.Marshal(data)
	return []string{"event: content_block_delta", "data: " + string(b), ""}
}

func (o *openAIToClaudeSSE) closeActiveBlock() []string {
	if o.activeBlockKind == "" {
		return nil
	}
	data := map[string]interface{}{
		"type":  "content_block_stop",
		"index": o.activeBlockIndex,
	}
	b, _ := json.Marshal(data)
	o.activeBlockKind = ""
	return []string{"event: content_block_stop", "data: " + string(b), ""}
}

func (o *openAIToClaudeSSE) bufferedToolBlockLines() []string {
	if len(o.toolBlocks) == 0 {
		return nil
	}
	maxIndex := -1
	for idx := range o.toolBlocks {
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	var out []string
	for idx := 0; idx <= maxIndex; idx++ {
		block, ok := o.toolBlocks[idx]
		if !ok {
			continue
		}
		out = append(out, o.toolBlockStartLines(block)...)
		if block.args != "" {
			out = append(out, o.toolArgsDeltaLines(block.blockIndex, block.args)...)
		}
		data := map[string]interface{}{
			"type":  "content_block_stop",
			"index": block.blockIndex,
		}
		b, _ := json.Marshal(data)
		out = append(out, "event: content_block_stop", "data: "+string(b), "")
	}
	return out
}

func (o *openAIToClaudeSSE) stopEvents() []string {
	var out []string
	if !o.sentStart {
		o.sentStart = true
		out = append(out, o.messageStartLines()...)
	}
	out = append(out, o.closeActiveBlock()...)
	out = append(out, o.bufferedToolBlockLines()...)

	stopReason := o.stopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	outTok := o.outputTokens
	msgDelta := map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]interface{}{"output_tokens": outTok},
	}
	b, _ := json.Marshal(msgDelta)
	out = append(out,
		"event: message_delta",
		"data: " + string(b),
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	)
	return out
}

// ─────────────────────────────────────────────
// OpenAI SSE → Responses API SSE
//
// Codex CLI 使用 OpenAI Responses API（POST /v1/responses），
// 其 SSE 事件格式与 Chat Completions 完全不同。
// 此转换器将上游 OpenAI Chat Completions SSE 流转换为 Responses API SSE 事件。
//
// 事件顺序：
//   response.created → response.output_item.added → response.content_part.added
//   → (N×) response.output_text.delta
//   → response.output_text.done → response.content_part.done
//   → response.output_item.done → response.completed
// ─────────────────────────────────────────────

type openAIToResponsesSSE struct {
	respID   string
	itemID   string
	model    string
	fullText string
	// 状态标记
	headerSent bool
	doneSent   bool
	// token 统计
	inputTokens  int64
	outputTokens int64
	textOutputIndex int
	textStarted     bool
	textDone        bool
	nextOutputIndex int
	toolCalls       map[int]responsesToolCall
}

type responsesToolCall struct {
	outputIndex int
	itemID      string
	callID      string
	name        string
	arguments   string
	done        bool
}

func (r *openAIToResponsesSSE) Convert(line string) []string {
	if line == "" {
		return nil
	}
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}
	payload := strings.TrimPrefix(line, "data: ")
	if payload == "[DONE]" {
		return nil // 在 Flush 中处理收尾事件
	}

	var chunk map[string]interface{}
	if json.Unmarshal([]byte(payload), &chunk) != nil {
		return nil
	}

	// 首个 chunk：提取 id 和 model，发送 header 事件
	var out []string
	if !r.headerSent {
		r.headerSent = true
		if id, ok := chunk["id"].(string); ok {
			r.respID = id
		} else {
			r.respID = "resp_" + newShortID()
		}
		if m, ok := chunk["model"].(string); ok {
			r.model = m
		}
		out = append(out, r.emitCreated()...)
	}

	// 收集 usage（最后一个 chunk 会携带）
	if usg, ok := chunk["usage"].(map[string]interface{}); ok {
		if n, _ := usg["prompt_tokens"].(float64); n > 0 {
			r.inputTokens = int64(n)
		}
		if n, _ := usg["completion_tokens"].(float64); n > 0 {
			r.outputTokens = int64(n)
		}
	}

	// 提取 delta 文本
	choices, _ := chunk["choices"].([]interface{})
	if len(choices) == 0 {
		return out
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return out
	}

	// delta 文本增量
	if delta, ok := choice["delta"].(map[string]interface{}); ok {
		if text, ok := delta["content"].(string); ok && text != "" {
			out = append(out, r.ensureTextOutput()...)
			r.fullText += text
			out = append(out, r.emitTextDelta(text)...)
		}
		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			out = append(out, r.toolCallDeltaLines(toolCalls)...)
		}
	}

	return out
}

func (r *openAIToResponsesSSE) Flush() []string {
	if r.doneSent {
		return nil
	}
	r.doneSent = true
	var out []string
	out = append(out, r.finishTextOutput()...)
	out = append(out, r.finishToolOutputs()...)
	out = append(out, r.emitCompleted()...)
	return out
}

func (r *openAIToResponsesSSE) emitCreated() []string {
	resp := map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":     r.respID,
			"object": "response",
			"status": "in_progress",
			"model":  r.model,
			"output": []interface{}{},
		},
	}
	b, _ := json.Marshal(resp)
	return []string{"event: response.created", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) ensureTextOutput() []string {
	if r.textStarted {
		return nil
	}
	r.textStarted = true
	r.itemID = "msg_" + newShortID()
	r.textOutputIndex = r.nextOutputIndex
	r.nextOutputIndex++
	out := r.emitOutputItemAdded()
	out = append(out, r.emitContentPartAdded()...)
	return out
}

func (r *openAIToResponsesSSE) emitOutputItemAdded() []string {
	item := map[string]interface{}{
		"type":         "response.output_item.added",
		"output_index": r.textOutputIndex,
		"item": map[string]interface{}{
			"id":      r.itemID,
			"type":    "message",
			"role":    "assistant",
			"content": []interface{}{},
			"status":  "in_progress",
		},
	}
	b, _ := json.Marshal(item)
	return []string{"event: response.output_item.added", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitContentPartAdded() []string {
	ev := map[string]interface{}{
		"type":          "response.content_part.added",
		"item_id":       r.itemID,
		"output_index":  r.textOutputIndex,
		"content_index": 0,
		"part": map[string]interface{}{
			"type": "output_text",
			"text": "",
		},
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.content_part.added", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitTextDelta(delta string) []string {
	ev := map[string]interface{}{
		"type":          "response.output_text.delta",
		"item_id":       r.itemID,
		"output_index":  r.textOutputIndex,
		"content_index": 0,
		"delta":         delta,
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.output_text.delta", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitTextDone() []string {
	ev := map[string]interface{}{
		"type":          "response.output_text.done",
		"item_id":       r.itemID,
		"output_index":  r.textOutputIndex,
		"content_index": 0,
		"text":          r.fullText,
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.output_text.done", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitContentPartDone() []string {
	ev := map[string]interface{}{
		"type":          "response.content_part.done",
		"item_id":       r.itemID,
		"output_index":  r.textOutputIndex,
		"content_index": 0,
		"part": map[string]interface{}{
			"type": "output_text",
			"text": r.fullText,
		},
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.content_part.done", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitOutputItemDone() []string {
	ev := map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": r.textOutputIndex,
		"item": map[string]interface{}{
			"id":     r.itemID,
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []interface{}{
				map[string]interface{}{
					"type": "output_text",
					"text": r.fullText,
				},
			},
		},
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.output_item.done", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) finishTextOutput() []string {
	if !r.textStarted || r.textDone {
		return nil
	}
	r.textDone = true
	var out []string
	out = append(out, r.emitTextDone()...)
	out = append(out, r.emitContentPartDone()...)
	out = append(out, r.emitOutputItemDone()...)
	return out
}

func (r *openAIToResponsesSSE) toolCallDeltaLines(toolCalls []interface{}) []string {
	var out []string
	for _, raw := range toolCalls {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		idx := intFromJSON(tc["index"])
		fn, _ := tc["function"].(map[string]interface{})
		id, _ := tc["id"].(string)
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)

		block, exists := r.openAIToolCall(idx)
		if id != "" {
			block.callID = id
		}
		if name != "" {
			block.name = name
		}
		if block.callID == "" {
			block.callID = "call_" + newShortID()
		}
		if !exists {
			out = append(out, r.emitToolOutputItemAdded(block)...)
		}
		if args != "" {
			block.arguments += args
			out = append(out, r.emitToolArgumentsDelta(block, args)...)
		}
		r.toolCalls[idx] = block
	}
	return out
}

func (r *openAIToResponsesSSE) openAIToolCall(index int) (responsesToolCall, bool) {
	if r.toolCalls == nil {
		r.toolCalls = make(map[int]responsesToolCall)
	}
	block, exists := r.toolCalls[index]
	if !exists {
		block = responsesToolCall{
			outputIndex: r.nextOutputIndex,
			itemID:      "fc_" + newShortID(),
		}
		r.nextOutputIndex++
	}
	return block, exists
}

func (r *openAIToResponsesSSE) emitToolOutputItemAdded(block responsesToolCall) []string {
	ev := map[string]interface{}{
		"type":         "response.output_item.added",
		"output_index": block.outputIndex,
		"item":         r.toolOutputItem(block, "in_progress"),
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.output_item.added", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitToolArgumentsDelta(block responsesToolCall, delta string) []string {
	ev := map[string]interface{}{
		"type":         "response.function_call_arguments.delta",
		"item_id":      block.itemID,
		"output_index": block.outputIndex,
		"delta":        delta,
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.function_call_arguments.delta", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitToolArgumentsDone(block responsesToolCall) []string {
	ev := map[string]interface{}{
		"type":         "response.function_call_arguments.done",
		"item_id":      block.itemID,
		"output_index": block.outputIndex,
		"arguments":    block.arguments,
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.function_call_arguments.done", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) emitToolOutputItemDone(block responsesToolCall) []string {
	ev := map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": block.outputIndex,
		"item":         r.toolOutputItem(block, "completed"),
	}
	b, _ := json.Marshal(ev)
	return []string{"event: response.output_item.done", "data: " + string(b), ""}
}

func (r *openAIToResponsesSSE) finishToolOutputs() []string {
	if len(r.toolCalls) == 0 {
		return nil
	}
	var out []string
	for _, idx := range r.sortedToolCallIndexes() {
		block := r.toolCalls[idx]
		if block.done {
			continue
		}
		block.done = true
		out = append(out, r.emitToolArgumentsDone(block)...)
		out = append(out, r.emitToolOutputItemDone(block)...)
		r.toolCalls[idx] = block
	}
	return out
}

func (r *openAIToResponsesSSE) sortedToolCallIndexes() []int {
	indexes := make([]int, 0, len(r.toolCalls))
	for idx := range r.toolCalls {
		indexes = append(indexes, idx)
	}
	for i := 1; i < len(indexes); i++ {
		for j := i; j > 0 && r.toolCalls[indexes[j-1]].outputIndex > r.toolCalls[indexes[j]].outputIndex; j-- {
			indexes[j-1], indexes[j] = indexes[j], indexes[j-1]
		}
	}
	return indexes
}

func (r *openAIToResponsesSSE) toolOutputItem(block responsesToolCall, status string) map[string]interface{} {
	return map[string]interface{}{
		"id":        block.itemID,
		"type":      "function_call",
		"status":    status,
		"call_id":   block.callID,
		"name":      block.name,
		"arguments": block.arguments,
	}
}

func (r *openAIToResponsesSSE) completedOutput() []interface{} {
	output := make([]interface{}, 0)
	if r.textStarted {
		output = append(output, map[string]interface{}{
			"id":     r.itemID,
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []interface{}{
				map[string]interface{}{
					"type": "output_text",
					"text": r.fullText,
				},
			},
		})
	}
	for _, idx := range r.sortedToolCallIndexes() {
		output = append(output, r.toolOutputItem(r.toolCalls[idx], "completed"))
	}
	return output
}

func (r *openAIToResponsesSSE) emitCompleted() []string {
	usage := map[string]interface{}{
		"input_tokens":  r.inputTokens,
		"output_tokens": r.outputTokens,
		"total_tokens":  r.inputTokens + r.outputTokens,
	}
	resp := map[string]interface{}{
		"type": "response.completed",
		"response": map[string]interface{}{
			"id":     r.respID,
			"object": "response",
			"status": "completed",
			"model":  r.model,
			"output": r.completedOutput(),
			"usage":  usage,
		},
	}
	b, _ := json.Marshal(resp)
	return []string{"event: response.completed", "data: " + string(b), ""}
}

// newShortID 生成短 ID（不含横线）供 Responses API 的 id 字段使用。
func newShortID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}
