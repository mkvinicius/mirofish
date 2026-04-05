package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	LLMBaseURL  string
	LLMAPIKey   string
	LLMModel    string
	EmbedModel  string // embedding model, defaults to LLMModel if empty
	DataDir     string
	Port        string
	FrontendDir string
}

var Global *Config

func Load() {
	_ = godotenv.Load("../.env")

	Global = &Config{
		LLMBaseURL:  getEnv("LLM_BASE_URL", "http://localhost:11434/v1"),
		LLMAPIKey:   getEnv("LLM_API_KEY", "ollama"),
		LLMModel:    getEnv("LLM_MODEL", "llama3"),
		EmbedModel:  getEnv("EMBED_MODEL", ""),
		DataDir:     getEnv("DATA_DIR", "./data"),
		Port:        getEnv("PORT", "5002"),
		FrontendDir: getEnv("FRONTEND_DIR", "../frontend/dist"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
