# User Service (user-svc)

Микросервис для проекта **Date Wishlist Hub**, отвечает за данные о пользователях и подписках.  

Стек: `Go`, `gRPC`, `PostgreSQL`

## Основные возможности

**User Service** предназначен только для внутреннего межсервисного взаимодействия и не должен быть доступен пользователям напрямую. Все запросы от конечных пользователей принимает **Gateway Service**.

- Регистрация новых пользователей (метод `Register` - вызывается через **API Gateway**, для этого пользователю НЕ нужен JWT-токен)

- Выдача access-токенов пользователям при успешной аутентификации (метод `Login` - вызывается через **API Gateway**, для этого пользователю НЕ нужен JWT-токен)

- Получение информации о своём профиле (метод `GetMyProfile` - вызывается через **API Gateway**, для этого пользователю нужен JWT-токен)

- Подписка на других пользователей (метод `Subscribe` - вызывается через **API Gateway**, для этого пользователю нужен JWT-токен)

- Отмена своей подписки (метод `Unsubscribe` - вызывается через **API Gateway**, для этого пользователю нужен JWT-токен)

- Получение списка подписчиков пользователя (метод `GetFollowers` - вызывается через **API Gateway**, для этого пользователю нужен JWT-токен, из которого извлекается `user_id` для запроса (на данном этапе пользователь может смотреть только своих подписчиков); также вызывается через **Notification Service** (может получить подписчиков любого пользователя) по внутренней сети при создании нового места в **Wishlist Service**)

**Важно!** Сам **User Service** только генерирует и возвращает новые JWT токены для успешно залогинившихся пользователей. Токены, переданные пользователем, должны проверяться в сервисе **API Gateway**, который при успешной аутентификации отправляет gRPC запросы с конкретными данными в **User Service**.

Все запросы для **User Service** должны передавать заголовок `x-service-name` - имя сервиса, вызывающего метод

Для методов, требующих идентификации через JWT токен, необходимо передавать заголовок `x-user-id`

## Архитектура

Основные компоненты структуры

- Команды для запуска приложения ([./cmd](./cmd/))

- Код для запуска различных компонентов приложения ([./internal/app](./internal/app/))

- Структуры данных и модели домена ([./internal/domain/models](./internal/domain/models/))

- **gPRC-хэндлеры** ([./internal/grpc/handlers](./internal/grpc/handlers/))

- gRPC-интерсепторы ([./internal/grpc/interceptors](./internal/grpc/interceptors/))

- Общие вспомогательные утилиты и функции ([./internal/lib](./internal/lib/))

- **Сервисный слой (бизнес-логика)** ([./internal/service](./internal/service/))

- **Слой хранения данных** ([./internal/storage](./internal/storage/))

- Миграции для БД ([./migrations](./migrations/))

## Локальная установка и запуск

Ниже приведены шаги для запуска проекта в дистрибутиве Ubuntu 24.04, установленном через WSL2

### 0. Подготовка

Убедитесь, что в вашем дистрибутиве установлены и готовы к работе следующие инструменты.

1. Go 1.21+

2. Сервер БД PostgreSQL (версия 13+) и клиентские утилиты (в частности `psql`)

После проверки создайте отдельную папку (можете выбрать название по душе) и перейдите в неё

```bash
mkdir sandbox && cd sandbox
```

Создание отдельной папки будет особенно удобно в дальнейшем [при локальном тестировании](#локальное-тестирование)

### 1. Клонируйте репозиторий

Клонируйте этот репозиторий c помощью HTTP или SSH

```bash
git clone https://github.com/alexgul25/user-svc.git
```

```bash
git clone git@github.com:alexgul25/user-svc.git
```

### 2. Подготовьте к работе сервер PostgreSQL

Необходимо убедиться, что сервер БД запущен. Для этого используйте команду вывода всех установленных и настроенных кластеров PostgreSQL в системе

```bash
pg_lsclusters
```

Ниже пример вывода из моего дистрибутива. Главное, проверьте значение поля Status

```bash
Ver Cluster Port Status Owner    Data directory              Log file
16  main    5432 online postgres /var/lib/postgresql/16/main /var/log/postgresql/postgresql-16-main.log
```

Пример запуска и остановки конкретного сервера в моём дистрибутиве

```bash
sudo systemctl start postgresql@16-main
```

```bash
sudo systemctl stop postgresql@16-main
```

### 3. Создайте пользователя и БД для работы с сервером PostgreSQL

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

Если всё работает корректно, вас попросят ввести пароль для только что созданного пользователя, указав который, вы увидите следующий вывод

```bash
 ?column? 
----------
        1
(1 row)
```

### 4. Настройте окружение

Создайте `.env` файл в корне проекта и заполните его по аналогии с [.env.example](.env.example). Для переменных окружения БД используйте данные, созданные на прошлом шаге.

Ключи и секреты можно сгенерировать с помощью приведённой команды. Она печатает случайные байты в Base64 (32 байта → 44 символа)

```bash
openssl rand -base64 32
```

### 5. Примените миграции и запустите сервер

Перейдите в папку **user-svc** и выполните команды

```bash
go run ./cmd/migrator && go run ./cmd/svc-starter
```

Сервер слушает порт, заданный в переменной окружения `GRPCSERVER_PORT` (по умолчанию `50051`)

Чтобы остановить работу сервера, введите в терминале `CTRL + C`

## Локальное тестирование

Как и в разделе [Локальная установка и запуск](#локальная-установка-и-запуск), здесь приведены шаги для локального тестирования в дистрибутиве Ubuntu 24.04, установленном через WSL2

Находясь в папке, созданной при [подготовке к локальному запуску](#0-подготовка), клонируйте репозиторий [protos](https://github.com/alexgul25/protos) с помощью HTTP или SSH

```bash
git clone https://github.com/alexgul25/protos.git
```

```bash
git clone git@github.com:alexgul25/protos.git
```

Теперь [запустите сервер](#локальная-установка-и-запуск)

Далее приведены два способа тестирования, можете выбрать любой, исходя из своих предпочтений

### 1. Тестирование с помощью `grpcurl`

Необходимы утилиты `make` и `grpcurl`

Создайте новый терминал (при этом терминал с запущенным сервером должен продолжать работать)

Перейдите в папку **protos**, созданную при клонировании репозитория

Затем, находясь в папке `protos`, создайте бинарный файл **user_service_v1.protoset** с помощью утилиты `make` (если вы изменили .proto-файлы, эту команду нужно будет повторить снова)

```bash
make protoset
```

Протестируйте сервер. Ниже приведены примеры готовых запросов (**Важно!** В приведённых ниже командах адрес **user_service_v1.protoset** прописан с учётом того, что вы находитесь  в папке, созданной при [подготовке к локальному запуску](#0-подготовка))

- Зарегистрировать пользователя

```bash
grpcurl -plaintext \
  -protoset protos/user_service_v1.protoset \
  -H 'x-service-name: gateway-svc' \
  -d '{"email":"alex@example.com","password":"secret123","display_name":"Alex"}' \
  localhost:50051 user.v1.UserService/Register
```

- Выдать новый токен пользователю при успешной аутентификации

```bash
grpcurl -plaintext \
  -protoset protos/user_service_v1.protoset \
  -H 'x-service-name: gateway-svc' \
  -d '{"email":"alex@example.com","password":"secret123"}' \
  localhost:50051 user.v1.UserService/Login
```

- Получить свой профиль (вставьте id пользователя)

```bash
grpcurl -plaintext \
  -protoset protos/user_service_v1.protoset \
  -H 'x-service-name: gateway-svc' \
  -H 'x-user-id: <ваш user_id>' \
  -d '{}' \
  localhost:50051 user.v1.UserService/GetMyProfile
```

- Подписаться на другого пользователя (вставьте id пользователя и id целевого пользователя)

```bash
grpcurl -plaintext \
  -protoset protos/user_service_v1.protoset \
  -H 'x-service-name: gateway-svc' \
  -H 'x-user-id: <ваш user_id>' \
  -d '{"followee_id":"<ваш followee_id>"}' \
  localhost:50051 user.v1.UserService/Subscribe
```

- Отписаться от другого пользователя (вставьте id пользователя и id целевого пользователя)

```bash
grpcurl -plaintext \
  -protoset protos/user_service_v1.protoset \
  -H 'x-service-name: gateway-svc' \
  -H 'x-user-id: <ваш user_id>' \
  -d '{"followee_id":"<ваш followee_id>"}' \
  localhost:50051 user.v1.UserService/Unsubscribe
```

- Получить список подписчиков пользователя (вставьте id пользователя и имя сервиса (см. значения [SERVICES_WITH_EMAIL_HIDDEN](.env.example) для ответа с email или без))

```bash
grpcurl -plaintext \
  -protoset protos/user_service_v1.protoset \
  -H 'x-service-name: <имя сервиса>' \
  -d '{"user_id":"<ваш user_id>"}' \
  localhost:50051 user.v1.UserService/GetFollowers
```

- Посмотреть список доступных методов

```bash
grpcurl -plaintext \
  -protoset protos/user_service_v1.protoset \
  list user.v1.UserService
```

### 2. Тестирование с помощью [Postman](https://www.postman.com/)

Раздел активно разрабатывается в данный момент
