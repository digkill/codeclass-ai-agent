package rdb

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/digkill/codeclass-ai-agent/internal/config"
)

func New(cfg *config.Config) (*redis.Client, error) {
	var opts *redis.Options
	var err error

	if cfg.RedisURL != "" {
		opts, err = redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("parse REDIS_URL: %w", err)
		}
	} else {
		opts = &redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}
	}

	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return client, nil
}

// Keys used by the queue system.
const (
	QueueKey   = "ai:jobs"            // LIST — job payloads (LPUSH / BRPOP)
	EventsKey  = "ai:events:%s"       // PubSub channel per job (PUBLISH / SUBSCRIBE)
	JobMetaKey = "ai:job:%s"          // HASH — job state/metadata (TTL 10 min)
)

func EventChannel(jobID string) string { return fmt.Sprintf(EventsKey, jobID) }
func JobKey(jobID string) string       { return fmt.Sprintf(JobMetaKey, jobID) }
