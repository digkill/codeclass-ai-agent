package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"github.com/digkill/codeclass-ai-agent/internal/config"
	"github.com/digkill/codeclass-ai-agent/internal/db"
	"github.com/digkill/codeclass-ai-agent/internal/handler"
	"github.com/digkill/codeclass-ai-agent/internal/queue"
	"github.com/digkill/codeclass-ai-agent/internal/rdb"
	"github.com/digkill/codeclass-ai-agent/internal/tools"
)

func main() {
	cfg := config.Load()

	// ── DB ────────────────────────────────────────────────────────────────────
	database, err := db.New(cfg)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	// ── Redis (optional) ──────────────────────────────────────────────────────
	var rd *redis.Client
	if cfg.RedisEnabled {
		rd, err = rdb.New(cfg)
		if err != nil {
			log.Printf("redis: unavailable (%v) — running in direct mode", err)
			rd = nil
		} else {
			log.Printf("redis: connected (%s)", cfg.RedisHost+":"+cfg.RedisPort)
		}
	}

	// ── Tools & registry ──────────────────────────────────────────────────────
	registry := tools.NewRegistry(database, cfg)

	// ── Context for graceful shutdown ─────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Workers (only when Redis is available) ────────────────────────────────
	if rd != nil && cfg.RedisEnabled {
		queue.StartWorkers(ctx, cfg.Workers, cfg, rd, database, registry)
	}

	// ── Handlers ──────────────────────────────────────────────────────────────
	convH := handler.NewConversationsHandler(cfg, database)
	ctxH := handler.NewContextHandler(cfg, database)

	// ── HTTP ──────────────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		mode := "direct"
		if rd != nil {
			mode = "queue"
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"mode":"` + mode + `","model":"` + cfg.OpenAIModel + `"}`))
	})

	// Chat (SSE)
	mux.HandleFunc("POST /chat", handler.NewChatHandler(cfg, database, registry, rd))

	// Conversations
	mux.HandleFunc("GET /conversations", convH.List)
	mux.HandleFunc("POST /conversations", convH.Create)
	mux.HandleFunc("GET /conversations/{id}", convH.Get)
	mux.HandleFunc("PATCH /conversations/{id}", convH.Update)
	mux.HandleFunc("DELETE /conversations/{id}", convH.Delete)
	mux.HandleFunc("GET /conversations/{id}/export", convH.Export)

	// Context entries
	mux.HandleFunc("GET /context", ctxH.Index)
	mux.HandleFunc("POST /context", ctxH.Store)
	mux.HandleFunc("PUT /context/{id}", ctxH.Update)
	mux.HandleFunc("DELETE /context/{id}", ctxH.Delete)

	// Tools list
	mux.HandleFunc("GET /tools", handler.NewToolsAPIHandler(cfg, registry))

	addr := "127.0.0.1:" + cfg.Port
	log.Printf("ai-agent listening on %s  model=%s  write=%v  mode=%s  workers=%d",
		addr, cfg.OpenAIModel, cfg.AllowWrite,
		map[bool]string{true: "queue", false: "direct"}[rd != nil],
		cfg.Workers,
	)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down…")
}
