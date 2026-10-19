SHELL := /bin/bash
.SHELLFLAGS := -o pipefail -c
.DEFAULT_GOAL := help
.DELETE_ON_ERROR:

SERVICE_NAME := user-svc
BIN_DIR := bin
BINARY := $(BIN_DIR)/$(SERVICE_NAME)

SERVER_CMD := ./cmd/svc-starter
MIGRATOR_CMD := ./cmd/migrator

# Порт либо из .env, либо стандартное значение, без возможности переопределения
override GRPCSERVER_PORT := $(shell grep -m1 '^GRPCSERVER_PORT=' .env 2>/dev/null | cut -d'=' -f2- | tr -d '\r' | xargs)
ifeq ($(strip $(GRPCSERVER_PORT)),)
override GRPCSERVER_PORT := 50051
endif

GRPC_ADDR := localhost:$(GRPCSERVER_PORT)

PROTOS_DIR ?= $(abspath $(CURDIR)/../protos)
PROTOSET := $(PROTOS_DIR)/user_service_v1.protoset
GRPCURL_FLAGS := -plaintext -protoset "$(PROTOSET)"

PUBLIC_SERVICE_NAME ?= gateway-svc
INTERNAL_SERVICE_NAME := notify-svc
SERVICE_HEADER_NAME := x-service-name
USER_HEADER_NAME := x-user-id

COVER_PROFILE := coverage.out
COVER_HTML := coverage.html

# PostgreSQL для интеграционных тестов: контейнер с отдельным портом, чтобы не мешать локальной БД
TEST_DB_CONTAINER := user-svc-test-db
TEST_DB_IMAGE ?= postgres:18-alpine
TEST_DB_PORT ?= 55432
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:$(TEST_DB_PORT)/user_svc_test?sslmode=disable

# Контейнер нужен, только если TEST_DATABASE_URL не задан явно (в окружении или в командной строке make)
ifeq ($(origin TEST_DATABASE_URL),file)
TEST_DB_DEP := test-db-up
endif

USER_FILE := .user_id
override USER_ID = $(shell cat $(USER_FILE) 2>/dev/null | tr -d '\r\n')

.PHONY: help protoset build migrate run run-only \
		set-user register login me search subscribe unsubscribe my-followers followers-public followers-internal \
		clean clean-id clean-bin clean-cover print-config \
		_check_tools _check_protoset _check_user _check_lint _check_docker \
		test test-integration test-cover test-db-up test-db-down lint fmt tidy-check vulncheck ci

help: ## Показать список доступных команд
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

protoset: ## Сгенерировать protoset для grpcurl
	@if [ ! -d "$(PROTOS_DIR)" ]; then \
		echo "❌  Папка $(PROTOS_DIR) не найдена. Склонируйте protos или передайте PROTOS_DIR=..."; \
		exit 1; \
	fi
	@$(MAKE) -C "$(PROTOS_DIR)" protoset-user
	@echo "✅  Protoset обновлён: $(PROTOSET)"

migrate: ## Применить миграции
	@echo "⚙️  Применение миграций $(SERVICE_NAME)..."
	@go run $(MIGRATOR_CMD)

build: ## Собрать бинарник сервиса
	@echo "🔨  Сборка $(SERVICE_NAME)..."
	@mkdir -p "$(BIN_DIR)"
	@go build -o "$(BINARY)" $(SERVER_CMD)
	@echo "✅  Собран $(BINARY)"

test: ## Запустить unit-тесты с детектором гонок (без БД)
	@go test -race ./...

test-integration: $(TEST_DB_DEP) ## Запустить все тесты, включая интеграционные с PostgreSQL
	@TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -tags=integration ./...

test-cover: $(TEST_DB_DEP) ## Запустить все тесты и собрать отчёт о покрытии
	@TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -tags=integration \
		-covermode=atomic -coverpkg=./... -coverprofile="$(COVER_PROFILE)" ./...
	@go tool cover -html="$(COVER_PROFILE)" -o "$(COVER_HTML)"
	@echo "📊  Покрытие: $$(go tool cover -func="$(COVER_PROFILE)" | awk '/^total:/ {print $$3}'), отчёт: $(COVER_HTML)"

test-db-up: _check_docker ## Запустить PostgreSQL для интеграционных тестов (Docker)
	@if [ -z "$$(docker ps -q -f name=^$(TEST_DB_CONTAINER)$$)" ]; then \
		echo "🐘  Запуск $(TEST_DB_IMAGE) на порту $(TEST_DB_PORT)..."; \
		docker run -d --rm --name "$(TEST_DB_CONTAINER)" \
			-e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=user_svc_test \
			-p "$(TEST_DB_PORT):5432" "$(TEST_DB_IMAGE)" >/dev/null; \
	fi
	@# Проверка по TCP: при первом запуске образ сначала поднимает временный сервер только на сокете
	@for i in $$(seq 1 60); do \
		docker exec "$(TEST_DB_CONTAINER)" pg_isready -h 127.0.0.1 -U postgres -q && exit 0; \
		sleep 0.5; \
	done; \
	echo "❌  PostgreSQL не запустился, см. docker logs $(TEST_DB_CONTAINER)"; exit 1

test-db-down: ## Остановить и удалить PostgreSQL для тестов
	@docker rm -f "$(TEST_DB_CONTAINER)" >/dev/null 2>&1 || true
	@echo "🧹  Контейнер $(TEST_DB_CONTAINER) удалён"

lint: _check_lint ## Запустить линтеры (как в CI)
	@golangci-lint run ./...

fmt: _check_lint ## Отформатировать код
	@golangci-lint fmt ./...

tidy-check: ## Проверить, что go.mod и go.sum приведены в порядок
	@go mod tidy -diff

vulncheck: ## Проверить код на известные уязвимости
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...

ci: tidy-check lint vulncheck test test-integration ## Запустить все проверки CI локально
	@echo "✅  Все проверки пройдены"

run: migrate build ## Применить миграции, собрать бинарник и запустить сервис
	@echo "🚀  Запуск $(SERVICE_NAME)..."
	@exec "$(BINARY)"

run-only: ## Запустить сервис без миграций и сборки бинарника
	@test -f "$(BINARY)" || { echo "❌  $(BINARY) не найден. Выполните make build"; exit 1; }
	@echo "🚀  Запуск $(SERVICE_NAME)..."
	@exec "$(BINARY)"

set-user: ## Сохранить ID пользователя для grpc-запросов
	@read -rp "ID пользователя: " id; \
	if [ -z "$$id" ]; then \
		echo "❌  ID пользователя не может быть пустым"; \
		exit 1; \
	fi; \
	echo "$$id" > "$(USER_FILE)"; \
	chmod 600 "$(USER_FILE)"; \
	echo "🆔  ID пользователя сохранён в $(USER_FILE)"

register: _check_tools _check_protoset ## Регистрация пользователя и сохранение ID
	@read -rp "Email: " email; \
	read -rsp "Password: " pass; echo; \
	read -rp "Display name: " name; \
	payload=$$(jq -n --arg email "$$email" --arg pass "$$pass" --arg name "$$name" \
		'{email: $$email, password: $$pass, display_name: $$name}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/Register 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		user_id=$$(echo "$$resp" | jq -r '.userId // empty'); \
		if [ -n "$$user_id" ]; then \
			echo "$$user_id" > "$(USER_FILE)"; \
			chmod 600 "$(USER_FILE)"; \
			echo "🆔  ID пользователя сохранён в $(USER_FILE)"; \
		fi; \
		echo "✅  Пользователь зарегистрирован"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось зарегистрироваться"; \
		exit 1; \
	fi

login: _check_tools _check_protoset ## Аутентификация (без сохранения ID)
	@read -rp "Email: " email; \
	read -rsp "Password: " pass; echo; \
	payload=$$(jq -n --arg email "$$email" --arg pass "$$pass" \
		'{email: $$email, password: $$pass}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/Login 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Пользователь авторизован"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось авторизоваться"; \
		exit 1; \
	fi

me: _check_tools _check_protoset _check_user ## Данные профиля текущего пользователя
	@echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-H "$(USER_HEADER_NAME): $(USER_ID)" \
		-d '{}' \
		"$(GRPC_ADDR)" user.v1.UserService/GetMyProfile 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Данные получены"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось получить данные"; \
		exit 1; \
	fi

search: _check_tools _check_protoset _check_user ## Поиск пользователей по имени
	@read -rp "Имя для поиска: " q; \
	payload=$$(jq -n --arg q "$$q" '{search_query: $$q}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-H "$(USER_HEADER_NAME): $(USER_ID)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/FindUsersByDisplayName 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Пользователи найдены"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось выполнить поиск"; \
		exit 1; \
	fi

subscribe: _check_tools _check_protoset _check_user ## Подписаться на пользователя
	@read -rp "ID пользователя для подписки: " id; \
	payload=$$(jq -n --arg id "$$id" '{followee_id: $$id}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-H "$(USER_HEADER_NAME): $(USER_ID)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/Subscribe 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Подписка на $$id"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось подписаться"; \
		exit 1; \
	fi

unsubscribe: _check_tools _check_protoset _check_user ## Отписаться от пользователя
	@read -rp "ID пользователя для отписки: " id; \
	payload=$$(jq -n --arg id "$$id" '{followee_id: $$id}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-H "$(USER_HEADER_NAME): $(USER_ID)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/Unsubscribe 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Отписка от $$id"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось отписаться"; \
		exit 1; \
	fi

my-followers: _check_tools _check_protoset _check_user ## Список своих подписчиков
	@payload=$$(jq -n --arg user_id "$(USER_ID)" '{user_id: $$user_id}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-H "$(USER_HEADER_NAME): $(USER_ID)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/GetFollowers 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Подписчики получены"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось получить подписчиков"; \
		exit 1; \
	fi

followers-public: _check_tools _check_protoset _check_user ## Публичный список подписчиков пользователя по ID
	@read -rp "ID пользователя: " id; \
	payload=$$(jq -n --arg user_id "$$id" '{user_id: $$user_id}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(PUBLIC_SERVICE_NAME)" \
		-H "$(USER_HEADER_NAME): $(USER_ID)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/GetFollowers 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Подписчики получены"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось получить подписчиков"; \
		exit 1; \
	fi

followers-internal: _check_tools _check_protoset ## Внутренний список подписчиков пользователя по ID (с email)
	@read -rp "ID пользователя: " id; \
	payload=$$(jq -n --arg user_id "$$id" '{user_id: $$user_id}'); \
	echo "📬  Ответ сервера:"; \
	if resp=$$(grpcurl $(GRPCURL_FLAGS) \
		-H "$(SERVICE_HEADER_NAME): $(INTERNAL_SERVICE_NAME)" \
		-d "$$payload" \
		"$(GRPC_ADDR)" user.v1.UserService/GetFollowers 2>&1); then \
		echo "$$resp" | jq . 2>/dev/null || echo "$$resp"; \
		echo "✅  Подписчики получены"; \
	else \
		echo "$$resp"; \
		echo "❌  Не удалось получить подписчиков"; \
		exit 1; \
	fi

clean: clean-id clean-bin clean-cover ## Очистить локальные артефакты и ID
clean-id: ## Удалить файл с сохранённым ID пользователя
	@rm -f "$(USER_FILE)"
	@echo "🧹  Файл $(USER_FILE) удалён"
clean-bin: ## Удалить собранные бинарники
	@rm -rf "$(BIN_DIR)"
	@echo "🧹  Директория $(BIN_DIR) удалена"
clean-cover: ## Удалить отчёты о покрытии
	@rm -f "$(COVER_PROFILE)" "$(COVER_HTML)"
	@echo "🧹  Отчёты о покрытии удалены"

print-config: ## Показать текущую конфигурацию
	@echo "SERVICE_NAME         = $(SERVICE_NAME)"
	@echo "BINARY               = $(BINARY)"
	@echo "GRPC_ADDR            = $(GRPC_ADDR)"
	@echo "PROTOS_DIR           = $(PROTOS_DIR)"
	@echo "PROTOSET             = $(PROTOSET)"
	@echo "PUBLIC_SERVICE_NAME  = $(PUBLIC_SERVICE_NAME)"
	@echo "INTERNAL_SERVICE_NAME= $(INTERNAL_SERVICE_NAME)"
	@echo "USER_ID              = $(USER_ID)"

_check_tools:
	@command -v grpcurl >/dev/null 2>&1 || { echo "❌  grpcurl не найден. Установите grpcurl."; exit 1; }
	@command -v jq >/dev/null 2>&1 || { echo "❌  jq не найден. Установите jq."; exit 1; }

_check_docker:
	@command -v docker >/dev/null 2>&1 || { echo "❌  docker не найден. Установите Docker или задайте TEST_DATABASE_URL вручную."; exit 1; }

_check_lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "❌  golangci-lint не найден. Установите golangci-lint (см. README)."; exit 1; }

_check_protoset:
	@test -f "$(PROTOSET)" || { echo "❌  Protoset не найден: $(PROTOSET). Выполните make protoset."; exit 1; }

_check_user:
	@if [ -z "$(USER_ID)" ]; then \
		echo "❌  USER_ID не задан. Выполните make set-user или make register."; \
		exit 1; \
	fi