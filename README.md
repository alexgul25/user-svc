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

В данном разделе приведена инструкция по запуску одного **User Service**.

Инструкция по запуску всего проекта целиком доступна по **[ссылке](https://github.com/alexgul25/date-wishlist-hub-deploy#desktop_computer-локальный-запуск-и-работа-через-терминал)**.

### 1. Подготовка окружения

В вашем дистрибутиве должны быть установлены и готовы к работе:

- актуальная для проекта версия Go (см. [go.mod](./go.mod));

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

### 4. Запуск и работа

Для удобства локальной работы в корне репозитория определён Makefile.

1. `make help` - узнайте о доступных командах.

2. `make protoset` - сгенерируйте protoset (обязательно перед локальной отправкой запросов).

3. `make run` - примените миграции, соберите бинарник и запустите gRPC-сервер.

4. `CTRL + C` - пошлите серверу сигнал завершения, когда закончите работу.

В отдельном терминале перейдите в корневую папку репозитория и посылайте запросы на сервер.

- `make register` - зарегистрируйте пользователя (для удобства полученный ID будет сохранён в файл `.user_id` и использоваться для запросов, требующих авторизации).

- `make login` - авторизуйтесь (возвращает JWT-токен, который должен проверяться в **[Gateway Service](https://github.com/alexgul25/gateway-svc)**).

- `make set-user` - установить ID конкретного пользователя.

- `make search`, `make subscribe` и т.д. - вызывайте gRPC-методы.

- `make clean` - выполните, чтобы удалить сохранённые бинарник и ID пользователя.
