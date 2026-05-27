package handler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/digkill/codeclass-ai-agent/internal/agent"
	"github.com/digkill/codeclass-ai-agent/internal/config"
	"github.com/digkill/codeclass-ai-agent/internal/openai"
	"github.com/digkill/codeclass-ai-agent/internal/queue"
	"github.com/digkill/codeclass-ai-agent/internal/rdb"
	"github.com/digkill/codeclass-ai-agent/internal/tools"
)

type ChatHandler struct {
	cfg      *config.Config
	db       *sql.DB
	registry *tools.Registry
	rd       *redis.Client // nil when Redis is disabled
}

func NewChatHandler(cfg *config.Config, db *sql.DB, registry *tools.Registry, rd *redis.Client) http.HandlerFunc {
	h := &ChatHandler{cfg: cfg, db: db, registry: registry, rd: rd}
	return h.ServeHTTP
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ── Auth ──────────────────────────────────────────────────────────────────
	if h.cfg.Secret != "" && r.Header.Get("X-Agent-Secret") != h.cfg.Secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// ── Parse body ────────────────────────────────────────────────────────────
	var req struct {
		ConversationID int64  `json:"conversation_id"`
		Message        string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" || req.ConversationID == 0 {
		http.Error(w, "invalid request: conversation_id and message are required", http.StatusBadRequest)
		return
	}

	// ── Admin context ─────────────────────────────────────────────────────────
	adminID, _ := strconv.ParseInt(r.Header.Get("X-Admin-Id"), 10, 64)
	adminLevel, _ := strconv.Atoi(r.Header.Get("X-Admin-Level"))
	adminName := r.Header.Get("X-Admin-Name")
	if adminName == "" {
		adminName = "Admin"
	}
	var franchiseID *int64
	if fStr := r.Header.Get("X-Franchise-Id"); fStr != "" {
		if fid, err := strconv.ParseInt(fStr, 10, 64); err == nil && fid > 0 {
			franchiseID = &fid
		}
	}
	canWrite := h.cfg.AllowWrite || (adminLevel >= h.cfg.SuperRootLevel)

	// ── Verify conversation ownership ─────────────────────────────────────────
	var convAdminID int64
	err := h.db.QueryRowContext(r.Context(),
		"SELECT admin_id FROM ai_conversations WHERE id = ?", req.ConversationID).Scan(&convAdminID)
	if err == sql.ErrNoRows {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	if err != nil || convAdminID != adminID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// ── SSE headers ───────────────────────────────────────────────────────────
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	writeLine := func(data []byte) {
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	sseEvent := func(evt openai.Event) {
		if data, err := json.Marshal(evt); err == nil {
			writeLine(data)
		}
	}

	log.Printf("chat: conv=%d admin=%d level=%d write=%v queue=%v",
		req.ConversationID, adminID, adminLevel, canWrite, h.rd != nil && h.cfg.RedisEnabled)

	// ── Dispatch: queue or direct ─────────────────────────────────────────────
	if h.rd != nil && h.cfg.RedisEnabled {
		h.runViaQueue(r.Context(), req.ConversationID, req.Message,
			adminID, adminLevel, adminName, franchiseID, canWrite, sseEvent)
	} else {
		tc := tools.ToolContext{
			AdminID: adminID, AdminLevel: adminLevel, AdminName: adminName,
			FranchiseID: franchiseID, CanWrite: canWrite, ERPRoot: h.cfg.ERPRoot,
		}
		agent.Run(r.Context(), h.cfg, h.db, h.registry, tc, req.ConversationID, req.Message, sseEvent)
	}
}

// runViaQueue pushes the job to Redis and subscribes to the per-job pub/sub
// channel, streaming events back to the HTTP client as they arrive.
func (h *ChatHandler) runViaQueue(
	ctx context.Context,
	convID int64, message string,
	adminID int64, adminLevel int, adminName string,
	franchiseID *int64, canWrite bool,
	sseEvent func(openai.Event),
) {
	jobID := newJobID()
	job := queue.ChatJob{
		ID:             jobID,
		ConversationID: convID,
		Message:        message,
		AdminID:        adminID,
		AdminLevel:     adminLevel,
		AdminName:      adminName,
		FranchiseID:    franchiseID,
		CreatedAt:      time.Now().Unix(),
	}
	payload, _ := json.Marshal(job)

	// Store job metadata
	h.rd.HSet(ctx, rdb.JobKey(jobID), "status", "queued", "conv_id", convID)
	h.rd.Expire(ctx, rdb.JobKey(jobID), 10*time.Minute)

	// Push to queue
	if err := h.rd.LPush(ctx, rdb.QueueKey, string(payload)).Err(); err != nil {
		sseEvent(openai.Event{Type: "error", Message: "queue error: " + err.Error()})
		return
	}

	// Subscribe to per-job event channel and relay to SSE
	sub := h.rd.Subscribe(ctx, rdb.EventChannel(jobID))
	defer sub.Close()

	ch := sub.Channel()
	timeout := time.After(3 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return

		case <-timeout:
			sseEvent(openai.Event{Type: "error", Message: "timeout waiting for response"})
			return

		case msg, ok := <-ch:
			if !ok {
				return
			}
			var evt openai.Event
			if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
				continue
			}
			sseEvent(evt)
			if evt.Type == "done" || evt.Type == "error" {
				return
			}
		}
	}
}

func newJobID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
