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

	// Health check
	r.Handle("GET", "/health", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": "1.0.0",
			"name":    "PicoFish",
		})
	})

	// Serve Svelte frontend (SPA)
	frontendDir := config.Global.FrontendDir
	if _, err := os.Stat(frontendDir); err == nil {
		http.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(frontendDir+"/assets"))))
		http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			// For SPA: serve index.html for non-API, non-asset paths
			if len(req.URL.Path) > 4 && req.URL.Path[:4] == "/api" {
				r.ServeHTTP(w, req)
				return
			}
			http.ServeFile(w, req, frontendDir+"/index.html")
		})
		http.Handle("/api/", r)
	} else {
		// No frontend built — serve API only
		http.Handle("/", r)
	}

	addr := ":" + config.Global.Port
	log.Printf("🐟 PicoFish running on http://localhost%s", addr)
	log.Printf("   LLM: %s | Model: %s", config.Global.LLMBaseURL, config.Global.LLMModel)
	log.Printf("   Data: %s", config.Global.DataDir)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server: %v", err)
	}
}
