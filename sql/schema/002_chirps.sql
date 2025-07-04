-- +goose Up
CREATE TABLE chirps
(
    id         UUID PRIMARY KEY,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    body       TEXT      NOT NULL,
    user_id    UUID      NOT NULL REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX idx_chirps_user_id ON chirps (user_id);

-- +goose Down
DROP TABLE chirps;