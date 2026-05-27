package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/digkill/codeclass-ai-agent/internal/config"
)

type ConversationsHandler struct {
	cfg *config.Config
	db  *sql.DB
}

func NewConversationsHandler(cfg *config.Config, db *sql.DB) *ConversationsHandler {
	return &ConversationsHandler{cfg: cfg, db: db}
}

// List GET /conversations
func (h *ConversationsHandler) List(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, title, model, created_at, updated_at
		 FROM ai_conversations WHERE admin_id = ? ORDER BY updated_at DESC LIMIT 50`, aID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var list []map[string]any
	for rows.Next() {
		var id int64
		var title sql.NullString
		var model, createdAt, updatedAt string
		rows.Scan(&id, &title, &model, &createdAt, &updatedAt)
		list = append(list, map[string]any{
			"id":         id,
			"title":      nullStr(title),
			"model":      model,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	if list == nil {
		list = []map[string]any{}
	}
	jsonOK(w, list)
}

// Create POST /conversations
func (h *ConversationsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := h.db.ExecContext(r.Context(),
		`INSERT INTO ai_conversations (admin_id, title, model, created_at, updated_at) VALUES (?, NULL, ?, ?, ?)`,
		aID, h.cfg.OpenAIModel, now, now)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonOK(w, map[string]any{
		"id":         id,
		"admin_id":   aID,
		"title":      nil,
		"model":      h.cfg.OpenAIModel,
		"created_at": now,
		"updated_at": now,
	})
}

// Get GET /conversations/{id}
func (h *ConversationsHandler) Get(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	id := pathID(r, "id")

	var convAdminID int64
	var title sql.NullString
	var model, createdAt, updatedAt string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT admin_id, title, model, created_at, updated_at FROM ai_conversations WHERE id = ?`, id).
		Scan(&convAdminID, &title, &model, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		jsonErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err != nil || convAdminID != aID {
		jsonErr(w, http.StatusForbidden, "forbidden")
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, role, content, tool_calls, tool_call_id, tool_name, is_error, created_at
		 FROM ai_messages WHERE conversation_id = ? ORDER BY id`, id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var messages []map[string]any
	for rows.Next() {
		var msgID int64
		var role, createdAt string
		var content, toolCalls, toolCallID, toolName sql.NullString
		var isError bool
		rows.Scan(&msgID, &role, &content, &toolCalls, &toolCallID, &toolName, &isError, &createdAt)
		messages = append(messages, map[string]any{
			"id":              msgID,
			"role":            role,
			"content":         nullStr(content),
			"tool_calls":      nullStr(toolCalls),
			"tool_call_id":    nullStr(toolCallID),
			"tool_name":       nullStr(toolName),
			"is_error":        isError,
			"created_at":      createdAt,
		})
	}
	if messages == nil {
		messages = []map[string]any{}
	}

	jsonOK(w, map[string]any{
		"conversation": map[string]any{
			"id":         id,
			"admin_id":   convAdminID,
			"title":      nullStr(title),
			"model":      model,
			"created_at": createdAt,
			"updated_at": updatedAt,
		},
		"messages": messages,
	})
}

// Update PATCH /conversations/{id}
func (h *ConversationsHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	id := pathID(r, "id")

	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		jsonErr(w, http.StatusBadRequest, "title is required")
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := h.db.ExecContext(r.Context(),
		`UPDATE ai_conversations SET title = ?, updated_at = ? WHERE id = ? AND admin_id = ?`,
		req.Title, now, id, aID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	jsonOK(w, map[string]any{"id": id, "title": req.Title, "updated_at": now})
}

// Delete DELETE /conversations/{id}
func (h *ConversationsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	id := pathID(r, "id")

	h.db.ExecContext(r.Context(), `DELETE FROM ai_messages WHERE conversation_id = ?`, id)
	res, err := h.db.ExecContext(r.Context(),
		`DELETE FROM ai_conversations WHERE id = ? AND admin_id = ?`, id, aID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	jsonOK(w, map[string]any{"deleted": true})
}

// Export GET /conversations/{id}/export
func (h *ConversationsHandler) Export(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	id := pathID(r, "id")

	var title sql.NullString
	var convAdminID int64
	err := h.db.QueryRowContext(r.Context(),
		`SELECT admin_id, title FROM ai_conversations WHERE id = ?`, id).
		Scan(&convAdminID, &title)
	if err == sql.ErrNoRows || convAdminID != aID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT role, content, tool_name FROM ai_messages WHERE conversation_id = ? ORDER BY id`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	heading := fmt.Sprintf("Диалог #%d", id)
	if title.Valid && title.String != "" {
		heading = title.String
	}
	lines := []string{"# " + heading, "", "Экспортировано: " + time.Now().Format("02.01.2006 15:04"), ""}

	for rows.Next() {
		var role string
		var content, toolName sql.NullString
		rows.Scan(&role, &content, &toolName)
		switch role {
		case "user":
			lines = append(lines, "**Вы:**", content.String, "")
		case "assistant":
			if content.Valid && content.String != "" {
				lines = append(lines, "**AI:**", content.String, "")
			}
		case "tool":
			name := ""
			if toolName.Valid {
				name = toolName.String
			}
			lines = append(lines, "_[Инструмент: "+name+"]_", "")
		}
	}

	filename := fmt.Sprintf("ai-conversation-%d-%s.md", id, time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/markdown; charset=UTF-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Write([]byte(strings.Join(lines, "\n")))
}

func nullStr(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}
