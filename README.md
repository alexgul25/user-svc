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

:open_file_folder: **[./cmd](./cmd/)** - команды для запуска приложения.

:open_file_folder: **[./migrations](./migrations/)** - миграции для БД.

:open_file_folder: **[./internal/app](./internal/app/)** - код для запуска различных компонентов приложения.

:open_file_folder: **[./internal/domain](./internal/domain/)** - структуры данных и модели домена.

:open_file_folder: **[./internal/grpc/handlers](./internal/grpc/handlers/)** - **gPRC-хэндлеры**.

:open_file_folder: **[./internal/grpc/interceptors](./internal/grpc/interceptors/)** - gRPC-интерсепторы.

:open_file_folder: **[./internal/lib](./internal/lib/)** - общие вспомогательные утилиты и функции.

:open_file_folder: **[./internal/service](./internal/service/)** - **сервисный слой (бизнес-логика)**.

:open_file_folder: **[./internal/storage](./internal/storage/)** - **слой хранения данных**.

## :desktop_computer: Локальный запуск и работа через терминал

В данном разделе приведена инструкция по запуску одного **User Service**. Сервис можно запустить двумя способами: собрать и запустить бинарник локально или запустить в Docker-контейнере.

Инструкция по запуску всего проекта целиком доступна по **[ссылке](https://github.com/alexgul25/date-wishlist-hub-deploy#desktop_computer-локальный-запуск-и-работа-через-терминал)**.

### 1. Подготовка окружения

В вашем дистрибутиве должны быть установлены и готовы к работе:

- актуальная для проекта версия Go (см. [go.mod](./go.mod)) - для локального запуска;
- Docker Engine с плагином `buildx` - для запуска в контейнере;
- сервер PostgreSQL (версия 13+) и утилита `psql`;
- компилятор Protocol Buffers (`protoc`);
- утилита `grpcurl`;
- утилита `jq`;
- утилита `make`.

### 2. Клонирование нужных репозиториев

***ВАЖНО!*** Репозитории должны быть клонированы **в одну и ту же папку**.

- Клонируйте этот репозиторий c помощью HTTP или SSH.

```bash
git clone https://github.com/alexgul25/user-svc.git
```

```bash
git clone git@github.com:alexgul25/user-svc.git
```

- Клонируйте репозиторий **[Protos](https://github.com/alexgul25/protos)** с помощью HTTP или SSH. С его помощью будет сгенерирован protoset-файл, необходимый для отправки gRPC-запросов через терминал (он нужен, поскольку User Service не поддерживает reflection).

```bash
git clone https://github.com/alexgul25/protos.git
```

```bash
git clone git@github.com:alexgul25/protos.git
```

### 3. Настройка PostgreSQL и файлов конфигурации

Запустите сервер PostgreSQL. Затем создайте пользователя и базу данных для **User Service**.

```bash
sudo -u postgres psql -c "CREATE USER <имя пользователя> WITH PASSWORD '<пароль>';"
```

```bash
sudo -u postgres psql -c "CREATE DATABASE <имя БД> OWNER <имя пользователя>;"
```

Проверьте доступ.

```bash
psql -h localhost -U <имя пользователя> -d <имя БД> -c "SELECT 1;"
```

Если всё работает корректно, вы увидите следующий вывод:

```bash
 ?column? 
----------
        1
(1 row)
```

***ВАЖНО!*** Создайте в корневой папке репозитория файл `.env` для переменных окружения и заполните его (см [.env.example](.env.example)). Для переменных `DB_USER`, `DB_PASSWORD` и `DB_NAME` используйте значения, созданные на этом шаге. `JWT_SECRET` можно сгенерировать с помощью команды:

```bash
openssl rand -base64 32
```

<!-- markdownlint-disable MD033 -->
<details>
<summary>Особенности .env при запуске в Docker</summary>

- Значения указывайте без кавычек: Docker передаёт их в контейнер как есть, вместе с кавычками.
- `localhost` в `DB_HOST` внутри контейнера означает сам контейнер, а не вашу машину (см. подсказки к варианту запуска в Docker в [следующем шаге](#4-запуск-и-работа)).

</details>
<!-- markdownlint-enable MD033 -->

### 4. Запуск и работа

Для удобства локальной работы в корне репозитория определён Makefile.

1. `make help` - узнайте о доступных командах.
2. `make protoset` - сгенерируйте protoset (обязательно перед локальной отправкой запросов).

#### Вариант 1. Локальный запуск

1. `make run` - примените миграции, соберите бинарник и запустите gRPC-сервер.
2. `CTRL + C` - пошлите серверу сигнал завершения, когда закончите работу.

#### Вариант 2. Запуск в Docker

В корне репозитория определены **[Dockerfile](./Dockerfile)** и **[.dockerignore](./.dockerignore)**. Итоговый образ содержит бинарник сервиса, бинарник мигратора и папку с миграциями, переменные окружения передаются в контейнер при запуске.

1. `docker build -t user-svc .` - соберите образ.
2. `docker run --rm --network host --env-file .env --entrypoint /app/migrator user-svc` - примените миграции.
3. `docker run --rm --name user-svc --network host --env-file .env user-svc` - запустите контейнер с gRPC-сервером.
4. `CTRL + C` или `docker stop user-svc` из другого терминала - пошлите серверу сигнал завершения, когда закончите работу.

<!-- markdownlint-disable MD033 -->
<details>
<summary>Подсказки</summary>

- Флаг `--network host` запускает контейнер в сети вашей машины: `localhost` в `DB_HOST` указывает на локальный PostgreSQL, а gRPC-сервер доступен на `localhost:<GRPCSERVER_PORT>`, поэтому команды Makefile для отправки запросов работают без изменений. Режим работает в Docker Engine на Linux (в том числе в WSL2).
- Если PostgreSQL доступен контейнеру по сети (например, запущен в другом контейнере), вместо `--network host` опубликуйте порт сервера: `-p <порт>:<порт>`, где порт совпадает с `GRPCSERVER_PORT`, и укажите в `DB_HOST` адрес сервера PostgreSQL.
- Если при сборке не удаётся скачать Go-модули (например, `proxy.golang.org` недоступен), передайте другой прокси через аргумент сборки: `docker build --build-arg GOPROXY=https://goproxy.io,direct -t user-svc .`
- Чтобы запустить контейнер в фоне, замените `--rm` на `-d`. Логи сервиса можно посмотреть командой `docker logs -f user-svc`, остановить и удалить контейнер - командами `docker stop user-svc` и `docker rm user-svc`.
- Проверить состояние запущенного сервиса можно командой `docker exec user-svc /bin/grpc_health_probe -addr=:<порт>`, где порт совпадает с `GRPCSERVER_PORT`. Сервис отвечает по стандартному протоколу gRPC Health Checking.

</details>
<!-- markdownlint-enable MD033 -->

#### Работа с API

В отдельном терминале перейдите в корневую папку репозитория и посылайте запросы на сервер.

- `make register` - зарегистрируйте пользователя (для удобства полученный ID будет сохранён в файл `.user_id` и использоваться для запросов, требующих авторизации).
- `make login` - авторизуйтесь (возвращает JWT-токен, который должен проверяться в **[Gateway Service](https://github.com/alexgul25/gateway-svc)**).
- `make set-user` - установить ID конкретного пользователя.
- `make search`, `make subscribe` и т.д. - вызывайте gRPC-методы.
- `make clean` - выполните, чтобы удалить сохранённые бинарник и ID пользователя.
