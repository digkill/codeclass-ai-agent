package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/digkill/codeclass-ai-agent/internal/config"
)

func checkSecret(cfg *config.Config, r *http.Request) bool {
	return cfg.Secret == "" || r.Header.Get("X-Agent-Secret") == cfg.Secret
}

func adminIDFromReq(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.Header.Get("X-Admin-Id"), 10, 64)
	return id
}

func pathID(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(r.PathValue(key), 10, 64)
	return id
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}
