package handler

import (
	"net/http"

	"github.com/digkill/codeclass-ai-agent/internal/config"
	"github.com/digkill/codeclass-ai-agent/internal/tools"
)

func NewToolsAPIHandler(cfg *config.Config, registry *tools.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkSecret(cfg, r) {
			jsonErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var list []map[string]any
		for _, t := range registry.All() {
			list = append(list, map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"read_only":   t.ReadOnly(),
			})
		}
		jsonOK(w, list)
	}
}
