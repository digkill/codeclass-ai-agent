package tools

import (
	"context"
	"database/sql"
	"fmt"
)

type SearchUsersTool struct{}

func (t *SearchUsersTool) Name() string      { return "search_users" }
func (t *SearchUsersTool) ReadOnly() bool    { return true }
func (t *SearchUsersTool) Description() string {
	return "Поиск учеников/клиентов в ERP по имени, email или телефону."
}
func (t *SearchUsersTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Имя, email или телефон"},
			"limit": map[string]any{"type": "integer", "description": "Максимум результатов (по умолчанию 10, максимум 50)"},
		},
		"required": []string{"query"},
	}
}

func (t *SearchUsersTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	q := strArg(args, "query")
	if q == "" {
		return errResult("query is required")
	}
	limit := intArgDefault(args, "limit", 10)
	if limit > 50 {
		limit = 50
	}
	like := "%" + q + "%"
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, last_name, email, phone, created_at
		 FROM users
		 WHERE name LIKE ? OR last_name LIKE ? OR email LIKE ? OR phone LIKE ?
		 ORDER BY id DESC LIMIT ?`,
		like, like, like, like, limit,
	)
	if err != nil {
		return errResult(fmt.Sprintf("query error: %v", err))
	}
	defer rows.Close()
	return okResult(scanRows(rows))
}
