# User Service (user-svc)

Микросервис для проекта **Date Wishlist Hub**, отвечает за данные о пользователях и подписках.  
Реализован на `Go`, общается по `gRPC`, хранит данные в `PostgreSQL`.

## Основные возможности

- Регистрация и вход (JWT access‑токен)
- Получение собственного профиля
- Подписка на других пользователей и, соответственно, отписка
- Получение списка подписчиков (используется в **Notification Service**)
- Graceful shutdown

Важно! Сам **User Service** только генерирует и возвращает новые JWT токены для успешно залогинившихся пользователей. Токены, переданные пользователем, должны проверяться в сервисе **API Gateway**, который при успешной аутентификации отправляет gRPC запросы с конкретными данными в **User Service**, которым последний доверяет. Поэтому только **API Gateway** должен иметь сетевой доступ к **User Service**

## Архитектура

Сервис состоит из трёх основных слоёв:

- **Транспортный слой** – gRPC‑хендлеры (`internal/grpc/handlers`)
- **Бизнес‑логика** – сценарии регистрации, аутентификации (получения JWT токена), подписок (`internal/service/user`)
- **Хранилище** – `PostgreSQL` через `database/sql` и `pgx` (`internal/storage/postgresql`)

## Стек

- Go 1.21+
- gRPC (google.golang.org/grpc)
- PostgreSQL (драйвер pgx)
- Миграции через [goose](https://github.com/pressly/goose)
- JWT (golang-jwt/jwt/v5)
- bcrypt для паролей
- Структурированное логирование (slog)

## Установка и запуск

### Требования

- Go 1.21+
- PostgreSQL 12+ (с расширением pgcrypto, если версия <13)
- Утилита `psql`
- Утилита `grpcurl`
- protoc (компилятор Protocol Buffers)
- make

### 0. Подготовка

Для удобства, если считаете нужным, создайте отдельную папку и перейдите в неё

```bash
mkdir sandbox && cd sandbox
```

### 1. Клонируйте нужные репозитории

Клонируем репозиторий user-svc

```bash
git clone https://github.com/alexgul25/user-svc.git
```

Клонируем репозиторий [protos](https://github.com/alexgul25/protos). Он пригодится для тестирования

```bash
git clone https://github.com/alexgul25/protos.git
```

### 2. Убедитесь, что PostgreSQL уже запущен

Проверить статус запуска:

```bash
sudo service postgresql status
```

Запустить сервер PostgreSQL:

```bash
sudo service postgresql start
```

Остановить сервер PostgreSQL:

```bash
sudo service postgresql stop
```

### 3. Создайте пользователя и БД в PostgreSQL

Создание пользователя

```bash
sudo -u postgres psql -c "CREATE USER test_user_svc_owner WITH PASSWORD 'strongpass';"
```

Создание БД

```bash
sudo -u postgres psql -c "CREATE DATABASE test_user_db OWNER test_user_svc_owner;"
```

Проверить доступ

```bash
psql -h localhost -U test_user_svc_owner -d test_user_db -c "SELECT 1;"
```

### 4. Настройте окружение

Создайте `.env` файл в корне проекта и заполните его по аналогии с `.env.example`. Для переменных окружения БД используйте данные, созданные на прошлом шаге.

### 5. Сгенерируйте protoset файл

Для удобного локального тестирования перейдите в папку **protos** и создайте **protoset**

```bash
cd protos && make protoset
```

### 6. Примените миграции и запустите сервер

Перейдите в папку **user-svc** и выполните команды

```bash
go run ./cmd/migrator && go run ./cmd/svc-starter
```

Сервер слушает порт, заданный в переменной окружения `GRPCSERVER_PORT` (по умолчанию `50051`)

### 6. Протестируйте сервер

Примеры тестовых запросов.

- Зарегистрировать пользователя

```bash
grpcurl -plaintext \
  -protoset ../protos/user_service_v1.protoset \
  -d '{"email":"alex@example.com","password":"secret123","display_name":"Alex"}' \
  localhost:50051 user.v1.UserService/Register
```

- Аутентифицировать пользователя

```bash
grpcurl -plaintext \
  -protoset ../protos/user_service_v1.protoset \
  -d '{"email":"alex@example.com","password":"secret123"}' \
  localhost:50051 user.v1.UserService/Login
```

- Получить свой профиль (добавьте нужный id в метаданные)

```bash
grpcurl -plaintext \
  -protoset ../protos/user_service_v1.protoset \
  -H 'x-user-id: <ваш_user_id>' \
  -d '{}' \
  localhost:50051 user.v1.UserService/GetMyProfile
```

- Подписаться на другого пользователя

```bash
grpcurl -plaintext \
  -protoset ../protos/user_service_v1.protoset \
  -H 'x-user-id: <ваш_user_id>' \
  -d '{"followee_id":"<целевой_user_id>"}' \
  localhost:50051 user.v1.UserService/Subscribe
```

- Посмотреть список доступных методов

```bash
grpcurl -plaintext \
  -protoset ../protos/user_service_v1.protoset \
  list user.v1.UserService
```
