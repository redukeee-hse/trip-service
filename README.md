# Trip Service

Сервис поездок курса «Разработка микросервисов на Go». В ЛР1 умеет создавать
поездку, отдавать её и завершать. Данные хранятся в PostgreSQL, схема
накатывается миграциями goose, HTTP-слой генерируется из OpenAPI-контракта.

## Требования

- Go 1.26+
- `tripgoctl` — поднимает локальный PostgreSQL
- `psql` — не обязательно, нужен только для `make db`

Инструменты (`oapi-codegen`, `goose`) подключены в `go.mod` через `go tool`,
ставить их глобально не нужно.

## Запуск

```bash
tripgoctl cluster start        # один раз на машине
tripgoctl environment start    # PostgreSQL + генерация .env
make migrate                   # накатить миграции
make run                       # собрать и запустить сервис
```

`tripgoctl environment start` создаёт `.env` с адресом БД. `Makefile` и сам
сервис читают его автоматически. Без `.env` все переменные можно передать
через окружение.

## Команды Makefile

| Команда | Что делает |
|---|---|
| `make generate` | генерирует `internal/generated/api.gen.go` из `contracts/openapi/trip-service.openapi.yaml` (только операции ЛР1) |
| `make run` | собирает `bin/trip-service` и запускает |
| `make migrate` | применяет все миграции |
| `make migrate-down` | откатывает все миграции |
| `make migrate-status` | показывает, какие миграции применены |
| `make db` | открывает `psql` к базе из `DATABASE_URL` |

## Переменные окружения

Полный список с значениями по умолчанию — в `.env.example`.

| Переменная | По умолчанию | Описание |
|---|---|---|
| `HTTP_ADDR` | `:8080` | адрес HTTP-сервера |
| `LOG_LEVEL` | `info` | уровень логов: `debug`, `info`, `warn`, `error` |
| `SHUTDOWN_TIMEOUT` | `10s` | сколько ждать завершения активных запросов при остановке |
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

Если переменная задана с неверным значением (например `DATABASE_MAX_CONNS=abc`)
или не задан `DATABASE_URL`, сервис не стартует и пишет, что не так. Если БД
недоступна на старте, сервис тоже не стартует.

## API

Контракт: `contracts/openapi/trip-service.openapi.yaml`.

| Метод | Путь | Ответы |
|---|---|---|
| `POST` | `/api/v1/trips` | `201`, `400`, `409`, `500` |
| `GET` | `/api/v1/trips/{tripId}` | `200`, `400`, `404`, `500` |
| `POST` | `/api/v1/trips/{tripId}/finish` | `200`, `400`, `404`, `409`, `500` |
| `GET` | `/health` | `200` |
| `GET` | `/ready` | `200`, `503` |

Ошибки отдаются в `application/problem+json` (RFC 9457) с полем `code`:
`invalid_request`, `trip_not_found`, `trip_completed`, `driver_busy`,
`internal_error`.

`/health` отвечает `200`, пока жив процесс, и в БД не ходит. `/ready` делает
`Ping` PostgreSQL и отвечает `503`, если БД недоступна.

## Структура

```
cmd/trip-service/        main: конфиг, пул, сборка зависимостей, graceful shutdown
internal/config/         чтение и проверка переменных окружения
internal/generated/      код из OpenAPI (не править руками, make generate)
internal/handlers/       HTTP-ручки, валидация, ошибки в problem+json
internal/business/       логика поездок: создание и завершение
internal/database/       пул, TxManager, репозиторий поездок
internal/model/          модель поездки и доменные ошибки
migrations/              миграции goose
contracts/               копия контрактов курса
```

## Решения

### Уровень изоляции

Транзакции открываются с `READ COMMITTED` (задаётся явно в `TxManager.Do`).
От гонок в сервисе защищает не уровень изоляции, а уникальный индекс и
блокировка строки (см. ниже) — оба работают на `READ COMMITTED`. Более
строгие уровни (`REPEATABLE READ`, `SERIALIZABLE`) ничего бы здесь не добавили,
но принесли бы ошибки сериализации, которые пришлось бы обрабатывать повтором.

### Менеджер транзакций

`database.TxManager` с методом `Do(ctx, fn)`:

- `Do` открывает транзакцию (`BeginTx`), кладёт её в `context` под
  неэкспортируемым ключом `txKey{}` и вызывает `fn` с этим контекстом;
- `fn` вернула `nil` — `COMMIT`; вернула ошибку — `ROLLBACK`, ошибка
  возвращается наружу без изменений (чтобы наверху работал `errors.Is`);
- откат сделан через `defer tx.Rollback(...)`, поэтому он выполняется и при
  панике внутри `fn`, а паника летит дальше. После успешного `COMMIT` этот
  `Rollback` ничего не делает;
- если в контексте уже есть транзакция, вложенный `Do` не открывает новую,
  а просто вызывает `fn` в текущей. Коммитит тот, кто открыл.

Репозиторий транзакцию аргументом не получает. Каждый метод берёт исполнителя
через `executorFrom(ctx, pool)`: если в контексте есть транзакция — запрос идёт
через неё, если нет — через пул. Поэтому вызов репозитория вне `Do` просто
выполняется отдельным запросом через пул (так работает `GET /trips/{id}`).

Бизнес-логика (`business.TripService`) знает только интерфейс
`Do(ctx, fn) error` и ничего не знает про `pgx`.

Создание поездки — один `Do`: `INSERT` в `trips` и `INSERT` в
`trip_status_history`. Если вторая вставка падает, поездка тоже откатывается.

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
распознаёт её по коду и имени ограничения и возвращает `ErrDriverBusy`,
ручка отвечает `409 driver_busy`.

Проверка `SELECT` перед `INSERT` не подошла бы: два параллельных запроса оба
увидели бы «активных нет» и оба вставили бы поездку.

### Конкурентное завершение поездки

`FinishTrip` внутри `Do` читает поездку через `SELECT ... FOR UPDATE`, проверяет
статус и делает `UPDATE`. `FOR UPDATE` блокирует строку до конца транзакции:
второй параллельный `finish` ждёт, после `COMMIT` первого получает уже
`completed` и отвечает `409 trip_completed`. `finished_at` не перезаписывается.

Блокировка работает только внутри транзакции — вне `Do` она снялась бы сразу
после `SELECT`, и между чтением и записью снова появилось бы окно.

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
