package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// LLM
	LLMBaseURL    string
	LLMAPIKey     string
	LLMModel      string
	EmbedModel    string // embedding model, defaults to LLMModel if empty
	LLMTimeoutSec int    // per-call LLM timeout (default 30s)
	LLMMaxRetries int    // max LLM call retries (default 3)

	// Server
	DataDir     string
	Port        string
	FrontendDir string

	// Simulation
	MaxAgents              int
	DefaultSimulationHours int
	DeadHourThreshold      float64
	EchoChamberBoost       float64
	OppStanceSuppression   float64
	RecencyDecayRate       float64
	PopularityLogWeight    float64
	SimWorkerPoolSize      int

	// Memory
	BeliefDecayPerHour          float64
	ContradictionThreshold      float64
	MaxEpisodicMemoryIndividual int
	MaxEpisodicMemoryGroup      int

	// Report
	ReportCritiqueIterations int
	MinToolCallsPerSection   int

	// Graph
	GraphMaxHops int
}

var Global *Config

func Load() {
	_ = godotenv.Load("../.env")

	Global = &Config{
		// LLM
		LLMBaseURL:    getEnv("LLM_BASE_URL", "http://localhost:11434/v1"),
		LLMAPIKey:     getEnv("LLM_API_KEY", "ollama"),
		LLMModel:      getEnv("LLM_MODEL", "llama3"),
		EmbedModel:    getEnv("EMBED_MODEL", ""),
		LLMTimeoutSec: getEnvInt("LLM_TIMEOUT_SECONDS", 30),
		LLMMaxRetries: getEnvInt("LLM_MAX_RETRIES", 3),

		// Server
		DataDir:     getEnv("DATA_DIR", "./data"),
		Port:        getEnv("PORT", "5002"),
		FrontendDir: getEnv("FRONTEND_DIR", "../frontend/dist"),

		// Simulation
		MaxAgents:              getEnvInt("MAX_AGENTS", 100),
		DefaultSimulationHours: getEnvInt("DEFAULT_SIMULATION_HOURS", 24),
		DeadHourThreshold:      getEnvFloat("DEAD_HOUR_THRESHOLD", 0.1),
		EchoChamberBoost:       getEnvFloat("ECHO_CHAMBER_BOOST", 1.8),
		OppStanceSuppression:   getEnvFloat("OPPOSITE_STANCE_SUPPRESSION", 0.4),
		RecencyDecayRate:       getEnvFloat("RECENCY_DECAY_RATE", 0.15),
		PopularityLogWeight:    getEnvFloat("POPULARITY_LOG_WEIGHT", 0.3),
		SimWorkerPoolSize:      getEnvInt("SIMULATION_WORKER_POOL_SIZE", 10),

		// Memory
		BeliefDecayPerHour:          getEnvFloat("BELIEF_DECAY_PER_HOUR", 0.05),
		ContradictionThreshold:      getEnvFloat("CONTRADICTION_THRESHOLD", 0.3),
		MaxEpisodicMemoryIndividual: getEnvInt("MAX_EPISODIC_MEMORY_INDIVIDUAL", 100),
		MaxEpisodicMemoryGroup:      getEnvInt("MAX_EPISODIC_MEMORY_GROUP", 10),

		// Report
		ReportCritiqueIterations: getEnvInt("REPORT_CRITIQUE_ITERATIONS", 2),
		MinToolCallsPerSection:   getEnvInt("MIN_TOOL_CALLS_PER_SECTION", 3),

		// Graph
		GraphMaxHops: getEnvInt("GRAPH_MAX_HOPS", 3),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
