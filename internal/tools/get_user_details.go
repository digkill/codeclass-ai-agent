package tools

import (
	"context"
	"database/sql"
	"fmt"
)

type GetUserDetailsTool struct{}

func (t *GetUserDetailsTool) Name() string      { return "get_user_details" }
func (t *GetUserDetailsTool) ReadOnly() bool    { return true }
func (t *GetUserDetailsTool) Description() string {
	return "Подробная информация об ученике по ID: данные, группы, последний платёж, статус."
}
func (t *GetUserDetailsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id": map[string]any{"type": "integer", "description": "ID ученика"},
		},
		"required": []string{"user_id"},
	}
}

func (t *GetUserDetailsTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	userID := int64Arg(args, "user_id")
	if userID == 0 {
		return errResult("user_id is required")
	}

	row := db.QueryRowContext(ctx,
		`SELECT id, name, last_name, parent_name, email, phone, birthday, franchise_id, created_at
		 FROM users WHERE id = ?`, userID)

	var user struct {
		ID          int64   `json:"id"`
		Name        string  `json:"name"`
		LastName    string  `json:"last_name"`
		ParentName  string  `json:"parent_name"`
		Email       string  `json:"email"`
		Phone       string  `json:"phone"`
		Birthday    *string `json:"birthday"`
		FranchiseID *int64  `json:"franchise_id"`
		CreatedAt   string  `json:"created_at"`
	}
	var birthday sql.NullString
	var fid sql.NullInt64
	if err := row.Scan(&user.ID, &user.Name, &user.LastName, &user.ParentName,
		&user.Email, &user.Phone, &birthday, &fid, &user.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return errResult(fmt.Sprintf("user %d not found", userID))
		}
		return errResult(err.Error())
	}
	if birthday.Valid {
		user.Birthday = &birthday.String
	}
	if fid.Valid {
		user.FranchiseID = &fid.Int64
	}

	// Groups
	gRows, _ := db.QueryContext(ctx,
		`SELECT g.id, g.title, s.title as school
		 FROM group_user gu
		 JOIN groups g ON g.id = gu.group_id
		 JOIN schools s ON s.id = g.school_id
		 WHERE gu.user_id = ?`, userID)
	groups := scanRows(gRows)
	if gRows != nil {
		gRows.Close()
	}

	// Last invoice
	iRow := db.QueryRowContext(ctx,
		`SELECT id, pay_amount, status, created_at FROM invoices
		 WHERE user_id = ? ORDER BY id DESC LIMIT 1`, userID)
	var inv struct {
		ID        int64   `json:"id"`
		Amount    float64 `json:"amount"`
		Status    string  `json:"status"`
		CreatedAt string  `json:"created_at"`
	}
	var lastInvoice any
	if err := iRow.Scan(&inv.ID, &inv.Amount, &inv.Status, &inv.CreatedAt); err == nil {
		lastInvoice = inv
	}

	// Current status
	sRow := db.QueryRowContext(ctx,
		`SELECT st.name, su.created_at FROM status_user su
		 JOIN statuses st ON st.id = su.status_id
		 WHERE su.user_id = ? ORDER BY su.id DESC LIMIT 1`, userID)
	var statusName, statusAt sql.NullString
	var currentStatus any
	if err := sRow.Scan(&statusName, &statusAt); err == nil && statusName.Valid {
		currentStatus = map[string]any{"name": statusName.String, "since": statusAt.String}
	}

	return okResult(map[string]any{
		"user":           user,
		"groups":         groups,
		"last_invoice":   lastInvoice,
		"current_status": currentStatus,
	})
}
