package tools

import (
	"context"
	"database/sql"
	"strings"
)

type UpdateUserTool struct{}

func (t *UpdateUserTool) Name() string      { return "update_user" }
func (t *UpdateUserTool) ReadOnly() bool    { return false }
func (t *UpdateUserTool) Description() string {
	return "Обновить данные ученика: имя, фамилию, имя родителя, email, телефон, дату рождения, комментарий. Только после явного подтверждения."
}
func (t *UpdateUserTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id":     map[string]any{"type": "integer", "description": "ID ученика"},
			"name":        map[string]any{"type": "string"},
			"last_name":   map[string]any{"type": "string"},
			"parent_name": map[string]any{"type": "string"},
			"email":       map[string]any{"type": "string"},
			"phone":       map[string]any{"type": "string"},
			"birthday":    map[string]any{"type": "string", "description": "YYYY-MM-DD"},
			"comment":     map[string]any{"type": "string"},
		},
		"required": []string{"user_id"},
	}
}

func (t *UpdateUserTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	if !tc.CanWrite {
		return errResult("Запись запрещена. Требуются права superroot или флаг AI_AGENT_ALLOW_WRITE.")
	}
	userID := int64Arg(args, "user_id")
	if userID == 0 {
		return errResult("user_id is required")
	}

	fields := []string{}
	vals := []any{}
	for _, col := range []string{"name", "last_name", "parent_name", "email", "phone", "birthday", "comment"} {
		if v, ok := args[col]; ok && v != nil {
			fields = append(fields, col+" = ?")
			vals = append(vals, v)
		}
	}
	if len(fields) == 0 {
		return errResult("no fields to update")
	}
	vals = append(vals, userID)
	_, err := db.ExecContext(ctx, "UPDATE users SET "+strings.Join(fields, ", ")+" WHERE id = ?", vals...)
	if err != nil {
		return errResult(err.Error())
	}
	return okResult(map[string]any{"updated": true, "user_id": userID})
}
