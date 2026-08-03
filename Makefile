SHELL := /bin/bash

# Читаем GRPCSERVER_PORT из .env
GRPCSERVER_PORT := $(shell grep -m1 '^GRPCSERVER_PORT=' .env | cut -d'=' -f2- | xargs)
ifeq ($(GRPCSERVER_PORT),)
  GRPCSERVER_PORT := 50051
endif

GRPC_ADDR := localhost:$(GRPCSERVER_PORT)

PROTOS_DIR ?= ../protos
PROTOSET   := $(PROTOS_DIR)/user_service_v1.protoset

PUBLIC_SERVICE_NAME := gateway-svc
INTERNAL_SERVICE_NAME := notify-svc

USER_FILE := .user_id
USER_ID := $(shell cat $(USER_FILE) 2>/dev/null)

GRPCURL := grpcurl -plaintext -protoset $(PROTOSET)

.PHONY: help protoset run-svc set-user register login me search subscribe unsubscribe \
        my-followers followers-public followers-internal clean

help:
	@echo "Доступные команды:"
	@echo "  make protoset       - Сгенерировать protoset (запускается в ../protos)"
	@echo "  make run-svc        - Запустить User Service (go run)"
	@echo "  make set-user       - Сохранить ID пользователя (вспомогательная команда)"
	@echo "  make register       - Регистрация нового пользователя и сохранение ID"
	@echo "  make login          - Аутентификация пользователя"
	@echo "  make me             - Показать свой профиль"
	@echo "  make search         - Поиск пользователей по имени"
	@echo "  make subscribe      - Подписаться на пользователя"
	@echo "  make unsubscribe    - Отписаться от пользователя"
	@echo "  make my-followers   - Список своих подписчиков"
	@echo "  make followers      - Список подписчиков пользователя по ID"
	@echo "  make clean          - Удалить сохранённый ID"

protoset:
	@if [ ! -d $(PROTOS_DIR) ]; then \
		echo "❌  Папка $(PROTOS_DIR) не найдена, клонируйте репозиторий protos согласно README"; \
		exit 1; \
	fi
	@$(MAKE) -C $(PROTOS_DIR) protoset-user
	@echo "✅  Protoset обновлён: $(PROTOSET)"

# Проверка наличия protoset перед вызовами
_check_protoset:
	@test -f $(PROTOSET) || { \
		echo "❌  Protoset-файл не найден, выполните: make protoset"; \
		exit 1; \
	}

run-svc:
	@echo "🚀  Применение миграций и запуск User Service..."
	@go run ./cmd/migrator/main.go && go run ./cmd/svc-starter/main.go; exit 0

set-user:
	@echo "Определение пользователя, от лица которого будут отправляться запросы"; \
	read -p "ID пользователя: " id; \
	echo "$$id" > $(USER_FILE); \
	chmod 600 $(USER_FILE); \
	echo "🆔  ID пользователя сохранён в $(USER_FILE)"; 

register: _check_protoset
	@echo "Запрос на регистрацию пользователя"; \
	read -p "Email: " email; \
	read -sp "Password: " pass; echo; \
	read -p "Display name: " name; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -d "{\"email\":\"$$email\",\"password\":\"$$pass\",\"display_name\":\"$$name\"}" \
	  $(GRPC_ADDR) user.v1.UserService/Register); \
	if echo "$$resp" | grep -qE '^[\[\{]'; then \
	    echo "$$resp" | jq .; \
		user_id=$$(echo "$$resp" | jq -r '.userId // empty'); \
	    if [ -n "$$user_id" ]; then \
	        echo "$$user_id" > $(USER_FILE); \
	        chmod 600 $(USER_FILE); \
	        echo "🆔  ID пользователя сохранён в $(USER_FILE)"; \
	    fi; \
	    echo "✅  Пользователь зарегистрирован"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

login: _check_protoset
	@echo "Запрос на авторизацию пользователя"; \
	read -p "Email: " email; \
	read -sp "Password: " pass; echo; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -d "{\"email\":\"$$email\",\"password\":\"$$pass\"}" \
	  $(GRPC_ADDR) user.v1.UserService/Login); \
	if echo "$$resp" | grep -qE '^[\[\{]'; then \
	    echo "$$resp" | jq .; \
	    echo "✅  Пользователь авторизован"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

me: _check_protoset
	@test -f $(USER_FILE) || { echo "❌  Сначала выполните make register или make set-user"; exit 1; }
	@echo "Запрос данных профиля текущего пользователя"; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -H 'x-user-id: $(USER_ID)' \
	  -d '{}' \
	  $(GRPC_ADDR) user.v1.UserService/GetMyProfile); \
	if echo "$$resp" | grep -qE '^[\[\{]'; then \
	    echo "$$resp" | jq .; \
	    echo "✅  Данные получены"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

search: _check_protoset
	@test -f $(USER_FILE) || { echo "❌  Сначала выполните make register или make set-user"; exit 1; }
	@echo "Запрос на поиск пользователей по имени"; \
	read -p "Имя для поиска: " q; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -H 'x-user-id: $(USER_ID)' \
	  -d "{\"search_query\":\"$$q\"}" \
	  $(GRPC_ADDR) user.v1.UserService/FindUsersByDisplayName); \
	if echo "$$resp" | grep -qE '^[\[\{]'; then \
	    echo "$$resp" | jq .; \
	    echo "✅  Пользователи найдены"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

subscribe: _check_protoset
	@test -f $(USER_FILE) || { echo "❌  Сначала выполните make register или make set-user"; exit 1; }
	@echo "Запрос подписки на другого пользователя"; \
	read -p "ID пользователя для подписки: " id; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -H 'x-user-id: $(USER_ID)' \
	  -d "{\"followee_id\":\"$$id\"}" \
	  $(GRPC_ADDR) user.v1.UserService/Subscribe); \
	if [ "$$resp" = "{}" ]; then \
	    echo "(пустой)"; \
	    echo "✅  Подписка на $$id"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

unsubscribe: _check_protoset
	@test -f $(USER_FILE) || { echo "❌  Сначала выполните make register или make set-user"; exit 1; }
	@echo "Запрос отписки от другого пользователя"; \
	read -p "ID пользователя для отписки: " id; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -H 'x-user-id: $(USER_ID)' \
	  -d "{\"followee_id\":\"$$id\"}" \
	  $(GRPC_ADDR) user.v1.UserService/Unsubscribe); \
	if [ "$$resp" = "{}" ]; then \
	    echo "(пустой)"; \
	    echo "✅  Отписка от $$id"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

my-followers: _check_protoset
	@test -f $(USER_FILE) || { echo "❌  Сначала выполните make register или make set-user"; exit 1; }
	@echo "Запрос на получение списка своих подписчиков"; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -H 'x-user-id: $(USER_ID)' \
	  -d "{\"user_id\":\"$(USER_ID)\"}" \
	  $(GRPC_ADDR) user.v1.UserService/GetFollowers); \
	if echo "$$resp" | grep -qE '^[\[\{]'; then \
	    echo "$$resp" | jq .; \
	    echo "✅  Подписчики получены"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

followers-public: _check_protoset
	@test -f $(USER_FILE) || { echo "❌  Сначала выполните make register или make set-user"; exit 1; }
	@echo "Публичный запрос на получение списка подписчиков пользователя"; \
	read -p "ID пользователя: " id; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(PUBLIC_SERVICE_NAME)' \
	  -H 'x-user-id: $(USER_ID)' \
	  -d "{\"user_id\":\"$$id\"}" \
	  $(GRPC_ADDR) user.v1.UserService/GetFollowers); \
	if echo "$$resp" | grep -qE '^[\[\{]'; then \
	    echo "$$resp" | jq .; \
	    echo "✅  Подписчики получены"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

followers-internal: _check_protoset
	@echo "Внутренний запрос на получение списка подписчиков пользователя"; \
	read -p "ID пользователя: " id; \
	echo "📬  Ответ сервера:"; \
	resp=$$($(GRPCURL) \
	  -H 'x-service-name: $(INTERNAL_SERVICE_NAME)' \
	  -d "{\"user_id\":\"$$id\"}" \
	  $(GRPC_ADDR) user.v1.UserService/GetFollowers); \
	if echo "$$resp" | grep -qE '^[\[\{]'; then \
	    echo "$$resp" | jq .; \
	    echo "✅  Подписчики получены"; \
	else \
	    echo "$$resp"; \
	    echo "❌  Что-то пошло не так..."; \
	fi

clean:
	@rm -f $(USER_FILE)
	@echo "🗑️   Файл $(USER_FILE) удалён"