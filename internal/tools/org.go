package tools

import (
	"context"
	"database/sql"
	"time"
)

// ── GetFranchises ─────────────────────────────────────────────────────────────

type GetFranchisesTool struct{}

func (t *GetFranchisesTool) Name() string      { return "get_franchises_list" }
func (t *GetFranchisesTool) ReadOnly() bool    { return true }
func (t *GetFranchisesTool) Description() string {
	return "Список франшиз: id, название, город. Используй для поиска franchise_id."
}
func (t *GetFranchisesTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *GetFranchisesTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	rows, err := db.QueryContext(ctx, "SELECT id, title, city FROM franchises ORDER BY id")
	if err != nil {
		return errResult(err.Error())
	}
	defer rows.Close()
	return okResult(scanRows(rows))
}

// ── GetSchools ────────────────────────────────────────────────────────────────

type GetSchoolsTool struct{}

func (t *GetSchoolsTool) Name() string      { return "get_schools_list" }
func (t *GetSchoolsTool) ReadOnly() bool    { return true }
func (t *GetSchoolsTool) Description() string {
	return "Список школ с фильтрацией по франшизе. Возвращает id, название, franchise_id."
}
func (t *GetSchoolsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"franchise_id": map[string]any{"type": "integer"},
		},
	}
}
func (t *GetSchoolsTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	query := "SELECT id, title, franchise_id FROM schools WHERE 1=1"
	var vals []any
	if v := int64Arg(args, "franchise_id"); v != 0 {
		query += " AND franchise_id = ?"
		vals = append(vals, v)
	}
	query += " ORDER BY id"
	rows, err := db.QueryContext(ctx, query, vals...)
	if err != nil {
		return errResult(err.Error())
	}
	defer rows.Close()
	return okResult(scanRows(rows))
}

// ── GetDashboardStats ─────────────────────────────────────────────────────────

type GetDashboardStatsTool struct{}

func (t *GetDashboardStatsTool) Name() string      { return "get_dashboard_stats" }
func (t *GetDashboardStatsTool) ReadOnly() bool    { return true }
func (t *GetDashboardStatsTool) Description() string {
	return "Сводная статистика ERP: ученики, активные группы, платежи за месяц, долги."
}
func (t *GetDashboardStatsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"franchise_id": map[string]any{"type": "integer", "description": "Фильтр по франшизе (опционально)"},
		},
	}
}
func (t *GetDashboardStatsTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	fid := int64Arg(args, "franchise_id")
	filt := ""
	var fv []any
	if fid != 0 {
		filt = " AND franchise_id = ?"
		fv = []any{fid}
	}

	now := time.Now()
	monthStart := now.Format("2006-01-01 00:00:00")
	prevStart := now.AddDate(0, -1, 0).Format("2006-01-01 00:00:00")
	prevEnd := now.AddDate(0, 0, -now.Day()).Format("2006-01-02 23:59:59")

	scan1 := func(q string, vals ...any) float64 {
		var v float64
		db.QueryRowContext(ctx, q, vals...).Scan(&v)
		return v
	}

	users := scan1("SELECT COUNT(*) FROM users WHERE 1=1"+filt, fv...)
	activeGroups := scan1("SELECT COUNT(*) FROM groups WHERE status=1"+filt, fv...)

	iBase := "SELECT COALESCE(SUM(pay_amount),0) FROM invoices WHERE status='paid'"
	paidThis := scan1(iBase+" AND pay_at >= ?"+filt, append([]any{monthStart}, fv...)...)
	paidPrev := scan1(iBase+" AND pay_at BETWEEN ? AND ?"+filt, append([]any{prevStart, prevEnd}, fv...)...)

	unpaidBase := "SELECT COUNT(*) FROM invoices WHERE status='unpaid'"
	unpaidCount := scan1(unpaidBase+filt, fv...)
	unpaidAmt := scan1("SELECT COALESCE(SUM(pay_amount),0) FROM invoices WHERE status='unpaid'"+filt, fv...)

	return okResult(map[string]any{
		"users_total":      users,
		"groups_active":    activeGroups,
		"paid_this_month":  paidThis,
		"paid_last_month":  paidPrev,
		"unpaid_invoices":  unpaidCount,
		"unpaid_amount":    unpaidAmt,
		"period_from":      monthStart,
	})
}
