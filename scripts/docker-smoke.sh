#!/usr/bin/env bash
# Смоук-тест Docker-образа: мигратор применяет миграции, сервис запускается,
# отвечает на health-check и корректно останавливается по SIGTERM (docker stop).
#
# Использование: scripts/docker-smoke.sh IMAGE
# База задаётся переменными DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME.
# Контейнеры работают в сети хоста (--network host): так в Linux и WSL2
# контейнер видит базу на localhost, а порт сервиса доступен с хоста.
set -euo pipefail

IMAGE="${1:?usage: $0 IMAGE}"
NAME="user-svc-smoke"
PORT="${GRPCSERVER_PORT:-50051}"

env_file="$(mktemp)"
cleanup() {
	docker rm -f "$NAME" >/dev/null 2>&1 || true
	rm -f "$env_file"
}
trap cleanup EXIT

# Значения без кавычек: docker передаёт их в контейнер как есть.
cat >"$env_file" <<EOF
ENV=prod
DB_USER=${DB_USER:-postgres}
DB_PASSWORD=${DB_PASSWORD:-postgres}
DB_NAME=${DB_NAME:-user_svc_test}
DB_HOST=${DB_HOST:-localhost}
DB_PORT=${DB_PORT:-5432}
GRPCSERVER_PORT=${PORT}
GRACEFUL_TIMEOUT=10s
SERVICES_WITH_EMAIL_HIDDEN=gateway-svc
JWT_SECRET=smoke-secret
EOF

fail() {
	echo "❌  $1"
	docker logs "$NAME" 2>&1 || true
	exit 1
}

echo "⚙️   Миграции..."
docker run --rm --network host --env-file "$env_file" --entrypoint /app/migrator "$IMAGE"

echo "🚀  Запуск сервиса..."
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" --network host --env-file "$env_file" "$IMAGE" >/dev/null

echo "🩺  Ожидание health-check..."
healthy=""
for _ in $(seq 1 30); do
	if docker exec "$NAME" /bin/grpc_health_probe -addr=":${PORT}" >/dev/null 2>&1; then
		healthy=1
		break
	fi
	if [ "$(docker inspect -f '{{.State.Running}}' "$NAME")" != "true" ]; then
		fail "Контейнер завершился до health-check"
	fi
	sleep 1
done
[ -n "$healthy" ] || fail "Сервис не ответил на health-check за 30 секунд"

echo "🛑  Остановка (SIGTERM)..."
docker stop -t 15 "$NAME" >/dev/null
exit_code="$(docker inspect -f '{{.State.ExitCode}}' "$NAME")"
[ "$exit_code" = "0" ] || fail "Код выхода $exit_code, ожидался 0"
docker logs "$NAME" 2>&1 | grep -q "grpc server gracefully stopped" ||
	fail "В логах нет записи о корректной остановке"

echo "✅  Образ $IMAGE прошёл смоук-тест"
