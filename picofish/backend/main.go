package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"picofish/api"
	"picofish/api/middleware"
	"picofish/config"
	"picofish/services/llm"
	"picofish/storage"
)

func main() {
	config.Load()

	if err := storage.Init(config.Global.DataDir); err != nil {
		log.Fatalf("storage init: %v", err)
	}

	// Validate LLM connectivity on startup
	log.Printf("PicoFish v3 — validating LLM connection (%s)...", config.Global.LLMBaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := llm.Validate(ctx); err != nil {
		cancel()
		log.Fatalf("startup: %v\n\nCheck your .env — LLM_BASE_URL, LLM_API_KEY, LLM_MODEL", err)
	}
	cancel()
	log.Printf("  LLM OK — model: %s", config.Global.LLMModel)
	if config.Global.EmbedModel != "" {
		log.Printf("  Embed model: %s", config.Global.EmbedModel)
	}
	log.Printf("  Worker pool size: %d | Timeout: %ds",
		config.Global.SimWorkerPoolSize, config.Global.LLMTimeoutSec)

	r := api.NewRouter()
	api.RegisterProjects(r)
	api.RegisterSeeds(r)

	r.Handle("GET", "/health", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": "3.0.0",
			"name":    "PicoFish",
		})
	})

	// Wrap everything in recovery middleware
	var handler http.Handler = r

	frontendDir := config.Global.FrontendDir
	if _, err := os.Stat(frontendDir); err == nil {
		fs := http.FileServer(http.Dir(frontendDir))
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			if len(req.URL.Path) >= 4 && req.URL.Path[:4] == "/api" {
				r.ServeHTTP(w, req)
				return
			}
			if req.URL.Path != "/" {
				if _, err := os.Stat(frontendDir + req.URL.Path); err != nil {
					http.ServeFile(w, req, frontendDir+"/index.html")
					return
				}
			}
			fs.ServeHTTP(w, req)
		})
		handler = mux
	}

	addr := ":" + config.Global.Port
	log.Printf("PicoFish running on http://localhost%s", addr)
	log.Printf("  Data: %s", config.Global.DataDir)

	if err := http.ListenAndServe(addr, middleware.Recovery(handler)); err != nil {
		log.Fatalf("server: %v", err)
	}
}
