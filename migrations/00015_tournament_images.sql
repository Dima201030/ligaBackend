-- +goose Up
-- +goose StatementBegin
CREATE TABLE tournament_images (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id       UUID NOT NULL REFERENCES clubs (id) ON DELETE CASCADE,
    tournament_id UUID NOT NULL REFERENCES tournaments (id) ON DELETE CASCADE,
    file_id       UUID REFERENCES files (id) ON DELETE SET NULL,
    url           TEXT NOT NULL,
    sort_order    INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tournament_images_tournament_idx
    ON tournament_images (tournament_id, sort_order, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE tournament_images;
-- +goose StatementEnd
