package tools

import (
	"database/sql"
	"fmt"
)

// ── SQL row scanner ───────────────────────────────────────────────────────────

// scanRows converts sql.Rows into []map[string]any.
func scanRows(rows *sql.Rows) []map[string]any {
	if rows == nil {
		return []map[string]any{}
	}
	cols, err := rows.Columns()
	if err != nil {
		return []map[string]any{}
	}
	var result []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			v := vals[i]
			// Convert []byte to string for readability
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[col] = v
		}
		result = append(result, row)
	}
	if result == nil {
		return []map[string]any{}
	}
	return result
}

// ── Arg helpers ───────────────────────────────────────────────────────────────

func strArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func int64Arg(args map[string]any, key string) int64 {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func intArgDefault(args map[string]any, key string, def int) int {
	v := int64Arg(args, key)
	if v == 0 {
		return def
	}
	return int(v)
}

func floatArg(args map[string]any, key string) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func nullableInt(args map[string]any, key string) any {
	v := int64Arg(args, key)
	if v == 0 {
		return nil
	}
	return v
}
