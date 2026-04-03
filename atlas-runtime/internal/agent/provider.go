package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// TokenUsage holds the input and output token counts from a single AI call.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// streamResult is returned by streamWithToolDetection — the single streaming
// call that replaces the old probe + re-stream pair. One API call per loop
// iteration regardless of whether the turn ends with text or tool calls.
type streamResult struct {
	FinalText    string        // text response (may be non-empty even when ToolCalls is set)
	ToolCalls    []OAIToolCall // non-nil means a tool-call turn
	FinishReason string
	Usage        TokenUsage
}

// ProviderType identifies which AI backend to use.
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
	ProviderGemini    ProviderType = "gemini"
	ProviderLMStudio  ProviderType = "lm_studio"
	ProviderOllama       ProviderType = "ollama"
	ProviderAtlasEngine  ProviderType = "atlas_engine"
)

// ProviderConfig carries everything the agent loop needs to call an AI backend.
type ProviderConfig struct {
	Type    ProviderType
	APIKey  string
	Model   string
	BaseURL string // used by LM Studio and Ollama
}

// ── Non-streaming (Forge + RegenerateMind) ────────────────────────────────────

// CallVision makes a single vision inference call using the configured provider.
// imageB64 is a raw base64-encoded PNG (no data URI prefix). prompt is the
// instruction sent alongside the image. Returns the model's text response.
//
// The image content block format is adapted per-provider (Anthropic vs OpenAI).
func CallVision(ctx context.Context, p ProviderConfig, imageB64, prompt string) (string, error) {
	var imageBlock any
	switch p.Type {
	case ProviderAnthropic:
		imageBlock = map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       imageB64,
			},
		}
	default: // openai, gemini, lm_studio, ollama, ollama — image_url format
		imageBlock = map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": "data:image/png;base64," + imageB64},
		}
	}

	msg := OAIMessage{
		Role: "user",
		Content: []any{
			imageBlock,
			map[string]any{"type": "text", "text": prompt},
		},
	}

	reply, _, _, err := callAINonStreaming(ctx, p, []OAIMessage{msg}, nil)
	if err != nil {
		return "", err
	}

	// Extract plain text from the reply content (string or content-block slice).
	switch c := reply.Content.(type) {
	case string:
		return strings.TrimSpace(c), nil
	case []any:
		var parts []string
		for _, blk := range c {
			if bmap, ok := blk.(map[string]any); ok {
				if t, ok := bmap["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "")), nil
	}
	return strings.TrimSpace(fmt.Sprintf("%v", reply.Content)), nil
}

// CallAINonStreamingExported allows packages outside the agent package to make
// single-shot non-streaming AI calls (used by Forge and RegenerateMind).
func CallAINonStreamingExported(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
) (OAIMessage, string, TokenUsage, error) {
	return callAINonStreaming(ctx, p, messages, tools)
}

func callAINonStreaming(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
) (OAIMessage, string, TokenUsage, error) {
	switch p.Type {
	case ProviderAnthropic:
		return callAnthropicNonStreaming(ctx, p, messages, tools)
	default: // openai, gemini, lm_studio, ollama
		return callOpenAICompatNonStreaming(ctx, p, messages, tools)
	}
}

// ── Streaming with tool detection (agent loop) ────────────────────────────────

// streamWithToolDetection is the sole streaming entry point for the agent loop.
// It makes one API call per iteration and handles both text and tool-call turns.
//
// Engine LM (llama-server) and LM Studio reject stream:true when tools are
// present in the request body. For those providers we fall back to a non-streaming
// call and emit the complete response as a single token event so the rest of the
// loop is unchanged.
func streamWithToolDetection(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
	convID string,
	bc Emitter,
) (streamResult, error) {
	switch p.Type {
	case ProviderAnthropic:
		return streamAnthropicWithToolDetection(ctx, p, messages, tools, convID, bc)
	case ProviderAtlasEngine, ProviderLMStudio, ProviderOllama:
		// These providers do not support stream:true + tools. Use non-streaming
		// and emit the full response text as one token event.
		return nonStreamingAsStream(ctx, p, messages, tools, convID, bc)
	default: // openai, gemini
		return streamOpenAICompatWithToolDetection(ctx, p, messages, tools, convID, bc)
	}
}

// nonStreamingAsStream calls callOpenAICompatNonStreaming and wraps the result
// as a streamResult, emitting the full text as a single SSE token event so the
// agent loop and UI behave identically to the streaming path.
func nonStreamingAsStream(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
	convID string,
	bc Emitter,
) (streamResult, error) {
	// Emit an empty "assistant started" token first (matches streaming behaviour).
	bc.Emit(convID, EmitEvent{Type: "token", Role: "assistant", ConvID: convID})

	msg, finishReason, usage, err := callOpenAICompatNonStreaming(ctx, p, messages, tools)
	if err != nil {
		return streamResult{}, err
	}

	// Emit the full text as a single token event.
	text := ""
	if s, ok := msg.Content.(string); ok {
		text = s
	}
	if text != "" {
		bc.Emit(convID, EmitEvent{Type: "token", Content: text, Role: "assistant", ConvID: convID})
	}

	return streamResult{
		FinalText:    text,
		ToolCalls:    msg.ToolCalls,
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}

// ── OpenAI-compatible (OpenAI, Gemini, LM Studio) ─────────────────────────────

func oaiCompatBaseURL(p ProviderConfig) string {
	switch p.Type {
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case ProviderLMStudio:
		base := strings.TrimRight(p.BaseURL, "/")
		if base == "" {
			base = "http://localhost:1234"
		}
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		return base
	case ProviderOllama:
		base := strings.TrimRight(p.BaseURL, "/")
		if base == "" {
			base = "http://localhost:11434"
		}
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		return base
	case ProviderAtlasEngine:
		// Engine LM runs a managed llama-server process on an internal port.
		// BaseURL is set by the process manager in Phase 1; defaults to 11985 for dev.
		base := strings.TrimRight(p.BaseURL, "/")
		if base == "" {
			base = "http://localhost:11985"
		}
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		return base
	default: // openai
		return "https://api.openai.com/v1"
	}
}

func callOpenAICompatNonStreaming(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
) (OAIMessage, string, TokenUsage, error) {
	reqBody := map[string]any{
		"model":    p.Model,
		"messages": messages,
		"stream":   false,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return OAIMessage{}, "", TokenUsage{}, err
	}

	url := oaiCompatBaseURL(p) + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return OAIMessage{}, "", TokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("AI non-streaming request failed (%s): %w", p.Type, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody)
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("AI error %d (%s): %s", resp.StatusCode, p.Type, errBody.Error.Message)
	}

	var completion struct {
		Choices []struct {
			Message      json.RawMessage `json:"message"`
			FinishReason string          `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("AI response parse failed (%s): %w", p.Type, err)
	}
	if len(completion.Choices) == 0 {
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("AI returned no choices (%s)", p.Type)
	}

	usage := TokenUsage{
		InputTokens:  completion.Usage.PromptTokens,
		OutputTokens: completion.Usage.CompletionTokens,
	}

	choice := completion.Choices[0]
	var rawMsg struct {
		Role      string        `json:"role"`
		Content   *string       `json:"content"`
		ToolCalls []OAIToolCall `json:"tool_calls"`
	}
	if err := json.Unmarshal(choice.Message, &rawMsg); err != nil {
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("AI message parse failed (%s): %w", p.Type, err)
	}

	msg := OAIMessage{
		Role:      rawMsg.Role,
		ToolCalls: rawMsg.ToolCalls,
	}
	if rawMsg.Content != nil {
		msg.Content = *rawMsg.Content
	}
	return msg, choice.FinishReason, usage, nil
}

// streamOpenAICompatWithToolDetection streams a single OpenAI-compatible API
// call. Text tokens are emitted in real time via bc. Tool call argument
// fragments are accumulated by delta index and assembled on completion.
func streamOpenAICompatWithToolDetection(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
	convID string,
	bc Emitter,
) (streamResult, error) {
	reqBody := map[string]any{
		"model":    p.Model,
		"messages": messages,
		"stream":   true,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}
	// stream_options is supported by OpenAI and Gemini but not LM Studio.
	if p.Type != ProviderLMStudio {
		reqBody["stream_options"] = map[string]any{"include_usage": true}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return streamResult{}, err
	}

	url := oaiCompatBaseURL(p) + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return streamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return streamResult{}, fmt.Errorf("AI streaming request failed (%s): %w", p.Type, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return streamResult{}, fmt.Errorf("AI streaming error %d (%s): %s", resp.StatusCode, p.Type, string(bodyBytes))
	}

	bc.Emit(convID, EmitEvent{Type: "token", Role: "assistant", ConvID: convID})

	// toolAccum holds partially-streamed data for one tool call.
	type toolAccum struct {
		id               string
		typ              string
		name             string
		args             strings.Builder
		thoughtSignature string // Gemini thinking models only
	}

	var (
		fullText     strings.Builder
		usage        TokenUsage
		finishReason string
		accums       = map[int]*toolAccum{}
	)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
						// Gemini thinking models nest the thought_signature inside extra_content.
						ExtraContent struct {
							Google struct {
								ThoughtSignature string `json:"thought_signature"`
							} `json:"google"`
						} `json:"extra_content"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Usage arrives in the final summary chunk (choices is empty).
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}

		// Accumulate tool call argument fragments.
		for _, tc := range choice.Delta.ToolCalls {
			acc := accums[tc.Index]
			if acc == nil {
				acc = &toolAccum{}
				accums[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Type != "" {
				acc.typ = tc.Type
			}
			if sig := tc.ExtraContent.Google.ThoughtSignature; sig != "" {
				acc.thoughtSignature = sig
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.args.WriteString(tc.Function.Arguments)
		}

		// Emit text tokens in real time.
		if token := choice.Delta.Content; token != "" {
			fullText.WriteString(token)
			bc.Emit(convID, EmitEvent{Type: "token", Content: token, Role: "assistant", ConvID: convID})
		}
	}

	if err := scanner.Err(); err != nil {
		return streamResult{}, fmt.Errorf("stream read error: %w", err)
	}

	// Assemble tool calls in index order.
	var toolCalls []OAIToolCall
	for i := 0; i < len(accums); i++ {
		acc, ok := accums[i]
		if !ok {
			break
		}
		tcType := "function"
		if acc.typ != "" {
			tcType = acc.typ
		}
		tc := OAIToolCall{
			ID:   acc.id,
			Type: tcType,
			Function: OAIFunctionCall{
				Name:      acc.name,
				Arguments: acc.args.String(),
			},
		}
		if acc.thoughtSignature != "" {
			tc.ExtraContent = &OAIToolCallExtras{
				Google: OAIToolCallGoogle{ThoughtSignature: acc.thoughtSignature},
			}
		}
		toolCalls = append(toolCalls, tc)
	}

	// Some LM Studio models omit finish_reason — infer from accumulated state.
	if finishReason == "" {
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}

	return streamResult{
		FinalText:    fullText.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}

// ── Anthropic ─────────────────────────────────────────────────────────────────

const anthropicBaseURL = "https://api.anthropic.com/v1"
const anthropicVersion = "2023-06-01"
const anthropicCachingBeta = "prompt-caching-2024-07-31"

// anthropicCachedSystem wraps the system prompt in the array format required
// for Anthropic prompt caching. The cache_control block is placed on the final
// content block so the entire prefix (system + tools) is cacheable.
//
// Cache economics (claude-3-5-sonnet):
//   - Write: 25 % more than normal input price (charged once per cache miss)
//   - Read:  ~10 % of normal input price (charged on every cache hit)
//   - TTL:   5 minutes (refreshed on each API call within that window)
//
// Break-even is 2 calls with the same prefix. Since Atlas sends the same
// system prompt on every turn, every turn after the first is a cache hit.
func anthropicCachedSystem(prompt string) []map[string]any {
	return []map[string]any{
		{
			"type":          "text",
			"text":          prompt,
			"cache_control": map[string]any{"type": "ephemeral"},
		},
	}
}

// anthropicCachedTools appends a cache_control block to the last tool so the
// entire tool list is included in the cached prefix.
func anthropicCachedTools(tools []map[string]any) []map[string]any {
	if len(tools) == 0 {
		return tools
	}
	out := make([]map[string]any, len(tools))
	copy(out, tools)
	last := make(map[string]any, len(out[len(out)-1])+1)
	for k, v := range out[len(out)-1] {
		last[k] = v
	}
	last["cache_control"] = map[string]any{"type": "ephemeral"}
	out[len(out)-1] = last
	return out
}

// convertContentToAnthropic converts an OAIMessage.Content value to the format
// expected by Anthropic's Messages API.
//
//   - string → returned as-is (Anthropic accepts plain string content).
//   - []map[string]any content parts (built by buildUserContent in chat/service.go):
//     • {"type":"text","text":"..."} → {"type":"text","text":"..."}
//     • {"type":"image_url","image_url":{"url":"data:image/*;base64,..."}} →
//       {"type":"image","source":{"type":"base64","media_type":"image/*","data":"..."}}
//     • {"type":"image_url","image_url":{"url":"data:application/pdf;base64,..."}} →
//       {"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"..."}}
func convertContentToAnthropic(content any) any {
	if s, ok := content.(string); ok {
		return s
	}

	parts, ok := content.([]map[string]any)
	if !ok {
		return ""
	}

	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		partType, _ := part["type"].(string)
		switch partType {
		case "text":
			out = append(out, map[string]any{"type": "text", "text": part["text"]})
		case "image_url":
			imgURL, _ := part["image_url"].(map[string]any)
			if imgURL == nil {
				continue
			}
			dataURL, _ := imgURL["url"].(string)
			if !strings.HasPrefix(dataURL, "data:") {
				continue
			}
			rest := strings.TrimPrefix(dataURL, "data:")
			semi := strings.Index(rest, ";base64,")
			if semi < 0 {
				continue
			}
			mimeType := rest[:semi]
			b64data := rest[semi+len(";base64,"):]
			if mimeType == "application/pdf" {
				out = append(out, map[string]any{
					"type": "document",
					"source": map[string]any{
						"type":       "base64",
						"media_type": mimeType,
						"data":       b64data,
					},
				})
			} else {
				out = append(out, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": mimeType,
						"data":       b64data,
					},
				})
			}
		}
	}
	return out
}

// convertToAnthropicMessages splits out the system prompt and converts the
// remaining messages into Anthropic's format, grouping consecutive tool-result
// messages into a single user message with a content array.
func convertToAnthropicMessages(messages []OAIMessage) (systemPrompt string, out []map[string]any) {
	for i := 0; i < len(messages); i++ {
		m := messages[i]

		if m.Role == "system" {
			if s, ok := m.Content.(string); ok {
				systemPrompt = s
			}
			continue
		}

		if m.Role == "tool" {
			var blocks []map[string]any
			for i < len(messages) && messages[i].Role == "tool" {
				t := messages[i]
				content := ""
				if s, ok := t.Content.(string); ok {
					content = s
				}
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": t.ToolCallID,
					"content":     content,
				})
				i++
			}
			i-- // outer loop will increment
			out = append(out, map[string]any{"role": "user", "content": blocks})
			continue
		}

		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			var contentArr []map[string]any
			if s, ok := m.Content.(string); ok && s != "" {
				contentArr = append(contentArr, map[string]any{"type": "text", "text": s})
			}
			for _, tc := range m.ToolCalls {
				var input any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil || input == nil {
					input = map[string]any{}
				}
				contentArr = append(contentArr, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": input,
				})
			}
			out = append(out, map[string]any{"role": "assistant", "content": contentArr})
			continue
		}

		out = append(out, map[string]any{"role": m.Role, "content": convertContentToAnthropic(m.Content)})
	}
	return
}

// convertToAnthropicTools converts OpenAI-format tool definitions to Anthropic format.
func convertToAnthropicTools(tools []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		fn, ok := t["function"].(map[string]any)
		if !ok {
			continue
		}
		atool := map[string]any{
			"name": fn["name"],
		}
		if desc, ok := fn["description"]; ok {
			atool["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			atool["input_schema"] = params
		} else {
			atool["input_schema"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, atool)
	}
	return out
}

func callAnthropicNonStreaming(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
) (OAIMessage, string, TokenUsage, error) {
	systemPrompt, anthropicMsgs := convertToAnthropicMessages(messages)

	reqBody := map[string]any{
		"model":      p.Model,
		"messages":   anthropicMsgs,
		"max_tokens": 4096,
	}
	if systemPrompt != "" {
		// Use the cached-system array format to enable prompt caching on the
		// system prompt prefix. Cache TTL is 5 minutes; refreshed each call.
		reqBody["system"] = anthropicCachedSystem(systemPrompt)
	}
	if len(tools) > 0 {
		// Place cache_control on the last tool so the full tool list is cached.
		reqBody["tools"] = anthropicCachedTools(convertToAnthropicTools(tools))
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return OAIMessage{}, "", TokenUsage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		anthropicBaseURL+"/messages",
		bytes.NewReader(body),
	)
	if err != nil {
		return OAIMessage{}, "", TokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("anthropic-beta", anthropicCachingBeta)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("Anthropic non-streaming request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody)
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("Anthropic error %d: %s", resp.StatusCode, errBody.Error.Message)
	}

	var completion struct {
		Content    []map[string]any `json:"content"`
		StopReason string           `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return OAIMessage{}, "", TokenUsage{}, fmt.Errorf("Anthropic response parse failed: %w", err)
	}

	usage := TokenUsage{
		InputTokens:  completion.Usage.InputTokens,
		OutputTokens: completion.Usage.OutputTokens,
	}

	msg := OAIMessage{Role: "assistant"}
	var textParts []string
	for _, block := range completion.Content {
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			if t, ok := block["text"].(string); ok {
				textParts = append(textParts, t)
			}
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			inputRaw := block["input"]
			argsJSON, _ := json.Marshal(inputRaw)
			msg.ToolCalls = append(msg.ToolCalls, OAIToolCall{
				ID:   id,
				Type: "function",
				Function: OAIFunctionCall{
					Name:      name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	msg.Content = strings.Join(textParts, "")

	finishReason := "stop"
	if completion.StopReason == "tool_use" || len(msg.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}
	return msg, finishReason, usage, nil
}

// streamAnthropicWithToolDetection streams a single Anthropic API call.
// Text tokens are emitted in real time via bc. Tool-use content blocks are
// accumulated from input_json_delta events and assembled on completion.
func streamAnthropicWithToolDetection(
	ctx context.Context,
	p ProviderConfig,
	messages []OAIMessage,
	tools []map[string]any,
	convID string,
	bc Emitter,
) (streamResult, error) {
	systemPrompt, anthropicMsgs := convertToAnthropicMessages(messages)

	reqBody := map[string]any{
		"model":      p.Model,
		"messages":   anthropicMsgs,
		"max_tokens": 4096,
		"stream":     true,
	}
	if systemPrompt != "" {
		// Use the cached-system array format to enable prompt caching on the
		// system prompt prefix. Cache TTL is 5 minutes; refreshed each call.
		reqBody["system"] = anthropicCachedSystem(systemPrompt)
	}
	if len(tools) > 0 {
		// Place cache_control on the last tool so the full tool list is cached.
		reqBody["tools"] = anthropicCachedTools(convertToAnthropicTools(tools))
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return streamResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		anthropicBaseURL+"/messages",
		bytes.NewReader(body),
	)
	if err != nil {
		return streamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("anthropic-beta", anthropicCachingBeta)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return streamResult{}, fmt.Errorf("Anthropic streaming request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return streamResult{}, fmt.Errorf("Anthropic streaming error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bc.Emit(convID, EmitEvent{Type: "token", Role: "assistant", ConvID: convID})

	// toolAccum holds the accumulated state for one tool_use content block.
	type toolAccum struct {
		id   string
		name string
		args strings.Builder
	}

	var (
		fullText   strings.Builder
		usage      TokenUsage
		stopReason string
		accums     = map[int]*toolAccum{}
	)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Message *struct {
				Usage *struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				usage.InputTokens = event.Message.Usage.InputTokens
			}

		case "content_block_start":
			// Register a new tool_use block at the given content index.
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				accums[event.Index] = &toolAccum{
					id:   event.ContentBlock.ID,
					name: event.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			if event.Delta == nil {
				continue
			}
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" {
					fullText.WriteString(event.Delta.Text)
					bc.Emit(convID, EmitEvent{
						Type:    "token",
						Content: event.Delta.Text,
						Role:    "assistant",
						ConvID:  convID,
					})
				}
			case "input_json_delta":
				if acc := accums[event.Index]; acc != nil {
					acc.args.WriteString(event.Delta.PartialJSON)
				}
			}

		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				stopReason = event.Delta.StopReason
			}
			if event.Usage != nil {
				usage.OutputTokens = event.Usage.OutputTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return streamResult{}, fmt.Errorf("Anthropic stream read error: %w", err)
	}

	// Assemble tool calls in content-block index order.
	var toolCalls []OAIToolCall
	for i := 0; i < len(accums); i++ {
		acc, ok := accums[i]
		if !ok {
			break
		}
		toolCalls = append(toolCalls, OAIToolCall{
			ID:   acc.id,
			Type: "function",
			Function: OAIFunctionCall{
				Name:      acc.name,
				Arguments: acc.args.String(),
			},
		})
	}

	finishReason := "stop"
	if stopReason == "tool_use" || len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return streamResult{
		FinalText:    fullText.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}
