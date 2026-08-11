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

USER_FILE := .user_id
override USER_ID = $(shell cat $(USER_FILE) 2>/dev/null | tr -d '\r\n')

.PHONY: help protoset build migrate run run-only \
		set-user register login me search subscribe unsubscribe my-followers followers-public followers-internal \
		clean clean-id clean-bin print-config \
		_check_tools _check_protoset _check_user

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

clean: clean-id clean-bin ## Очистить локальные артефакты и ID
clean-id: ## Удалить файл с сохранённым ID пользователя
	@rm -f "$(USER_FILE)"
	@echo "🧹  Файл $(USER_FILE) удалён"
clean-bin: ## Удалить собранные бинарники
	@rm -rf "$(BIN_DIR)"
	@echo "🧹  Директория $(BIN_DIR) удалена"

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

_check_protoset:
	@test -f "$(PROTOSET)" || { echo "❌  Protoset не найден: $(PROTOSET). Выполните make protoset."; exit 1; }

_check_user:
	@if [ -z "$(USER_ID)" ]; then \
		echo "❌  USER_ID не задан. Выполните make set-user или make register."; \
		exit 1; \
	fi