package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/digkill/codeclass-ai-agent/internal/config"
	"github.com/digkill/codeclass-ai-agent/internal/openai"
	"github.com/digkill/codeclass-ai-agent/internal/tools"
)

const maxIterations = 8
const maxHistory = 40

// Run streams the AI agent response over emit callback.
// Events: delta | tool_start | tool_result | done | error
func Run(
	ctx context.Context,
	cfg *config.Config,
	db *sql.DB,
	registry *tools.Registry,
	tc tools.ToolContext,
	conversationID int64,
	userMessage string,
	emit func(openai.Event),
) {
	ctx = tools.WithToolContext(ctx, tc)

	// 1. Persist user message
	now := time.Now().Format("2006-01-02 15:04:05")
	if _, err := db.ExecContext(ctx,
		`INSERT INTO ai_messages (conversation_id, role, content, created_at, updated_at) VALUES (?, 'user', ?, ?, ?)`,
		conversationID, userMessage, now, now); err != nil {
		emit(openai.Event{Type: "error", Message: "db error: " + err.Error()})
		return
	}

	// 2. Build message history
	messages := buildMessages(ctx, cfg, db, conversationID, tc)

	// 3. Build tool schemas (filter write tools if not allowed)
	var toolSchemas []openai.ToolSchema
	for _, t := range registry.All() {
		if !t.ReadOnly() && !tc.CanWrite {
			continue // hide write tools when not permitted
		}
		toolSchemas = append(toolSchemas, openai.ToolSchema{
			Type: "function",
			Function: openai.ToolSchemaFunc{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}

	// 4. Agent loop
	for iter := 0; iter < maxIterations; iter++ {
		result, err := openai.Stream(ctx, cfg, messages, toolSchemas, emit)
		if err != nil {
			emit(openai.Event{Type: "error", Message: err.Error()})
			return
		}

		if result.FinishReason == "tool_calls" && len(result.ToolCalls) > 0 {
			// Persist assistant message with tool_calls
			tcJSON, _ := json.Marshal(result.ToolCalls)
			contentVal := sql.NullString{String: result.Content, Valid: result.Content != ""}
			db.ExecContext(ctx,
				`INSERT INTO ai_messages (conversation_id, role, content, tool_calls, created_at, updated_at) VALUES (?, 'assistant', ?, ?, ?, ?)`,
				conversationID, contentVal, string(tcJSON), now, now)

			// Append assistant message to in-memory context
			var msgContent any = result.Content
			if result.Content == "" {
				msgContent = nil
			}
			messages = append(messages, openai.Message{
				Role:      "assistant",
				Content:   msgContent,
				ToolCalls: result.ToolCalls,
			})

			// Execute each tool
			for _, tc2 := range result.ToolCalls {
				toolName := tc2.Function.Name
				toolCallID := tc2.ID
				emit(openai.Event{Type: "tool_start", Name: toolName, ToolCallID: toolCallID})

				var toolResult string
				var isError bool

				tool, ok := registry.Get(toolName)
				if !ok {
					toolResult = fmt.Sprintf(`{"ok":false,"error":"unknown tool: %s"}`, toolName)
					isError = true
				} else {
					var toolArgs map[string]any
					if err := json.Unmarshal([]byte(tc2.Function.Arguments), &toolArgs); err != nil {
						toolArgs = map[string]any{}
					}
					toolResult = tool.Execute(ctx, db, toolArgs)
					isError = strings.Contains(toolResult, `"ok":false`)
				}

				// Persist tool result
				db.ExecContext(ctx,
					`INSERT INTO ai_messages (conversation_id, role, content, tool_call_id, tool_name, is_error, created_at, updated_at)
					 VALUES (?, 'tool', ?, ?, ?, ?, ?, ?)`,
					conversationID, toolResult, toolCallID, toolName, isError, now, now)

				var resultData any
				json.Unmarshal([]byte(toolResult), &resultData)
				emit(openai.Event{
					Type:       "tool_result",
					Name:       toolName,
					ToolCallID: toolCallID,
					Result:     resultData,
					IsError:    isError,
				})

				messages = append(messages, openai.Message{
					Role:       "tool",
					Content:    toolResult,
					ToolCallID: toolCallID,
				})
			}
			continue
		}

		// Final answer — persist and emit done
		db.ExecContext(ctx,
			`INSERT INTO ai_messages (conversation_id, role, content, created_at, updated_at) VALUES (?, 'assistant', ?, ?, ?)`,
			conversationID, result.Content, now, now)

		// Auto-title conversation on first real answer
		autoTitle(ctx, db, conversationID, userMessage, result.Content)

		emit(openai.Event{Type: "done", Content: result.Content})
		return
	}

	emit(openai.Event{Type: "error", Message: "превышено максимальное количество итераций инструментов"})
}

// ── Message builder ───────────────────────────────────────────────────────────

func buildMessages(ctx context.Context, cfg *config.Config, db *sql.DB, convID int64, tc tools.ToolContext) []openai.Message {
	msgs := []openai.Message{{Role: "system", Content: buildSystemPrompt(cfg, tc)}}

	rows, err := db.QueryContext(ctx,
		`SELECT role, content, tool_calls, tool_call_id FROM ai_messages
		 WHERE conversation_id = ? ORDER BY id`, convID)
	if err != nil {
		log.Printf("buildMessages: %v", err)
		return msgs
	}
	defer rows.Close()

	var history []openai.Message
	for rows.Next() {
		var role string
		var content, toolCallsJSON, toolCallID sql.NullString
		if err := rows.Scan(&role, &content, &toolCallsJSON, &toolCallID); err != nil {
			continue
		}
		msg := openai.Message{Role: role}
		if content.Valid {
			msg.Content = content.String
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			var tcs []openai.ToolCall
			json.Unmarshal([]byte(toolCallsJSON.String), &tcs)
			msg.ToolCalls = tcs
			if !content.Valid || content.String == "" {
				msg.Content = nil
			}
		}
		if toolCallID.Valid {
			msg.ToolCallID = toolCallID.String
		}
		history = append(history, msg)
	}

	// Trim to last maxHistory messages, never cutting mid-tool-sequence
	if len(history) > maxHistory {
		tail := history[len(history)-maxHistory:]
		for len(tail) > 0 && tail[0].Role == "tool" {
			tail = tail[1:]
		}
		msgs = append(msgs, openai.Message{
			Role:    "system",
			Content: "[...часть истории диалога скрыта для экономии контекста...]",
		})
		history = tail
	}

	return append(msgs, history...)
}

// ── System prompt ─────────────────────────────────────────────────────────────

func buildSystemPrompt(cfg *config.Config, tc tools.ToolContext) string {
	var fid string
	if tc.FranchiseID != nil {
		fid = fmt.Sprintf("%d", *tc.FranchiseID)
	} else {
		fid = "нет ограничений"
	}
	writeNote := "read-only: запросы на изменение данных — запросить подтверждение, затем отклонить, объяснив что прав нет."
	if tc.CanWrite {
		writeNote = "полный доступ: запись разрешена, но перед деструктивными операциями требуй явного подтверждения."
	}
	return fmt.Sprintf(`Ты — AI-агент ERP системы CodeClass. Ты умный, точный, полезный.
Пользователь: %s, franchise_id: %s.
Права: %s
Текущее время: %s.

Правила:
- Отвечай на русском, структурированно
- Не придумывай данные — используй инструменты
- Не знаешь ID? Сначала поиск
- Ошибка инструмента? Объясни и предложи альтернативу`,
		tc.AdminName, fid, writeNote, time.Now().Format("02.01.2006 15:04"),
	)
}

// ── Auto title ────────────────────────────────────────────────────────────────

func autoTitle(ctx context.Context, db *sql.DB, convID int64, userMsg, _ string) {
	var title sql.NullString
	db.QueryRowContext(ctx, "SELECT title FROM ai_conversations WHERE id = ?", convID).Scan(&title)
	if title.Valid && title.String != "" {
		return
	}
	t := userMsg
	if len([]rune(t)) > 60 {
		runes := []rune(t)
		t = string(runes[:60]) + "…"
	}
	db.ExecContext(ctx, "UPDATE ai_conversations SET title = ? WHERE id = ?", t, convID)
}
