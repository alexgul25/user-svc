-- +goose Up
CREATE TABLE IF NOT EXISTS subscriptions (
    follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, followee_id)
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_followee ON subscriptions(followee_id);

-- +goose Down
DROP TABLE IF EXISTS subscriptions;