package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/digkill/codeclass-ai-agent/internal/config"
)

type ContextHandler struct {
	cfg *config.Config
	db  *sql.DB
}

func NewContextHandler(cfg *config.Config, db *sql.DB) *ContextHandler {
	return &ContextHandler{cfg: cfg, db: db}
}

// Index GET /context
func (h *ContextHandler) Index(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, admin_id, title, content, type, source, is_active, created_at, updated_at
		 FROM ai_context_entries
		 WHERE is_active = 1 AND (admin_id = ? OR admin_id IS NULL)
		 ORDER BY id DESC`, aID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var list []map[string]any
	for rows.Next() {
		var id int64
		var adminID sql.NullInt64
		var title, content, typ string
		var source sql.NullString
		var isActive bool
		var createdAt, updatedAt string
		rows.Scan(&id, &adminID, &title, &content, &typ, &source, &isActive, &createdAt, &updatedAt)
		list = append(list, map[string]any{
			"id":         id,
			"admin_id":   nullInt(adminID),
			"title":      title,
			"content":    content,
			"type":       typ,
			"source":     nullStr(source),
			"is_active":  isActive,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	if list == nil {
		list = []map[string]any{}
	}
	jsonOK(w, list)
}

// Store POST /context
func (h *ContextHandler) Store(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Type    string `json:"type"`
		Source  string `json:"source"`
		Global  bool   `json:"global"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" || req.Content == "" {
		jsonErr(w, http.StatusBadRequest, "title and content are required")
		return
	}
	if req.Type == "" {
		req.Type = "text"
	}
	var ownerID any = aID
	if req.Global {
		ownerID = nil
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := h.db.ExecContext(r.Context(),
		`INSERT INTO ai_context_entries (admin_id, title, content, type, source, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		ownerID, req.Title, req.Content, req.Type, nullableStr(req.Source), now, now)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonOK(w, map[string]any{"id": id, "title": req.Title, "type": req.Type})
}

// Update PUT /context/{id}
func (h *ContextHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	id := pathID(r, "id")

	var req struct {
		Title    *string `json:"title"`
		Content  *string `json:"content"`
		IsActive *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	if req.Title != nil {
		h.db.ExecContext(r.Context(),
			`UPDATE ai_context_entries SET title = ?, updated_at = ?
			 WHERE id = ? AND (admin_id = ? OR admin_id IS NULL)`,
			*req.Title, now, id, aID)
	}
	if req.Content != nil {
		h.db.ExecContext(r.Context(),
			`UPDATE ai_context_entries SET content = ?, updated_at = ?
			 WHERE id = ? AND (admin_id = ? OR admin_id IS NULL)`,
			*req.Content, now, id, aID)
	}
	if req.IsActive != nil {
		h.db.ExecContext(r.Context(),
			`UPDATE ai_context_entries SET is_active = ?, updated_at = ?
			 WHERE id = ? AND (admin_id = ? OR admin_id IS NULL)`,
			*req.IsActive, now, id, aID)
	}
	jsonOK(w, map[string]any{"id": id, "updated": true})
}

// Delete DELETE /context/{id}
func (h *ContextHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !checkSecret(h.cfg, r) {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := adminIDFromReq(r)
	id := pathID(r, "id")

	res, err := h.db.ExecContext(r.Context(),
		`DELETE FROM ai_context_entries WHERE id = ? AND admin_id = ?`, id, aID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(w, http.StatusNotFound, "not found")
		return
	}
	jsonOK(w, map[string]any{"deleted": true})
}

func nullInt(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
