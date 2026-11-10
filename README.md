# Trip Service — Go backend для сервиса поездок

HTTP API для создания, получения и завершения поездок. Учебный проект курса
«Разработка микросервисов на Go». Данные хранятся в PostgreSQL, схема
накатывается миграциями goose, HTTP-слой генерируется из OpenAPI-контракта.

**Стек:** Go · Chi · PostgreSQL · pgx / pgxpool · Squirrel · goose · OpenAPI · oapi-codegen ·
log/slog · OpenTelemetry · gomock · testcontainers-go · golangci-lint · Docker.

**Главное:** транзакционная запись поездки и истории статусов, идемпотентное
создание по `Idempotency-Key`, защита от двух активных поездок водителя
частичным уникальным индексом, конкурентное завершение через
`SELECT ... FOR UPDATE`. Код разложен по слоям с зависимостями внутрь, покрыт
unit- и интеграционными тестами и отдаёт логи, трейсы и метрики. Подробности —
в разделах [Архитектура](#архитектура), [Observability](#observability) и
[Решения](#решения).

## Что сделано в ЛР2

Снаружи сервис не изменился: те же ручки, тот же контракт, новых миграций нет.
Изменилось устройство:

- код разложен по слоям `domain` → `usecase` → `http` / `repository` + `infra`,
  все зависимости собираются в `internal/container`;
- переходы статусов и валидация — методы доменной поездки, доменные ошибки
  объявлены в домене, перевод ошибки в HTTP-ответ — в одном месте
  (`internal/http/domainerror`);
- `golangci-lint` с конфигом курса и `depguard`, который проверяет правила слоёв;
- тесты: домен, usecase на `gomock`, хендлеры через `httptest`, маппинг ошибок,
  middleware, интеграционные тесты репозитория на реальном PostgreSQL;
- структурные логи `slog` с `trace_id`/`span_id`, трейсы и метрики через
  OpenTelemetry, middleware `Recover → Tracing → Logging → Metrics`.

## Требования

- Go 1.26+
- `tripgoctl` — поднимает PostgreSQL и стек observability (коллектор, Jaeger,
  Prometheus, Grafana)
- Docker — для интеграционных тестов (`testcontainers-go`)
- `golangci-lint` v2 — для `make lint`
- `psql` — не обязательно, нужен только для `make db`

Инструменты `oapi-codegen`, `goose` и `mockgen` подключены в `go.mod` через
`go tool`, ставить их глобально не нужно.

## Запуск

```bash
tripgoctl cluster start        # один раз на машине
tripgoctl environment start    # PostgreSQL + коллектор, Jaeger, Prometheus, Grafana; генерирует .env
tripgoctl connect              # адреса Jaeger, Grafana, Prometheus
make migrate                   # накатить миграции
make run                       # собрать и запустить сервис
```

Состав окружения задаёт `environment.toml` (`lab = 2`, компоненты `postgres` и
`observability`). `tripgoctl environment start` создаёт `.env` с адресом БД и
коллектора. `Makefile` и сам сервис читают его автоматически. Без `.env` все
переменные можно передать через окружение.

## Команды Makefile

| Команда | Что делает |
|---|---|
| `make generate` | вся кодогенерация: `internal/generated/api.gen.go` из OpenAPI и моки `mocks_test.go` (`go generate ./...`) |
| `make run` | собирает `bin/trip-service` и запускает |
| `make test` | все unit-тесты с `-race`; в конце печатает покрытие `usecase` + `domain` и всего проекта |
| `make test-integration` | интеграционные тесты репозитория (`-tags=integration`, нужен Docker) |
| `make lint` | `golangci-lint run ./...` по `.golangci.yml` |
| `make migrate` | применяет все миграции |
| `make migrate-down` | откатывает все миграции |
| `make migrate-status` | показывает, какие миграции применены |
| `make db` | открывает `psql` к базе из `DATABASE_URL` |
| `make docker-build` | собирает Docker-образ `trip-service:local` |
| `make docker-run` | запускает образ рядом с окружением из `tripgoctl` |

## Архитектура

### Слои

```
                        cmd/trip-service/main.go
                     конфиг → container.New → запуск → сигнал
                                     │
                          internal/container            создаёт всё и связывает
          ┌──────────────┬───────────┴───────┬──────────────────┐
          ▼              ▼                   ▼                  ▼
   internal/http   internal/usecase   internal/repository   internal/infra
   (транспорт)     (сценарии)         (SQL)                 (pgxpool, tx, OTel, slog)
          │              │                   │
          │              ▼                   │
          └──────▶ internal/domain ◀─────────┘
                   (поездка, правила, ошибки)
```

Стрелки — направление `import`. Зависимости направлены **внутрь**:

| Слой | Что делает | Что знает |
|---|---|---|
| `domain/trip` | сущность `Trip`, статусы, переход `Finish`, `Validate`, доменные ошибки | только стандартную библиотеку и `uuid` |
| `usecase/trip/{create,get,finish}` | сценарий: транзакция, загрузка, вызов доменного метода, сохранение, бизнес-лог и счётчик | `domain` и **свои** интерфейсы из `contract.go` |
| `http/trip/{create,get,finish}` | разбор запроса, вызов usecase, ответ | `domain`, свой интерфейс usecase, `generated` |
| `http/domainerror` | единственное место «ошибка → `Problem`» | `domain`, ошибки usecase |
| `repository/trip` | SQL на squirrel, перевод ошибок Postgres в доменные | `domain`, `pgx` |
| `infra/postgres`, `infra/telemetry` | пул, менеджер транзакций, OTel SDK, логгер | внешние библиотеки |
| `container` | создаёт конкретные реализации и передаёт их в конструкторы | всё |

Во время работы вызовы идут `http → usecase → repository`, а импорты — нет:
хендлер не импортирует пакет usecase, usecase не импортирует репозиторий.
Каждый объявляет интерфейс того, что ему нужно, а конкретный объект ему
передаёт `container`.

Правила импортов проверяет `depguard` в `.golangci.yml` (п. 4.2 ТЗ):

| Пакет | Нельзя импортировать |
|---|---|
| `internal/domain` | `net/http`, `chi`, `pgx`, `squirrel`, `otel` |
| `internal/usecase` | `net/http`, `chi`, `pgx`, `squirrel`, `internal/generated` |
| `internal/http/*` | `internal/repository/*`, `internal/infra/*` |

Проверка вручную: `go list -deps ./internal/domain/... | grep -E 'jackc/pgx|go-chi|net/http'` — пусто.

### Где объявлены интерфейсы и почему там

Интерфейсы объявляет **потребитель**, в `contract.go` своего пакета, с
маленькой буквы:

- `usecase/trip/create/contract.go` — `tripRepository`, `idempotencyRepository`,
  `txManager`, `tripMetrics`;
- `usecase/trip/finish/contract.go` — `tripRepository`, `txManager`, `tripMetrics`;
- `usecase/trip/get/contract.go` — `tripRepository` с одним методом `GetByID`;
- `http/trip/*/contract.go` — интерфейс своего usecase с одним методом `Execute`;
- `repository/trip/contract.go` — `executorProvider` (откуда взять пул или транзакцию).

Почему у потребителя:

- **зависимость идёт внутрь.** Usecase сам формулирует, что ему нужно, и не
  импортирует репозиторий. `*triprepo.Repository` подходит под интерфейс
  неявно, проверяет это компилятор в `container`;
- **интерфейсы узкие.** `get` видит один метод репозитория, а не все шесть —
  такой интерфейс проще мокать и он точно описывает зависимость;
- **моки рядом с кодом.** `mockgen` генерирует `mocks_test.go` из `contract.go`
  (`//go:generate`), отдельного пакета `mocks/` нет.

**Что изменится с вторым транспортом (ЛР4, gRPC).** Появится пакет
`internal/grpc/trip/{create,get,finish}` со своими `contract.go` и маппингом
ошибок в gRPC-коды в одном месте (аналог `domainerror`). `domain`, `usecase` и
`repository` не меняются, в `container` добавится сборка gRPC-сервера из тех же
usecase.

### Ошибки

- доменные ошибки — в `domain/trip/errors.go`: `ErrTripNotFound`, `ErrDriverBusy`,
  `ErrTripCompleted` и тип `ValidationError{Field, Reason}` для ошибок валидации;
- `ErrIdempotencyConflict` объявлен в `usecase/trip/create`: это правило
  протокола повторных запросов, а не правило поездки, и проверяет его только
  этот usecase;
- репозиторий переводит `pgx.ErrNoRows` и нарушение `trips_driver_active_uniq`
  в доменные ошибки, остальное оборачивает `fmt.Errorf("...: %w", err)`;
- `http/domainerror.Problem(err)` — единственный `switch` по `errors.Is` /
  `errors.As`. Неизвестная ошибка → `500 internal_error` без деталей: текст
  ошибки БД уходит только в лог и в спан.

### Отступления от раскладки ТЗ

- `cmd/trip-service/` вместо `cmd/service/` — так требует `conventions.md`
  (раздел 8) с ЛР1, Dockerfile и Makefile уже на него завязаны;
- нет `internal/model/`: между слоями ходит доменная `trip.Trip`, отдельной
  модели без поведения не понадобилось;
- в `internal/http/` кроме ручек и `domainerror` есть вспомогательные пакеты:
  `router` (адаптер к единому `ServerInterface` из oapi-codegen — он делегирует
  каждый метод своей ручке), `response` (запись JSON и problem+json), `tripapi`
  (общий для трёх ручек перевод `trip.Trip` → `api.Trip`), `health`
  (`/health`, `/ready`), `requesterror` (передаёт текст ошибки 500 из ответа в
  запись лога), `middleware`.

## Тесты

```bash
make test               # unit, с -race, печатает покрытие
make test-integration   # репозиторий на реальном PostgreSQL (нужен Docker)
```

Покрытие `internal/usecase/...` + `internal/domain/...` — **100%**.

| Что | Чем | Где |
|---|---|---|
| `Finish`, `Validate` | table-driven, без моков | `domain/trip/trip_test.go` |
| usecase | `gomock` на репозитории, `txManager`, счётчики | `usecase/trip/*/usecase_test.go` |
| маппинг ошибок в `Problem` | table-driven | `http/domainerror/domainerror_test.go` |
| хендлеры | `httptest` + мок usecase | `http/trip/*/handler_test.go`, `http/router` |
| middleware, логгер, OTel | `httptest`, `tracetest`, `ManualReader` | `http/middleware`, `infra/telemetry` |
| репозиторий | testcontainers-go, `postgres:16-alpine`, миграции из `migrations/` | `repository/trip/*_integration_test.go` |

Обязательные сценарии ТЗ:

| # | Сценарий | Тест |
|---|---|---|
| 1 | создание поездки — успех | `create.TestExecute_CreatesTrip`, `create` handler |
| 2 | водитель занят → `ErrDriverBusy` → `409` | `create.TestExecute_DependencyErrors`, `domainerror`, `TestRepository_Create_DriverBusy` |
| 3 | завершение активной — успех | `finish.TestUsecase_Execute_Success` |
| 4 | завершение завершённой → `ErrTripCompleted` → `409` | `finish.TestUsecase_Execute_Errors`, `domainerror` |
| 5 | получение несуществующей → `ErrTripNotFound` → `404` | `get` usecase и handler, `domainerror` |
| 6 | невалидное тело → `400 invalid_request` | `create` handler, `create.TestExecute_ValidationError` |
| 7 | ошибка репозитория → `500` без деталей | `domainerror`, handler, `middleware` |
| 8 | `INSERT` в двух таблицах откатывается целиком | `TestTransaction_RollbackBothTables` |

Интеграционные тесты отделены тегом `//go:build integration`, каждый готовит и
убирает свои данные и проходит при `-count=2`.

## Observability

Сервис отдаёт три сигнала. Метрики и трейсы уходят по OTLP в коллектор, тот
раскладывает их по Jaeger и Prometheus; в коде сервиса этих имён нет — только
OpenTelemetry. Логи пишутся в stdout.

### Логи

`log/slog`, JSON, stdout, уровень из `LOG_LEVEL`. Логгер создаётся в
`container` и передаётся зависимостью, `slog.Default()` в коде нет. В каждой
записи есть `service`, а если запись сделана внутри спана — `trace_id` и
`span_id`: по ним запись находится в Jaeger.

```json
{"level":"INFO","msg":"request handled","trace_id":"aaf5c1da9f7a246009870b03ff9de24f","span_id":"ba41172c932d7eb1","service":"trip-service","method":"POST","route":"/api/v1/trips","status":201,"duration_ms":6}
```

- middleware `Logging` пишет одну запись на запрос, когда ответ готов: метод,
  **шаблон** маршрута, статус, длительность. Тела не логируются;
- уровень `Info` для `2xx` и `4xx`, `Error` для `5xx` — тогда в записи есть
  поле `error` с текстом внутренней ошибки (клиент её не видит);
- бизнес-события `trip created` и `trip completed` пишут usecase, только с
  `trip_id`;
- `Recover` пишет `panic recovered` со стектрейсом.

**Как `trace_id` попадает в лог.** `Tracing` создаёт спан и кладёт его в
`context` запроса. Дальше этот `ctx` идёт в `Logging`, хендлер и usecase, а
логгер вызывается через `InfoContext(ctx, ...)` / `LogAttrs(ctx, ...)`. Обёртка
`traceHandler` в `infra/telemetry/logger.go` достаёт спан из `ctx` и добавляет
`trace_id` и `span_id` в запись.

### Трейсы

- `infra/telemetry.Setup`: ресурс (`service.name`, `service.version`,
  `service.namespace=tripgo`, `deployment.environment=local`), экспорт по OTLP
  (`grpc` или `http/protobuf`), сэмплер из `OTEL_TRACES_SAMPLER`, пропагатор W3C
  `traceparent`;
- `Tracing` (`otelhttp`) — корневой спан на запрос с именем
  `{method} {http.route}`, например `POST /api/v1/trips`, и атрибутом `http.route`;
- каждый запрос в БД — дочерний спан (`otelpgx` на уровне пула, репозиторий о
  телеметрии не знает) с `db.operation.name` и `db.collection.name`;
- ошибка помечает спан `codes.Error` и пишет `error.type`: для `500` —
  `internal_error` и событие с текстом ошибки, для БД — SQLSTATE (`23505` при
  занятом водителе), для паники — `panic`.

Трейс создания поездки в Jaeger:

```
POST /api/v1/trips
├── BEGIN
├── INSERT  trips
├── INSERT  trip_status_history
└── COMMIT
```

### Метрики

| Метрика | Тип | Лейблы | Где пишется |
|---|---|---|---|
| `http.server.request.duration` | histogram, с | `http.request.method`, `http.route`, `http.response.status_code` | middleware `Metrics` |
| `http.server.active_requests` | up-down counter | `http.request.method`, `http.route` | middleware `Metrics` |
| `db.client.operation.duration` | histogram, с | `db.operation.name`, `db.collection.name`, `error.type` | `infra/postgres` (трейсер пула) |
| `tripgo.trips.created` | counter | — | usecase `create` |
| `tripgo.trips.completed` | counter | — | usecase `finish` |

В Prometheus имена с подчёркиваниями: `http_server_request_duration_seconds`,
`tripgo_trips_created_total` и т. д.

В `http.route` — только шаблон (`/api/v1/trips/{tripId}`). С подставленным
`tripId` на каждую поездку заводилась бы своя временная серия: число серий
росло бы без ограничений, а метрика перестала бы агрегироваться по ручке.
Неизвестный маршрут пишется как `unknown`, нестандартный метод — `_OTHER`.

Отдельных счётчиков бизнес-ошибок нет: `driver_busy` и `trip_not_found` видны
по `http.response.status_code`.

### Middleware

```
Recover → Tracing → Logging → Metrics → роутер
```

`Logging` стоит внутри `Tracing`, поэтому в его записи уже есть `trace_id`.
`Recover` ловит панику, логирует со стектрейсом, отвечает `500 internal_error`,
процесс не падает. `Tracing` и `Metrics` узнают шаблон маршрута до роутера
через `chi.Routes.Find`.

### Если коллектор недоступен

Экспорт асинхронный: SDK копит спаны и метрики и отправляет пачками в фоне.
Неудачная отправка пишется в лог как `Warn` `telemetry export failed`, запросы
обслуживаются как обычно. При остановке сервис пытается дописать телеметрию,
но не дольше `SHUTDOWN_TIMEOUT`.

### Как проверить

```bash
tripgoctl environment start && make run
curl -sS -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' -d @trip.json -i
```

- Jaeger (адрес из `tripgoctl connect`): сервис `trip-service`, операция
  `POST /api/v1/trips` — внутри спаны `INSERT` в `trips` и `trip_status_history`;
- `trace_id` из записи `request handled` → поиск по Trace ID в Jaeger;
- Grafana → Explore → Prometheus: `http_server_request_duration_seconds_count`,
  `tripgo_trips_created_total`. Метрики приходят раз в `OTEL_METRIC_EXPORT_INTERVAL`;
- остановить коллектор — сервис продолжает отвечать, в логе `telemetry export failed`.

## Docker

```bash
tripgoctl environment start
make migrate
make docker-build
make docker-run
curl localhost:8080/ready
```

`deploy/Dockerfile` — многостадийная сборка. Первая стадия (`golang:1.26`)
собирает бинарь, во вторую (`gcr.io/distroless/static-debian12:nonroot`)
копируется только он: в итоговом образе нет исходников, Go и кеша сборки.
Бинарь собран с `CGO_ENABLED=0`, поэтому ему не нужна libc. Процесс работает
от пользователя `nonroot` (UID 65532), не от root. `.dockerignore` не пускает в
сборку `.git`, `.env`, тесты и локальные файлы.

Контейнер запускается с `--network host`, чтобы достучаться до PostgreSQL и
коллектора, которые `tripgoctl` пробрасывает на `localhost`. Переменные берутся
из `.env`.

## Переменные окружения

Полный список с значениями по умолчанию — в `.env.example`.

| Переменная | По умолчанию | Описание |
|---|---|---|
| `HTTP_ADDR` | `:8080` | адрес HTTP-сервера |
| `LOG_LEVEL` | `info` | уровень логов: `debug`, `info`, `warn`, `error` |
| `SHUTDOWN_TIMEOUT` | `10s` | сколько ждать завершения запросов и отправки телеметрии при остановке |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | время на чтение заголовков запроса |
| `HTTP_READ_TIMEOUT` | `10s` | время на чтение всего запроса |
| `HTTP_WRITE_TIMEOUT` | `15s` | время на обработку и запись ответа |
| `HTTP_IDLE_TIMEOUT` | `60s` | сколько держать простаивающее keep-alive соединение |
| `HTTP_PING_TIMEOUT` | `1s` | таймаут проверки БД в `/ready` |
| `DATABASE_URL` | — | **обязательная**, строка подключения к PostgreSQL |
| `DATABASE_MAX_CONNS` | `10` | максимум соединений в пуле |
| `DATABASE_MIN_CONNS` | `2` | сколько соединений держать открытыми без нагрузки |
| `DATABASE_MAX_CONN_LIFETIME` | `30m` | время жизни соединения |
| `DATABASE_CONNECT_TIMEOUT` | `5s` | таймаут подключения к БД (и `Ping` на старте) |
| `DATABASE_QUERY_TIMEOUT` | `3s` | таймаут одного запроса к БД |
| `OTEL_SERVICE_NAME` | `trip-service` | `service.name` в трейсах и метриках, `service` в логах |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | адрес коллектора |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | `grpc` или `http/protobuf` |
| `OTEL_TRACES_SAMPLER` | `parentbased_always_on` | сэмплер трейсов по спецификации OTel |
| `OTEL_TRACES_SAMPLER_ARG` | `1` | доля трейсов для `*traceidratio` |
| `OTEL_RESOURCE_ATTRIBUTES` | — | дополнительные атрибуты ресурса, переопределяют значения по умолчанию |
| `OTEL_METRIC_EXPORT_INTERVAL` | `60000` | интервал отправки метрик, мс (читает SDK) |

Если переменная задана с неверным значением (например `DATABASE_MAX_CONNS=abc`
или `OTEL_EXPORTER_OTLP_PROTOCOL=json`) или не задан `DATABASE_URL`, сервис не
стартует и пишет, что не так. Если БД недоступна на старте, сервис тоже не
стартует. Недоступный коллектор старт не блокирует.

## API

Контракт: `contracts/openapi/trip-service.openapi.yaml`.

| Метод | Путь | Ответы |
|---|---|---|
| `POST` | `/api/v1/trips` | `201`, `200` (повтор с `Idempotency-Key`), `400`, `409`, `500` |
| `GET` | `/api/v1/trips/{tripId}` | `200`, `400`, `404`, `500` |
| `POST` | `/api/v1/trips/{tripId}/finish` | `200`, `400`, `404`, `409`, `500` |
| `GET` | `/health` | `200` |
| `GET` | `/ready` | `200`, `503` |

Ошибки отдаются в `application/problem+json` (RFC 9457) с полем `code`:
`invalid_request`, `trip_not_found`, `trip_completed`, `driver_busy`,
`idempotency_conflict`, `internal_error`.

`/health` отвечает `200`, пока жив процесс, и в БД не ходит. `/ready` делает
`Ping` PostgreSQL и отвечает `503`, если БД недоступна.

## Структура

```
cmd/trip-service/            main: конфиг, container.New, запуск, graceful shutdown
internal/config/             чтение и проверка переменных окружения
internal/container/          создаёт все зависимости и связывает их
internal/domain/trip/        поездка: статусы, Finish, Validate, доменные ошибки
internal/usecase/trip/       create, get, finish: usecase.go, contract.go, тесты и моки
internal/http/trip/          create, get, finish: handler.go, contract.go, тесты и моки
internal/http/domainerror/   ошибка → Problem, одно место
internal/http/middleware/    Recover, Tracing, Logging, Metrics
internal/http/router/        адаптер к ServerInterface из oapi-codegen
internal/repository/trip/    SQL поездок и ключей идемпотентности, интеграционные тесты
internal/infra/postgres/     пул pgx, трейсер запросов; transaction/ — менеджер транзакций
internal/infra/telemetry/    OpenTelemetry SDK, slog, бизнес-счётчики
internal/generated/          код из OpenAPI (не править руками, make generate)
migrations/                  миграции goose
contracts/                   копия контрактов курса
deploy/                      Dockerfile
```

## Решения

### Уровень изоляции

Транзакции открываются с `READ COMMITTED` (задаётся явно в `transaction.Manager.Do`).
От гонок в сервисе защищает не уровень изоляции, а уникальный индекс и
блокировка строки (см. ниже) — оба работают на `READ COMMITTED`. Более
строгие уровни (`REPEATABLE READ`, `SERIALIZABLE`) ничего бы здесь не добавили,
но принесли бы ошибки сериализации, которые пришлось бы обрабатывать повтором.

### Менеджер транзакций

`infra/postgres/transaction.Manager` с методом `Do(ctx, fn)`:

- `Do` открывает транзакцию (`BeginTx`), кладёт её в `context` под
  неэкспортируемым ключом `txKey{}` и вызывает `fn` с этим контекстом;
- `fn` вернула `nil` — `COMMIT`; вернула ошибку — `ROLLBACK`, ошибка
  возвращается наружу без изменений (чтобы наверху работал `errors.Is`);
- откат сделан через `defer`, поэтому он выполняется и при панике внутри `fn`.
  После успешного `COMMIT` этот `Rollback` возвращает `pgx.ErrTxClosed`, и
  ошибка намеренно игнорируется;
- если в контексте уже есть транзакция, вложенный `Do` не открывает новую,
  а просто вызывает `fn` в текущей. Коммитит тот, кто открыл.

Репозиторий транзакцию аргументом не получает. Каждый метод берёт исполнителя
через `Manager.Executor(ctx)`: если в контексте есть транзакция — запрос идёт
через неё, если нет — через пул. Поэтому вызов репозитория вне `Do` просто
выполняется отдельным запросом через пул (так работает `GET /trips/{id}`).

Usecase знает только интерфейс `Do(ctx, fn) error` и ничего не знает про `pgx`.

Создание поездки — один `Do`: `INSERT` в `trips` и `INSERT` в
`trip_status_history`. Если вторая вставка падает, поездка тоже откатывается
(проверено интеграционным тестом).

### Запрет двух активных поездок у водителя

Частичный уникальный индекс в миграции `create_trips`:

```sql
CREATE UNIQUE INDEX trips_driver_active_uniq
    ON trips (driver_id)
    WHERE status = 'active';
```

В индекс попадают только активные поездки, поэтому завершённых у водителя может
быть сколько угодно, а активная — одна. Проверку делает сама БД в момент
`INSERT`, окна между «проверили» и «вставили» нет. При двух одновременных
запросах второй ждёт первый на индексе и получает ошибку `23505`. Репозиторий
распознаёт её по коду и имени ограничения и возвращает `trip.ErrDriverBusy`,
ручка отвечает `409 driver_busy`.

Проверка `SELECT` перед `INSERT` не подошла бы: два параллельных запроса оба
увидели бы «активных нет» и оба вставили бы поездку.

### Конкурентное завершение поездки

Usecase `finish` внутри `Do` читает поездку через `SELECT ... FOR UPDATE`,
вызывает доменный `trip.Finish(now)` и делает `UPDATE`. `FOR UPDATE` блокирует
строку до конца транзакции: второй параллельный `finish` ждёт, после `COMMIT`
первого получает уже `completed`, и `Finish` возвращает `ErrTripCompleted` →
`409 trip_completed`. `finished_at` не перезаписывается.

`Finish` разрешает переход только из `active`: любой другой статус, в том числе
будущий, по умолчанию запрещён.

### Идемпотентность создания поездки

`POST /api/v1/trips` с заголовком `Idempotency-Key`:

| Запрос | Ответ |
|---|---|
| первый с этим ключом | `201`, поездка создана |
| тот же ключ и те же данные | `200` и та же поездка, вторая не создаётся |
| тот же ключ, другие данные | `409 idempotency_conflict` |
| без заголовка | как обычно |

Ключи хранятся в таблице `idempotency_keys`: ключ, данные запроса одной строкой
и `id` созданной поездки. Повтор от конфликта отличается сравнением этих данных.
Повтор не считается новой поездкой: ни лога `trip created`, ни счётчика.

**Ключ живёт 24 часа** (константа в `usecase/trip/create`). Этого хватает на
повторы после сбоя сети. Просроченный ключ удаляется при следующем запросе с
ним и считается новым.

**Одновременные запросы с одним ключом.** Ключ занимается в той же транзакции,
что и создание поездки, и **до** неё: `INSERT ... ON CONFLICT (key) DO NOTHING`.
Второй запрос ждёт первый на первичном ключе. Первый закоммитил — второй видит
занятый ключ и отдаёт ту же поездку (`200`) или `409`. Первый откатился (например,
`driver_busy`) — ключ откатился вместе с ним, второй создаёт поездку сам.

### Линтер

`.golangci.yml` — конфиг курса без изменений в наборе линтеров, плюс правила
`depguard` для `internal/http` (третья строка таблицы п. 4.2) и запрет
`internal/generated` в usecase. Единственный `nolint` — на `int32(status)` в
`domainerror`: статус берётся из констант `net/http` (100–599), переполнения нет.

## Проверка

```bash
# создание
curl -sS -X POST localhost:8080/api/v1/trips \
  -H 'content-type: application/json' \
  -d '{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
       "driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
       "start_point":{"latitude":59.9398,"longitude":30.3146},
       "end_point":{"latitude":59.9290,"longitude":30.3626},
       "price":1450}'

# получение и завершение
curl -sS localhost:8080/api/v1/trips/$TRIP_ID
curl -sS -X POST localhost:8080/api/v1/trips/$TRIP_ID/finish -i

# 20 одновременных созданий на одного водителя: ожидается один 201
seq 20 | xargs -P20 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' \
  -d @trip.json | sort | uniq -c
```

## Материалы курса

[Курс и задания](https://github.com/course-go-autumn-2026/course) · [Локальное окружение](https://github.com/course-go-autumn-2026/course-infra) · [Правила сдачи](https://github.com/course-go-autumn-2026/course/blob/main/homework/docs/grading.md)
