package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/digkill/codeclass-ai-agent/internal/agent"
	"github.com/digkill/codeclass-ai-agent/internal/config"
	"github.com/digkill/codeclass-ai-agent/internal/openai"
	"github.com/digkill/codeclass-ai-agent/internal/rdb"
	"github.com/digkill/codeclass-ai-agent/internal/tools"
)

// StartWorkers launches n goroutines that consume jobs from Redis.
// Each worker BRPOP from ai:jobs, runs the agent, publishes SSE events
// back to the per-job pub/sub channel consumed by the HTTP handler.
func StartWorkers(ctx context.Context, n int, cfg *config.Config, rd *redis.Client, db *sql.DB, registry *tools.Registry) {
	for i := 0; i < n; i++ {
		go worker(ctx, i, cfg, rd, db, registry)
	}
	log.Printf("queue: %d workers started", n)
}

func worker(ctx context.Context, id int, cfg *config.Config, rd *redis.Client, db *sql.DB, registry *tools.Registry) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Block for up to 5s, then loop (checks ctx.Done)
		res, err := rd.BRPop(ctx, 5*time.Second, rdb.QueueKey).Result()
		if err != nil {
			if err != redis.Nil && ctx.Err() == nil {
				log.Printf("worker[%d]: BRPop error: %v", id, err)
				time.Sleep(time.Second)
			}
			continue
		}

		// res[0] = key, res[1] = payload
		var job ChatJob
		if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
			log.Printf("worker[%d]: bad job payload: %v", id, err)
			continue
		}

		log.Printf("worker[%d]: job=%s conv=%d admin=%d", id, job.ID, job.ConversationID, job.AdminID)
		processJob(ctx, id, cfg, rd, db, registry, job)
	}
}

func processJob(ctx context.Context, workerID int, cfg *config.Config, rd *redis.Client, db *sql.DB, registry *tools.Registry, job ChatJob) {
	channel := rdb.EventChannel(job.ID)

	// Mark job as processing
	rd.HSet(ctx, rdb.JobKey(job.ID), "status", "processing", "worker", workerID)
	rd.Expire(ctx, rdb.JobKey(job.ID), 10*time.Minute)

	tc := tools.ToolContext{
		AdminID:     job.AdminID,
		AdminLevel:  job.AdminLevel,
		AdminName:   job.AdminName,
		FranchiseID: job.FranchiseID,
		CanWrite:    cfg.AllowWrite || (job.AdminLevel >= cfg.SuperRootLevel),
		ERPRoot:     cfg.ERPRoot,
	}

	// Emit publishes SSE event JSON to the per-job Redis pub/sub channel.
	emit := func(evt openai.Event) {
		data, _ := json.Marshal(evt)
		if err := rd.Publish(ctx, channel, string(data)).Err(); err != nil {
			log.Printf("worker[%d]: publish error: %v", workerID, err)
		}
	}

	agent.Run(ctx, cfg, db, registry, tc, job.ConversationID, job.Message, emit)
	rd.HSet(ctx, rdb.JobKey(job.ID), "status", "done")
}
