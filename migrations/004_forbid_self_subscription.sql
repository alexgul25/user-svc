-- +goose Up
-- Подписка на самого себя запрещена бизнес-логикой. Ограничение в БД защищает
-- от таких записей, даже если проверка в коде будет пропущена.
-- Самоподписки, созданные до исправления в хэндлерах, ошибочны: удаляем их.
DELETE FROM subscriptions WHERE follower_id = followee_id;

ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_no_self_subscription CHECK (follower_id <> followee_id);

-- +goose Down
ALTER TABLE subscriptions DROP CONSTRAINT subscriptions_no_self_subscription;
