# :busts_in_silhouette: User Service

Микросервис для проекта **Date Wishlist Hub**.

Ссылка на центральный репозиторий проекта: **[Date Wishlist Hub Deploy](https://github.com/alexgul25/date-wishlist-hub-deploy)**

Ссылка на канбан-доску проекта: **[Date Wishlist Hub - Development](https://github.com/users/alexgul25/projects/2)**

*Стек технологий сервиса:* `Go`  `gRPC`  `PostgreSQL`

## :bulb: Описание сервиса

**User Service** - внутренний gRPC-сервер, организующий логику работы с данными о пользователях и подписках.

- Protobuf-контракты определены публично в **[Protos](https://github.com/alexgul25/protos)**.

- Только генерация новых JWT-токенов, проверка существующих делегирована **[Gateway Service](https://github.com/alexgul25/gateway-svc)**.

- В качестве БД используется `PostgreSQL`.

- Методы **не должны** быть доступны пользователям напрямую (см. [архитектуру проекта](https://github.com/alexgul25/date-wishlist-hub-deploy#building_construction-архитектура-проекта)).

***Таблица gRPC-методов.***

| Method Name            | Auth | Calling service  | Info                                                                                |
| :--------------------: | :--: | :--------------: | ----------------------------------------------------------------------------------- |
| Register               | ❌   | Gateway Service  | Регистрация нового пользователя                                                     |
| Login                  | ❌   | Gateway Service  | Аутентификация зарегестрированного пользователя, возвращает JWT-токен               |
| GetMyProfile           | ✅   | Gateway Service  | Получение данных собственного профиля пользователя                                  |
| FindUsersByDisplayName | ✅   | Gateway Service  | Поиск пользователей по отображаемому имени                                          |
| Subscribe              | ✅   | Gateway Service  | Подписка на другого пользователя                                                    |
| Unsubscribe            | ✅   | Gateway Service  | Отписка от другого пользователя                                                     |
| GetFollowers           | ✅   | Gateway Service  | Получение пользователем списка подписок другого пользователя по ID (email скрыт)    |
| GetFollowers           | -    | Notify Service   | Получение внутренним сервисом списка подписок пользователя по ID                    |

<!-- markdownlint-disable MD033 -->
<details>
<summary>Примечания</summary>

- Все запросы для **User Service** должны передавать заголовок `x-service-name` - имя сервиса, вызывающего метод (Calling service).

- Заполненный столбец `Auth` указывает:
    1. вызов метода инициирован пользователем;
    2. ✅ и ❌ - соответственно нужен или не нужен JWT-токен для успешного вызова.

- Для методов, требующих идентификации через JWT-токен, необходимо передавать заголовок `x-user-id`.

</details>
<!-- markdownlint-enable MD033 -->

## :gear: Структура сервиса

:open_file_folder: [./cmd](./cmd/) - команды для запуска приложения.

:open_file_folder: [./migrations](./migrations/) - миграции для БД.

:open_file_folder: [./internal/app](./internal/app/) - код для запуска различных компонентов приложения.

:open_file_folder: [./internal/domain](./internal/domain/) - структуры данных и модели домена.

:open_file_folder: [./internal/grpc/handlers](./internal/grpc/handlers/) - **gPRC-хэндлеры**.

:open_file_folder: [./internal/grpc/interceptors](./internal/grpc/interceptors/) - gRPC-интерсепторы.

:open_file_folder: [./internal/lib](./internal/lib/) - общие вспомогательные утилиты и функции.

:open_file_folder: [./internal/service](./internal/service/) - **сервисный слой (бизнес-логика)**.

:open_file_folder: [./internal/storage](./internal/storage/) - **слой хранения данных**.

## Локальная установка и запуск

Ниже приведены шаги для запуска проекта в дистрибутиве Ubuntu 24.04, установленном через WSL2

### 0. Подготовка

Убедитесь, что в вашем дистрибутиве установлены и готовы к работе следующие инструменты.

1. Актуальная для проекта версия Go (см. [go.mod](./go.mod))

2. Сервер БД PostgreSQL (версия 13+) и клиентские утилиты для него (в частности `psql`)

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
