package tools

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── HttpRequest ───────────────────────────────────────────────────────────────

type HttpRequestTool struct{}

func (t *HttpRequestTool) Name() string      { return "http_request" }
func (t *HttpRequestTool) ReadOnly() bool    { return true }
func (t *HttpRequestTool) Description() string {
	return "Выполнить HTTP GET/POST запрос к внешнему URL или внутреннему API."
}
func (t *HttpRequestTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":    map[string]any{"type": "string"},
			"method": map[string]any{"type": "string", "enum": []string{"GET", "POST", "PUT", "DELETE"}, "description": "По умолчанию GET"},
			"body":   map[string]any{"type": "string", "description": "Тело запроса для POST/PUT"},
			"headers": map[string]any{
				"type":        "object",
				"description": "Дополнительные HTTP-заголовки",
			},
		},
		"required": []string{"url"},
	}
}
func (t *HttpRequestTool) Execute(ctx context.Context, _ *sql.DB, args map[string]any) string {
	url := strArg(args, "url")
	if url == "" {
		return errResult("url is required")
	}
	method := strings.ToUpper(strArg(args, "method"))
	if method == "" {
		method = "GET"
	}
	body := strArg(args, "body")

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return errResult(err.Error())
	}
	if hdrs, ok := args["headers"].(map[string]any); ok {
		for k, v := range hdrs {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return errResult(err.Error())
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return okResult(map[string]any{
		"status": resp.StatusCode,
		"body":   string(data),
	})
}

// ── AddContext ────────────────────────────────────────────────────────────────

type AddContextTool struct{}

func (t *AddContextTool) Name() string      { return "add_context" }
func (t *AddContextTool) ReadOnly() bool    { return false }
func (t *AddContextTool) Description() string {
	return "Сохранить важную информацию в базу знаний AI-агента для будущих диалогов."
}
func (t *AddContextTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":   map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
			"type":    map[string]any{"type": "string", "enum": []string{"text", "url", "file"}},
			"source":  map[string]any{"type": "string"},
			"global":  map[string]any{"type": "boolean", "description": "Видна всем (true) или только вам (false)"},
		},
		"required": []string{"title", "content"},
	}
}
func (t *AddContextTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	title := strArg(args, "title")
	content := strArg(args, "content")
	if title == "" || content == "" {
		return errResult("title and content are required")
	}
	global, _ := args["global"].(bool)
	var adminID any = tc.AdminID
	if global {
		adminID = nil
	}
	typ := strArg(args, "type")
	if typ == "" {
		typ = "text"
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := db.ExecContext(ctx,
		`INSERT INTO ai_context_entries (admin_id, title, content, type, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		adminID, title, content, typ, strArg(args, "source"), now, now)
	if err != nil {
		return errResult(err.Error())
	}
	id, _ := res.LastInsertId()
	return okResult(map[string]any{"id": id, "saved": true})
}

// ── SearchContext ─────────────────────────────────────────────────────────────

type SearchContextTool struct{}

func (t *SearchContextTool) Name() string      { return "search_context" }
func (t *SearchContextTool) ReadOnly() bool    { return true }
func (t *SearchContextTool) Description() string {
	return "Поиск по базе знаний AI-агента: заголовок и содержимое записей."
}
func (t *SearchContextTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
		"required": []string{"query"},
	}
}
func (t *SearchContextTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	q := strArg(args, "query")
	if q == "" {
		return errResult("query is required")
	}
	like := "%" + q + "%"
	rows, err := db.QueryContext(ctx,
		`SELECT id, title, content, type, source, created_at FROM ai_context_entries
		 WHERE (admin_id = ? OR admin_id IS NULL) AND (title LIKE ? OR content LIKE ?)
		 ORDER BY id DESC LIMIT 10`,
		tc.AdminID, like, like)
	if err != nil {
		return errResult(err.Error())
	}
	defer rows.Close()
	return okResult(scanRows(rows))
}
