DATABASE_URL ?= postgres://nebula:nebula@localhost:5432/nebula?sslmode=disable
JWT_SECRET ?= dev-only-secret-change-me
COMPOSE_FILE := deployments/compose/docker-compose.yml

.PHONY: build run test vet migrate-up migrate-down compose-up compose-down

build:
	go build -o bin/nebula-api ./cmd/nebula-api
	go build -o bin/nebula-agent ./cmd/nebula-agent

run: build
	NEBULA_DATABASE_URL=$(DATABASE_URL) NEBULA_JWT_SECRET=$(JWT_SECRET) ./bin/nebula-api

test:
	go test ./...

vet:
	go vet ./...

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down 1

compose-up:
	docker compose -f $(COMPOSE_FILE) up -d --build

compose-down:
	docker compose -f $(COMPOSE_FILE) down
