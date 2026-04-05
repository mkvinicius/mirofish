.PHONY: all backend frontend dev clean build-pi

# Dev: run backend + frontend with hot reload
dev:
	@echo "Starting PicoFish..."
	@cp -n .env.example .env 2>/dev/null || true
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	cd backend && go run .

dev-frontend:
	cd frontend && npm install --silent && npm run dev

# Production build
all: build-backend build-frontend

build-backend:
	cd backend && go build -ldflags="-s -w" -o ../dist/picofish .

build-frontend:
	cd frontend && npm install --silent && npm run build && cp -r dist ../dist/frontend

# Cross-compile for Raspberry Pi (ARM64)
build-pi:
	cd backend && GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o ../dist/picofish-arm64 .
	cd frontend && npm install --silent && npm run build
	@echo "Binary: dist/picofish-arm64"
	@echo "Frontend: frontend/dist/"

# Run production binary
run:
	FRONTEND_DIR=./frontend/dist ./dist/picofish

clean:
	rm -rf dist backend/picofish frontend/dist frontend/node_modules

# Install Ollama on Raspberry Pi (ARM64)
install-ollama-pi:
	curl -fsSL https://ollama.ai/install.sh | sh
	ollama pull llama3
