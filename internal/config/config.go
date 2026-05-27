package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// HTTP
	Port   string
	Secret string

	// DB
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string

	// OpenAI
	OpenAIKey   string
	OpenAIURL   string
	OpenAIModel string

	// Redis
	RedisURL      string // takes priority if set, e.g. redis://127.0.0.1:6379
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int
	RedisEnabled  bool

	// Worker pool
	Workers int

	// ERP filesystem root (for read_file / grep_codebase tools)
	ERPRoot string

	// Write permission
	AllowWrite     bool
	SuperRootLevel int
}

func Load() *Config {
	// Load .env from: 1) current dir, 2) executable dir
	for _, path := range envCandidates() {
		if err := godotenv.Load(path); err == nil {
			log.Printf("config: loaded %s", path)
			break
		}
	}

	cfg := &Config{
		Port:           getenv("AI_AGENT_PORT", "8901"),
		Secret:         getenv("AI_AGENT_SECRET", ""),
		DBHost:         getenv("DB_HOST", "127.0.0.1"),
		DBPort:         getenv("DB_PORT", "3306"),
		DBName:         getenv("DB_DATABASE", "codeclass"),
		DBUser:         getenv("DB_USERNAME", "root"),
		DBPassword:     unquote(getenv("DB_PASSWORD", "")),
		OpenAIKey:      getenv("OPENAI_API_KEY", ""),
		OpenAIURL:      strings.TrimRight(getenv("OPENAI_API_URL", "https://api.openai.com/v1"), "/"),
		OpenAIModel:    getenv("OPENAI_AGENT_MODEL", getenv("OPENAI_MODEL", "gpt-4o")),
		RedisURL:       getenv("REDIS_URL", ""),
		RedisHost:      getenv("REDIS_HOST", "127.0.0.1"),
		RedisPort:      getenv("REDIS_PORT", "6379"),
		RedisPassword:  getenv("REDIS_PASSWORD", ""),
		RedisDB:        parseInt(getenv("REDIS_DB", "0"), 0),
		RedisEnabled:   parseBool(getenv("AI_AGENT_QUEUE", "true")),
		Workers:        parseInt(getenv("AI_AGENT_WORKERS", "4"), 4),
		ERPRoot:        getenvOrCwd("ERP_ROOT"),
		AllowWrite:     parseBool(getenv("AI_AGENT_ALLOW_WRITE", "false")),
		SuperRootLevel: parseInt(getenv("AI_AGENT_SUPERROOT_LEVEL", "100"), 100),
	}
	return cfg
}

func envCandidates() []string {
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{filepath.Join(cwd, ".env")}
	if exeDir != cwd {
		candidates = append(candidates, filepath.Join(exeDir, ".env"))
	}
	return candidates
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvOrCwd(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	cwd, _ := os.Getwd()
	return cwd
}

func unquote(s string) string {
	return strings.Trim(s, `'"`)
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

func parseInt(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}
