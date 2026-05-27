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

// ── GetSchedules ──────────────────────────────────────────────────────────────

type GetSchedulesTool struct{}

func (t *GetSchedulesTool) Name() string      { return "get_schedules" }
func (t *GetSchedulesTool) ReadOnly() bool    { return true }
func (t *GetSchedulesTool) Description() string {
	return "Получить расписание занятий по группе или школе. Можно указать диапазон дат."
}
func (t *GetSchedulesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"group_id":  map[string]any{"type": "integer", "description": "ID группы"},
			"school_id": map[string]any{"type": "integer", "description": "ID школы"},
			"date_from": map[string]any{"type": "string", "description": "Дата начала (YYYY-MM-DD)"},
			"date_to":   map[string]any{"type": "string", "description": "Дата конца (YYYY-MM-DD)"},
			"limit":     map[string]any{"type": "integer", "description": "Лимит (по умолчанию 30, макс 100)"},
		},
	}
}
func (t *GetSchedulesTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	limit := intArgDefault(args, "limit", 30)
	if limit > 100 {
		limit = 100
	}
	q := `SELECT s.id, s.group_id, g.title AS group_title, sc.title AS school_title,
	             l.title AS lesson_title, s.date_start,
	             DATE_FORMAT(s.date_start, '%d.%m.%Y %H:%i') AS date_formatted
	      FROM schedules s
	      JOIN groups g ON g.id = s.group_id
	      JOIN schools sc ON sc.id = g.school_id
	      LEFT JOIN lessons l ON l.id = s.lesson_id
	      WHERE 1=1`
	var vals []any
	if v := int64Arg(args, "group_id"); v != 0 {
		q += " AND s.group_id = ?"
		vals = append(vals, v)
	}
	if v := int64Arg(args, "school_id"); v != 0 {
		q += " AND g.school_id = ?"
		vals = append(vals, v)
	}
	if v := strArg(args, "date_from"); v != "" {
		q += " AND s.date_start >= ?"
		vals = append(vals, v)
	}
	if v := strArg(args, "date_to"); v != "" {
		q += " AND s.date_start <= ?"
		vals = append(vals, v+" 23:59:59")
	}
	q += " ORDER BY s.date_start LIMIT ?"
	vals = append(vals, limit)

	rows, err := db.QueryContext(ctx, q, vals...)
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
