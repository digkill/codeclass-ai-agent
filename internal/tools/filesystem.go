package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var allowedExts = map[string]bool{
	".php": true, ".js": true, ".vue": true, ".ts": true, ".tsx": true,
	".json": true, ".yaml": true, ".yml": true, ".md": true, ".txt": true,
	".go": true, ".env.example": true, ".sql": true, ".html": true,
}

func safeJoin(root, rel string) (string, error) {
	abs := filepath.Join(root, rel)
	clean := filepath.Clean(abs)
	if !strings.HasPrefix(clean, filepath.Clean(root)) {
		return "", fmt.Errorf("path outside project root")
	}
	return clean, nil
}

// ── ReadFile ──────────────────────────────────────────────────────────────────

type ReadFileTool struct{ Root string }

func (t *ReadFileTool) Name() string      { return "read_file" }
func (t *ReadFileTool) ReadOnly() bool    { return true }
func (t *ReadFileTool) Description() string {
	return "Прочитать содержимое файла кодовой базы ERP. Путь относительно корня проекта."
}
func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Относительный путь к файлу"},
			"offset": map[string]any{"type": "integer", "description": "Строка начала (0-based)"},
			"limit":  map[string]any{"type": "integer", "description": "Количество строк (по умолчанию 200)"},
		},
		"required": []string{"path"},
	}
}
func (t *ReadFileTool) Execute(ctx context.Context, _ *sql.DB, args map[string]any) string {
	rel := strArg(args, "path")
	abs, err := safeJoin(t.Root, rel)
	if err != nil {
		return errResult(err.Error())
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if !allowedExts[ext] {
		return errResult(fmt.Sprintf("extension %q not allowed", ext))
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return errResult(err.Error())
	}
	lines := strings.Split(string(data), "\n")
	offset := intArgDefault(args, "offset", 0)
	limit := intArgDefault(args, "limit", 200)
	if offset >= len(lines) {
		offset = 0
	}
	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}
	return okResult(map[string]any{
		"path":        rel,
		"total_lines": len(lines),
		"offset":      offset,
		"content":     strings.Join(lines[offset:end], "\n"),
	})
}

// ── ListFiles ─────────────────────────────────────────────────────────────────

type ListFilesTool struct{ Root string }

func (t *ListFilesTool) Name() string      { return "list_files" }
func (t *ListFilesTool) ReadOnly() bool    { return true }
func (t *ListFilesTool) Description() string {
	return "Список файлов в директории кодовой базы ERP."
}
func (t *ListFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Относительный путь к директории (по умолчанию корень)"},
		},
	}
}
func (t *ListFilesTool) Execute(ctx context.Context, _ *sql.DB, args map[string]any) string {
	rel := strArg(args, "path")
	if rel == "" {
		rel = "."
	}
	abs, err := safeJoin(t.Root, rel)
	if err != nil {
		return errResult(err.Error())
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return errResult(err.Error())
	}
	var result []map[string]any
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".env.example" {
			continue
		}
		result = append(result, map[string]any{
			"name": name,
			"type": map[bool]string{true: "dir", false: "file"}[e.IsDir()],
		})
	}
	return okResult(result)
}

// ── GrepCodebase ──────────────────────────────────────────────────────────────

type GrepCodebaseTool struct{ Root string }

func (t *GrepCodebaseTool) Name() string      { return "grep_codebase" }
func (t *GrepCodebaseTool) ReadOnly() bool    { return true }
func (t *GrepCodebaseTool) Description() string {
	return "Поиск строки/паттерна в файлах кодовой базы ERP."
}
func (t *GrepCodebaseTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":   map[string]any{"type": "string", "description": "Регулярное выражение или строка для поиска"},
			"path":      map[string]any{"type": "string", "description": "Ограничить поиск директорией (опционально)"},
			"extension": map[string]any{"type": "string", "description": "Расширение файлов: php, js, vue ..."},
			"limit":     map[string]any{"type": "integer", "description": "Максимум совпадений (по умолчанию 30)"},
		},
		"required": []string{"pattern"},
	}
}
func (t *GrepCodebaseTool) Execute(ctx context.Context, _ *sql.DB, args map[string]any) string {
	pattern := strArg(args, "pattern")
	if pattern == "" {
		return errResult("pattern is required")
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return errResult("invalid pattern: " + err.Error())
	}

	searchRoot := t.Root
	if rel := strArg(args, "path"); rel != "" {
		if abs, e := safeJoin(t.Root, rel); e == nil {
			searchRoot = abs
		}
	}
	filterExt := strArg(args, "extension")
	limit := intArgDefault(args, "limit", 30)

	type match struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	var matches []match

	skip := map[string]bool{
		"vendor": true, "node_modules": true, ".git": true,
		"storage": true, "bootstrap/cache": true,
	}

	var walk func(dir string) bool
	walk = func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return true
		}
		for _, e := range entries {
			if len(matches) >= limit {
				return false
			}
			name := e.Name()
			rel, _ := filepath.Rel(t.Root, filepath.Join(dir, name))
			top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
			if skip[top] || skip[name] {
				continue
			}
			fullPath := filepath.Join(dir, name)
			if e.IsDir() {
				if !walk(fullPath) {
					return false
				}
				continue
			}
			ext := strings.TrimPrefix(filepath.Ext(name), ".")
			if filterExt != "" && ext != filterExt {
				continue
			}
			if !allowedExts["."+ext] {
				continue
			}
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			for i, line := range strings.Split(string(data), "\n") {
				if len(matches) >= limit {
					return false
				}
				if re.MatchString(line) {
					relPath, _ := filepath.Rel(t.Root, fullPath)
					matches = append(matches, match{File: relPath, Line: i + 1, Text: strings.TrimSpace(line)})
				}
			}
		}
		return true
	}
	walk(searchRoot)
	return okResult(map[string]any{"matches": matches, "total": len(matches)})
}
