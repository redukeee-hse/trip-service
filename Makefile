-include .env
export

MIGRATIONS_DIR := migrations
GOOSE := go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)"

.PHONY: generate run migrate migrate-down migrate-status db

generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-package api \
		-include-operation-ids createTrip,getTrip,finishTrip,health,ready \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml

run:
	go build -o bin/trip-service ./cmd/trip-service && ./bin/trip-service

migrate:
	@$(GOOSE) up

migrate-down:
	@$(GOOSE) reset

migrate-status:
	@$(GOOSE) status

db:
	@psql "$(DATABASE_URL)"
