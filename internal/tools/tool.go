package tools

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/digkill/codeclass-ai-agent/internal/config"
)

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	// Schema returns the OpenAI function parameters JSON Schema.
	Schema() map[string]any
	// ReadOnly returns true when the tool never modifies data.
	ReadOnly() bool
	Execute(ctx context.Context, db *sql.DB, args map[string]any) string
}

// toolCtxKey is the context key for ToolContext.
type toolCtxKey struct{}

// ToolContext carries per-request admin information into tools.
type ToolContext struct {
	AdminID     int64
	AdminLevel  int
	AdminName   string
	FranchiseID *int64
	CanWrite    bool   // resolved at handler level
	ERPRoot     string
}

func WithToolContext(ctx context.Context, tc ToolContext) context.Context {
	return context.WithValue(ctx, toolCtxKey{}, tc)
}

func GetToolContext(ctx context.Context) ToolContext {
	if tc, ok := ctx.Value(toolCtxKey{}).(ToolContext); ok {
		return tc
	}
	return ToolContext{}
}

// Registry maps tool names to implementations.
type Registry struct {
	db    *sql.DB
	cfg   *config.Config
	tools map[string]Tool
}

func NewRegistry(db *sql.DB, cfg *config.Config) *Registry {
	r := &Registry{db: db, cfg: cfg, tools: make(map[string]Tool)}
	all := []Tool{
		&SearchUsersTool{},
		&GetUserDetailsTool{},
		&UpdateUserTool{},
		&SetUserStatusTool{},
		&SearchInvoicesTool{},
		&CreateInvoiceTool{},
		&UpdateInvoiceStatusTool{},
		&ListGroupsTool{},
		&CreateGroupTool{},
		&AssignUserToGroupTool{},
		&RemoveUserFromGroupTool{},
		&GetFranchisesTool{},
		&GetSchoolsTool{},
		&GetDashboardStatsTool{},
		&ListTablesTool{},
		&GetTableStructureTool{},
		&RunSqlQueryTool{},
		&ReadFileTool{Root: cfg.ERPRoot},
		&ListFilesTool{Root: cfg.ERPRoot},
		&GrepCodebaseTool{Root: cfg.ERPRoot},
		&HttpRequestTool{},
		&AddContextTool{},
		&SearchContextTool{},
	}
	for _, t := range all {
		r.tools[t.Name()] = t
	}
	return r
}

func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// ─── helpers used by tools ────────────────────────────────────────────────────

func okResult(data any) string {
	b, _ := json.Marshal(map[string]any{"ok": true, "data": data})
	return string(b)
}

func errResult(msg string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	return string(b)
}

func strPtr(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

func intPtr(i sql.NullInt64) any {
	if i.Valid {
		return i.Int64
	}
	return nil
}
