package tools

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ── SearchInvoices ────────────────────────────────────────────────────────────

type SearchInvoicesTool struct{}

func (t *SearchInvoicesTool) Name() string      { return "search_invoices" }
func (t *SearchInvoicesTool) ReadOnly() bool    { return true }
func (t *SearchInvoicesTool) Description() string {
	return "Поиск счетов/платежей по user_id, названию услуги или статусу."
}
func (t *SearchInvoicesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id":      map[string]any{"type": "integer"},
			"service_name": map[string]any{"type": "string", "description": "Часть названия услуги"},
			"status":       map[string]any{"type": "string", "description": "paid | unpaid | partial | canceled"},
			"limit":        map[string]any{"type": "integer", "description": "По умолчанию 20, максимум 100"},
		},
	}
}

func (t *SearchInvoicesTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	limit := intArgDefault(args, "limit", 20)
	if limit > 100 {
		limit = 100
	}

	query := `SELECT id, user_id, pay_amount, status, service_name, created_at
	          FROM invoices WHERE 1=1`
	var vals []any

	if v := int64Arg(args, "user_id"); v != 0 {
		query += " AND user_id = ?"
		vals = append(vals, v)
	}
	if v := strArg(args, "service_name"); v != "" {
		query += " AND service_name LIKE ?"
		vals = append(vals, "%"+v+"%")
	}
	if v := strArg(args, "status"); v != "" {
		query += " AND status = ?"
		vals = append(vals, v)
	}
	query += " ORDER BY id DESC LIMIT ?"
	vals = append(vals, limit)

	rows, err := db.QueryContext(ctx, query, vals...)
	if err != nil {
		return errResult(err.Error())
	}
	defer rows.Close()
	return okResult(scanRows(rows))
}

// ── CreateInvoice ─────────────────────────────────────────────────────────────

type CreateInvoiceTool struct{}

func (t *CreateInvoiceTool) Name() string      { return "create_invoice" }
func (t *CreateInvoiceTool) ReadOnly() bool    { return false }
func (t *CreateInvoiceTool) Description() string {
	return "Создать счёт/платёж для ученика. Требует явного подтверждения пользователя."
}
func (t *CreateInvoiceTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id":      map[string]any{"type": "integer", "description": "ID ученика"},
			"service_name": map[string]any{"type": "string", "description": "Название услуги/курса"},
			"pay_amount":   map[string]any{"type": "number", "description": "Сумма в рублях"},
		},
		"required": []string{"user_id", "service_name", "pay_amount"},
	}
}

func (t *CreateInvoiceTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	if !tc.CanWrite {
		return errResult("Запись запрещена. Требуются права superroot или флаг AI_AGENT_ALLOW_WRITE.")
	}
	userID := int64Arg(args, "user_id")
	serviceName := strArg(args, "service_name")
	payAmount := floatArg(args, "pay_amount")
	if userID == 0 || serviceName == "" || payAmount <= 0 {
		return errResult("user_id, service_name и pay_amount (>0) обязательны")
	}

	var franchiseID sql.NullInt64
	db.QueryRowContext(ctx, "SELECT franchise_id FROM users WHERE id = ?", userID).Scan(&franchiseID)

	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := db.ExecContext(ctx,
		`INSERT INTO invoices (user_id, franchise_id, service_name, pay_amount, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'unpaid', ?, ?)`,
		userID, franchiseID, serviceName, payAmount, now, now)
	if err != nil {
		return errResult(err.Error())
	}
	id, _ := res.LastInsertId()
	return okResult(map[string]any{"invoice_id": id, "status": "unpaid", "pay_amount": payAmount})
}

// ── UpdateInvoiceStatus ───────────────────────────────────────────────────────

type UpdateInvoiceStatusTool struct{}

func (t *UpdateInvoiceStatusTool) Name() string      { return "update_invoice_status" }
func (t *UpdateInvoiceStatusTool) ReadOnly() bool    { return false }
func (t *UpdateInvoiceStatusTool) Description() string {
	return "Изменить статус счёта. Допустимые статусы: paid, unpaid, partial, canceled. Только после подтверждения."
}
func (t *UpdateInvoiceStatusTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"invoice_id": map[string]any{"type": "integer"},
			"status":     map[string]any{"type": "string", "enum": []string{"paid", "unpaid", "partial", "canceled"}},
		},
		"required": []string{"invoice_id", "status"},
	}
}

func (t *UpdateInvoiceStatusTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	tc := GetToolContext(ctx)
	if !tc.CanWrite {
		return errResult("Запись запрещена. Требуются права superroot или флаг AI_AGENT_ALLOW_WRITE.")
	}
	invoiceID := int64Arg(args, "invoice_id")
	status := strArg(args, "status")
	allowed := map[string]bool{"paid": true, "unpaid": true, "partial": true, "canceled": true}
	if !allowed[status] {
		return errResult(fmt.Sprintf("invalid status: %q", status))
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	var payAt any
	if status == "paid" {
		payAt = now
	}
	_, err := db.ExecContext(ctx,
		"UPDATE invoices SET status = ?, pay_at = ?, updated_at = ? WHERE id = ?",
		status, payAt, now, invoiceID)
	if err != nil {
		return errResult(err.Error())
	}
	return okResult(map[string]any{"invoice_id": invoiceID, "status": status})
}
