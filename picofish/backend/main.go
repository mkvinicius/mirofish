package main

import (
	"log"
	"net/http"
	"os"

	"picofish/api"
	"picofish/config"
	"picofish/storage"
)

func main() {
	config.Load()

	if err := storage.Init(config.Global.DataDir); err != nil {
		log.Fatalf("storage init: %v", err)
	}

	r := api.NewRouter()
	api.RegisterProjects(r)

	r.Handle("GET", "/health", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": "2.0.0",
			"name":    "PicoFish",
		})
	})

	frontendDir := config.Global.FrontendDir
	if _, err := os.Stat(frontendDir); err == nil {
		fs := http.FileServer(http.Dir(frontendDir))
		http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			// API requests go to the router
			if len(req.URL.Path) >= 4 && req.URL.Path[:4] == "/api" {
				r.ServeHTTP(w, req)
				return
			}
			// Static assets served directly; everything else gets index.html (SPA)
			if req.URL.Path != "/" {
				if _, err := os.Stat(frontendDir + req.URL.Path); err != nil {
					http.ServeFile(w, req, frontendDir+"/index.html")
					return
				}
			}
			fs.ServeHTTP(w, req)
		})
	} else {
		http.Handle("/", r)
	}

	addr := ":" + config.Global.Port
	log.Printf("PicoFish v2 running on http://localhost%s", addr)
	log.Printf("  LLM: %s | Model: %s", config.Global.LLMBaseURL, config.Global.LLMModel)
	if config.Global.EmbedModel != "" {
		log.Printf("  Embed model: %s", config.Global.EmbedModel)
	}
	log.Printf("  Data: %s", config.Global.DataDir)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server: %v", err)
	}
}
