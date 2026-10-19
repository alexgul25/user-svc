-- +goose Up
-- Email уникален без учёта регистра: Alice@example.com и alice@example.com - один адрес.
-- Если в таблице уже есть такие дубликаты, создание индекса завершится ошибкой:
-- их нужно разобрать вручную до применения миграции.
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

-- Старое ограничение уникальности стало лишним: новый индекс строже.
ALTER TABLE users DROP CONSTRAINT users_email_key;

-- +goose Down
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
DROP INDEX users_email_lower_key;
