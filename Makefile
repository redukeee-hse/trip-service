-include .env
export

MIGRATIONS_DIR := migrations
GOOSE := go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)"

.PHONY: generate run migrate migrate-down migrate-status db docker-build docker-run

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

docker-build:
	docker build -f deploy/Dockerfile -t trip-service:local .

docker-run:
	docker run --rm --network host --env-file .env trip-service:local
