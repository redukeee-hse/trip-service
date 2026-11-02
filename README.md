# Trip Service — Go backend для сервиса поездок

HTTP API для создания, получения и завершения поездок. Учебный проект курса «Разработка микросервисов на Go».

**[Перейти к реализации и инструкции запуска → homework/01](https://github.com/redukeee-hse/trip-service/tree/homework/01)**

В основной ветке находится заготовка курса. Реализация сервиса, миграции и Dockerfile находятся в ветке `homework/01`.

## Стек

Go · Chi · PostgreSQL · pgx / pgxpool · Squirrel · goose · OpenAPI · oapi-codegen · Docker.

## Что реализовано

- Создание, получение и завершение поездок через HTTP API.
- Транзакционная запись поездки и истории статусов.
- Создание с `Idempotency-Key`: повтор возвращает созданную поездку, конфликт данных — HTTP 409.
- Защита от двух активных поездок одного водителя частичным уникальным индексом PostgreSQL.
- Конкурентное завершение с `SELECT ... FOR UPDATE`.
- Пул соединений, таймауты, graceful shutdown и `/health`, `/ready`.
- Многостадийная Docker-сборка.

## Запуск

Нужны Go 1.26+, Make и окружение курса с `tripgoctl`.

```bash
git clone --branch homework/01 https://github.com/redukeee-hse/trip-service.git
cd trip-service
tripgoctl cluster start
tripgoctl environment start
make migrate
make run
```

Проверка готовности:

```bash
curl http://localhost:8080/ready
```

Полные требования, переменные окружения, примеры запросов и объяснение архитектурных решений: [README ветки homework/01](https://github.com/redukeee-hse/trip-service/blob/homework/01/README.md).

## Код для знакомства

- [HTTP-сервер и graceful shutdown](https://github.com/redukeee-hse/trip-service/blob/homework/01/cmd/trip-service/main.go)
- [Бизнес-логика и идемпотентность](https://github.com/redukeee-hse/trip-service/blob/homework/01/internal/business/trip_service.go)
- [Репозиторий PostgreSQL](https://github.com/redukeee-hse/trip-service/blob/homework/01/internal/database/trip_repository.go)
- [Менеджер транзакций](https://github.com/redukeee-hse/trip-service/blob/homework/01/internal/database/tx_manager.go)

## Материалы курса

[Курс и задания](https://github.com/course-go-autumn-2026/course) · [Локальное окружение](https://github.com/course-go-autumn-2026/course-infra) · [Правила сдачи](https://github.com/course-go-autumn-2026/course/blob/main/homework/docs/grading.md)
