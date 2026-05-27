package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/digkill/codeclass-ai-agent/internal/config"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"` // string or nil
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ToolSchema struct {
	Type     string         `json:"type"`
	Function ToolSchemaFunc `json:"function"`
}

type ToolSchemaFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Event is emitted to the caller during streaming.
type Event struct {
	Type       string `json:"type"` // delta | tool_start | tool_result | done | error
	Content    string `json:"content,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Result     any    `json:"result,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	Message    string `json:"message,omitempty"`
}

// StreamResult is returned when streaming completes.
type StreamResult struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
}

// ── Streaming client ──────────────────────────────────────────────────────────

func Stream(
	ctx context.Context,
	cfg *config.Config,
	messages []Message,
	tools []ToolSchema,
	emit func(Event),
) (StreamResult, error) {
	body := map[string]any{
		"model":                  cfg.OpenAIModel,
		"messages":               messages,
		"temperature":            0.3,
		"max_completion_tokens":  4096,
		"stream":                 true,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return StreamResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.OpenAIURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return StreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return StreamResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody struct {
			Error struct{ Message string } `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody)
		msg := errBody.Error.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return StreamResult{}, fmt.Errorf("OpenAI API error %d: %s", resp.StatusCode, msg)
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	var contentBuf strings.Builder
	toolMap := map[int]*ToolCall{}
	finishReason := "stop"

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   *string    `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]

		if ch.FinishReason != nil && *ch.FinishReason != "" {
			finishReason = *ch.FinishReason
		}

		if ch.Delta.Content != nil && *ch.Delta.Content != "" {
			contentBuf.WriteString(*ch.Delta.Content)
			emit(Event{Type: "delta", Content: *ch.Delta.Content})
		}

		for _, tc := range ch.Delta.ToolCalls {
			idx := tc.Index
			if _, ok := toolMap[idx]; !ok {
				toolMap[idx] = &ToolCall{Type: "function"}
			}
			t := toolMap[idx]
			if tc.ID != "" {
				t.ID = tc.ID
			}
			if tc.Function.Name != "" {
				t.Function.Name += tc.Function.Name
			}
			t.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return StreamResult{}, err
	}

	var toolCalls []ToolCall
	for i := 0; i < len(toolMap); i++ {
		if tc, ok := toolMap[i]; ok {
			toolCalls = append(toolCalls, *tc)
		}
	}

	return StreamResult{
		Content:      contentBuf.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
	}, nil
}
