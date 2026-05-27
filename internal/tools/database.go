package tools

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// ── ListTables ────────────────────────────────────────────────────────────────

type ListTablesTool struct{}

func (t *ListTablesTool) Name() string      { return "list_tables" }
func (t *ListTablesTool) ReadOnly() bool    { return true }
func (t *ListTablesTool) Description() string {
	return "Список всех таблиц БД ERP. Используй перед get_table_structure или run_sql_query."
}
func (t *ListTablesTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *ListTablesTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	rows, err := db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		return errResult(err.Error())
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	return okResult(tables)
}

// ── GetTableStructure ─────────────────────────────────────────────────────────

type GetTableStructureTool struct{}

func (t *GetTableStructureTool) Name() string      { return "get_table_structure" }
func (t *GetTableStructureTool) ReadOnly() bool    { return true }
func (t *GetTableStructureTool) Description() string {
	return "Структура таблицы БД: колонки, типы, индексы. Используй перед написанием SQL-запроса."
}
func (t *GetTableStructureTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"table": map[string]any{"type": "string", "description": "Название таблицы"},
		},
		"required": []string{"table"},
	}
}
func (t *GetTableStructureTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	table := strArg(args, "table")
	if table == "" {
		return errResult("table is required")
	}
	// Sanitize table name
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(table) {
		return errResult("invalid table name")
	}

	rows, err := db.QueryContext(ctx, "DESCRIBE `"+table+"`")
	if err != nil {
		return errResult(fmt.Sprintf("table %q not found: %v", table, err))
	}
	defer rows.Close()
	columns := scanRows(rows)

	iRows, err := db.QueryContext(ctx, "SHOW INDEX FROM `"+table+"`")
	var indexes []map[string]any
	if err == nil {
		defer iRows.Close()
		indexes = scanRows(iRows)
	}

	return okResult(map[string]any{"columns": columns, "indexes": indexes})
}

// ── RunSqlQuery ───────────────────────────────────────────────────────────────

var blockedReadKeywords = regexp.MustCompile(
	`(?i)\b(INSERT|UPDATE|DELETE|DROP|TRUNCATE|ALTER|CREATE|RENAME|REPLACE|GRANT|REVOKE|LOAD|EXEC|CALL)\b`)

var blockedWriteKeywords = regexp.MustCompile(
	`(?i)\b(DROP|TRUNCATE|ALTER|CREATE|RENAME|GRANT|REVOKE|LOAD|EXEC|CALL)\b`)

type RunSqlQueryTool struct{}

func (t *RunSqlQueryTool) Name() string      { return "run_sql_query" }
func (t *RunSqlQueryTool) ReadOnly() bool    { return true }
func (t *RunSqlQueryTool) Description() string {
	return "Выполнить SELECT-запрос к БД ERP. Только чтение. Для аналитики и данных, не покрытых другими инструментами."
}
func (t *RunSqlQueryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sql": map[string]any{"type": "string", "description": "SQL SELECT-запрос"},
			"bindings": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Параметры для ? в запросе",
			},
		},
		"required": []string{"sql"},
	}
}
func (t *RunSqlQueryTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	q := strings.TrimSpace(strArg(args, "sql"))
	if q == "" {
		return errResult("sql is required")
	}
	upper := strings.ToUpper(strings.TrimSpace(q))
	if !strings.HasPrefix(upper, "SELECT") {
		return errResult("only SELECT queries are allowed")
	}
	if blockedReadKeywords.MatchString(q) {
		return errResult("query contains forbidden keywords")
	}

	var bindings []any
	if b, ok := args["bindings"].([]any); ok {
		bindings = b
	}

	rows, err := db.QueryContext(ctx, q, bindings...)
	if err != nil {
		return errResult("SQL error: " + err.Error())
	}
	defer rows.Close()
	result := scanRows(rows)
	truncated := false
	if len(result) > 200 {
		result = result[:200]
		truncated = true
	}
	return okResult(map[string]any{"rows": result, "truncated": truncated})
}

// ── RunSqlWrite ───────────────────────────────────────────────────────────────
// Write-only version: INSERT/UPDATE/DELETE. Requires CanWrite permission.

type RunSqlWriteTool struct{}

func (t *RunSqlWriteTool) Name() string      { return "run_sql_write" }
func (t *RunSqlWriteTool) ReadOnly() bool    { return false }
func (t *RunSqlWriteTool) Description() string {
	return "Выполнить INSERT/UPDATE/DELETE запрос к БД ERP. Доступно только при наличии прав записи. Использовать осторожно — требуй подтверждения у пользователя."
}
func (t *RunSqlWriteTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sql": map[string]any{"type": "string", "description": "SQL запрос (INSERT/UPDATE/DELETE)"},
			"bindings": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Параметры для ? в запросе",
			},
		},
		"required": []string{"sql"},
	}
}
func (t *RunSqlWriteTool) Execute(ctx context.Context, db *sql.DB, args map[string]any) string {
	q := strings.TrimSpace(strArg(args, "sql"))
	if q == "" {
		return errResult("sql is required")
	}
	upper := strings.ToUpper(strings.TrimSpace(q))
	if strings.HasPrefix(upper, "SELECT") {
		return errResult("use run_sql_query for SELECT queries")
	}
	if blockedWriteKeywords.MatchString(q) {
		return errResult("query contains forbidden keywords (DROP/TRUNCATE/ALTER etc.)")
	}

	var bindings []any
	if b, ok := args["bindings"].([]any); ok {
		bindings = b
	}

	res, err := db.ExecContext(ctx, q, bindings...)
	if err != nil {
		return errResult("SQL error: " + err.Error())
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	return okResult(map[string]any{"rows_affected": affected, "last_insert_id": lastID})
}
