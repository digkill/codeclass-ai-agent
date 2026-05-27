package tools

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ── ListGroups ────────────────────────────────────────────────────────────────

type ListGroupsTool struct{}

func (t *ListGroupsTool) Name() string      { return "list_groups" }
func (t *ListGroupsTool) ReadOnly() bool    { return true }
func (t *ListGroupsTool) Description() string {
	return "Список учебных групп с фильтрацией по школе, франшизе или названию."
}
func (t *ListGroupsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"school_id":    map[string]any{"type": "integer"},
			"franchise_id": map[string]any{"type": "integer"},
			"title":        map[string]any{"type": "string", "description": "Поиск по названию группы"},
			"limit":        map[string]any{"type": "integer", "description": "По умолчанию 20"},
		},
	}
}

func (t *ListGroupsTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	limit := intArgDefault(args, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	query := `SELECT g.id, g.title, g.school_id, s.title as school_title, s.franchise_id,
	           a.name as teacher_name,
	           (SELECT COUNT(*) FROM group_user gu WHERE gu.group_id = g.id) as students_count
	          FROM groups g
	          JOIN schools s ON s.id = g.school_id
	          LEFT JOIN admins a ON a.id = g.teacher_id
	          WHERE 1=1`
	var vals []any
	if v := int64Arg(args, "school_id"); v != 0 {
		query += " AND g.school_id = ?"
		vals = append(vals, v)
	}
	if v := int64Arg(args, "franchise_id"); v != 0 {
		query += " AND s.franchise_id = ?"
		vals = append(vals, v)
	}
	if v := strArg(args, "title"); v != "" {
		query += " AND g.title LIKE ?"
		vals = append(vals, "%"+v+"%")
	}
	query += " ORDER BY g.id DESC LIMIT ?"
	vals = append(vals, limit)

	rows, err := db.QueryContext(ctx, query, vals...)
	if err != nil {
		return errResult(err.Error())
	}
	defer rows.Close()
	return okResult(scanRows(rows))
}

// ── CreateGroup ───────────────────────────────────────────────────────────────

type CreateGroupTool struct{}

func (t *CreateGroupTool) Name() string      { return "create_group" }
func (t *CreateGroupTool) ReadOnly() bool    { return false }
func (t *CreateGroupTool) Description() string {
	return "Создать новую учебную группу. Требует явного подтверждения."
}
func (t *CreateGroupTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":         map[string]any{"type": "string", "description": "Название группы"},
			"school_id":     map[string]any{"type": "integer"},
			"franchise_id":  map[string]any{"type": "integer"},
			"course_id":     map[string]any{"type": "integer"},
			"teacher_id":    map[string]any{"type": "integer"},
			"number_places": map[string]any{"type": "integer", "description": "Макс. мест (по умолчанию 12)"},
		},
		"required": []string{"title", "school_id", "franchise_id"},
	}
}

func (t *CreateGroupTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	if !tc.CanWrite {
		return errResult("Запись запрещена. Требуются права superroot или флаг AI_AGENT_ALLOW_WRITE.")
	}
	title := strArg(args, "title")
	schoolID := int64Arg(args, "school_id")
	franchiseID := int64Arg(args, "franchise_id")
	if title == "" || schoolID == 0 || franchiseID == 0 {
		return errResult("title, school_id и franchise_id обязательны")
	}
	places := intArgDefault(args, "number_places", 12)
	now := time.Now().Format("2006-01-02 15:04:05")

	res, err := db.ExecContext(ctx,
		`INSERT INTO groups (title, school_id, franchise_id, course_id, teacher_id, number_places, status, student_Joined, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, 0, ?, ?)`,
		title, schoolID, franchiseID,
		nullableInt(args, "course_id"), nullableInt(args, "teacher_id"),
		places, now, now)
	if err != nil {
		return errResult(err.Error())
	}
	id, _ := res.LastInsertId()
	return okResult(map[string]any{"group_id": id, "title": title})
}

// ── AssignUserToGroup ─────────────────────────────────────────────────────────

type AssignUserToGroupTool struct{}

func (t *AssignUserToGroupTool) Name() string      { return "assign_user_to_group" }
func (t *AssignUserToGroupTool) ReadOnly() bool    { return false }
func (t *AssignUserToGroupTool) Description() string {
	return "Добавить ученика в группу. Только после явного подтверждения."
}
func (t *AssignUserToGroupTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id":  map[string]any{"type": "integer"},
			"group_id": map[string]any{"type": "integer"},
		},
		"required": []string{"user_id", "group_id"},
	}
}

func (t *AssignUserToGroupTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	if !tc.CanWrite {
		return errResult("Запись запрещена. Требуются права superroot или флаг AI_AGENT_ALLOW_WRITE.")
	}
	userID := int64Arg(args, "user_id")
	groupID := int64Arg(args, "group_id")
	if userID == 0 || groupID == 0 {
		return errResult("user_id and group_id are required")
	}

	var exists int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM group_user WHERE user_id=? AND group_id=?", userID, groupID).Scan(&exists)
	if exists > 0 {
		return errResult(fmt.Sprintf("user %d is already in group %d", userID, groupID))
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := db.ExecContext(ctx,
		"INSERT INTO group_user (user_id, group_id, created_at, updated_at) VALUES (?, ?, ?, ?)",
		userID, groupID, now, now)
	if err != nil {
		return errResult(err.Error())
	}
	return okResult(map[string]any{"user_id": userID, "group_id": groupID, "assigned": true})
}

// ── RemoveUserFromGroup ───────────────────────────────────────────────────────

type RemoveUserFromGroupTool struct{}

func (t *RemoveUserFromGroupTool) Name() string      { return "remove_user_from_group" }
func (t *RemoveUserFromGroupTool) ReadOnly() bool    { return false }
func (t *RemoveUserFromGroupTool) Description() string {
	return "Удалить ученика из группы. Только после явного подтверждения."
}
func (t *RemoveUserFromGroupTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id":  map[string]any{"type": "integer"},
			"group_id": map[string]any{"type": "integer"},
		},
		"required": []string{"user_id", "group_id"},
	}
}

func (t *RemoveUserFromGroupTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	if !tc.CanWrite {
		return errResult("Запись запрещена. Требуются права superroot или флаг AI_AGENT_ALLOW_WRITE.")
	}
	userID := int64Arg(args, "user_id")
	groupID := int64Arg(args, "group_id")
	if userID == 0 || groupID == 0 {
		return errResult("user_id and group_id are required")
	}
	res, err := db.ExecContext(ctx,
		"DELETE FROM group_user WHERE user_id = ? AND group_id = ?", userID, groupID)
	if err != nil {
		return errResult(err.Error())
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errResult(fmt.Sprintf("user %d is not in group %d", userID, groupID))
	}
	return okResult(map[string]any{"removed": true})
}
