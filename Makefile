BINARY=keywatcher
CMD=./main.go
VERSION=$(shell cat VERSION)

.PHONY: dev build test lint migrate-up migrate-down docker-build version-bump helm-deploy compose-up compose-up-d compose-down compose-logs compose-restart

version:
	@echo "Current version: $(VERSION)"

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/$(BINARY) $(CMD)

dev:
	go run $(CMD)

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

migrate-up:
	migrate -path internal/store/migrations -database "$$KEYWATCHER_DB_URL" up

migrate-down:
	migrate -path internal/store/migrations -database "$$KEYWATCHER_DB_URL" down 1

docker-build:
	docker build -f deploy/docker/Dockerfile --build-arg VERSION=$(VERSION) -t keywatcher:$(VERSION) .

version-bump-patch:
	@echo "Bump to next patch version (e.g., v0.1.0 → v0.1.1)"; \
	read -p "Enter new version: " VERSION_NEW; \
	echo "$$VERSION_NEW" > VERSION; \
	echo "Version updated to $$VERSION_NEW"

version-bump-minor:
	@echo "Bump to next minor version (e.g., v0.1.0 → v0.2.0)"; \
	read -p "Enter new version: " VERSION_NEW; \
	echo "$$VERSION_NEW" > VERSION; \
	echo "Version updated to $$VERSION_NEW"

helm-deploy:
	helm upgrade --install keywatcher deploy/helm/ -f deploy/helm/values.yaml

# Run full stack locally with Docker Compose (builds the app image)
compose-up:
	docker compose -f docker-compose.dev.yml up --build

# Run in background
compose-up-d:
	docker compose -f docker-compose.dev.yml up --build -d

# Stop everything
compose-down:
	docker compose -f docker-compose.dev.yml down

# Tail logs from the app only
compose-logs:
	docker compose -f docker-compose.dev.yml logs -f app

# Rebuild and restart only the app container (DB keeps running)
compose-restart:
	docker compose -f docker-compose.dev.yml up --build -d app
