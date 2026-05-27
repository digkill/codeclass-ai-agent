package tools

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type SetUserStatusTool struct{}

func (t *SetUserStatusTool) Name() string      { return "set_user_status" }
func (t *SetUserStatusTool) ReadOnly() bool    { return false }
func (t *SetUserStatusTool) Description() string {
	return "Установить статус ученика. Сначала узнай нужный status_id через run_sql_query (SELECT id, name FROM statuses). Только после подтверждения."
}
func (t *SetUserStatusTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id":   map[string]any{"type": "integer", "description": "ID ученика"},
			"status_id": map[string]any{"type": "integer", "description": "ID статуса из таблицы statuses"},
		},
		"required": []string{"user_id", "status_id"},
	}
}

func (t *SetUserStatusTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	if !tc.CanWrite {
		return errResult("Запись запрещена. Требуются права superroot или флаг AI_AGENT_ALLOW_WRITE.")
	}
	userID := int64Arg(args, "user_id")
	statusID := int64Arg(args, "status_id")
	if userID == 0 || statusID == 0 {
		return errResult("user_id and status_id are required")
	}

	var statusName string
	if err := db.QueryRowContext(ctx, "SELECT name FROM statuses WHERE id = ?", statusID).Scan(&statusName); err != nil {
		return errResult(fmt.Sprintf("status %d not found", statusID))
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := db.ExecContext(ctx,
		"INSERT INTO status_user (user_id, status_id, created_at, updated_at) VALUES (?, ?, ?, ?)",
		userID, statusID, now, now)
	if err != nil {
		return errResult(err.Error())
	}
	return okResult(map[string]any{"user_id": userID, "status": statusName})
}
