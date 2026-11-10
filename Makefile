-include .env
export

MIGRATIONS_DIR := migrations
GOOSE := go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)"

.PHONY: generate run migrate migrate-down migrate-status db docker-build docker-run test test-integration lint

generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-package api \
		-include-operation-ids createTrip,getTrip,finishTrip,health,ready \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml
	go generate ./...

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

COVERAGE_FILE := coverage.out
# Пакеты, для которых ТЗ требует покрытие не ниже 50%.
COVERAGE_CORE := /internal/(usecase|domain)/

# Unit-тесты всего проекта с -race. В конце печатается покрытие:
# отдельно для usecase + domain (требование ТЗ) и общее.
test:
	go test -race -count=1 -coverprofile=$(COVERAGE_FILE) ./...
	@grep -E '^mode:|$(COVERAGE_CORE)' $(COVERAGE_FILE) > $(COVERAGE_FILE).core
	@printf 'Покрытие usecase + domain: '; go tool cover -func=$(COVERAGE_FILE).core | tail -1 | awk '{print $$NF}'
	@printf 'Покрытие всего проекта:    '; go tool cover -func=$(COVERAGE_FILE) | tail -1 | awk '{print $$NF}'
	@rm -f $(COVERAGE_FILE).core

lint:
	golangci-lint run ./...

# Интеграционные тесты репозитория: PostgreSQL поднимается через testcontainers-go
# (нужен запущенный Docker), миграции накатываются из migrations/.
test-integration:
	go test -tags=integration -race -count=2 ./internal/repository/...
