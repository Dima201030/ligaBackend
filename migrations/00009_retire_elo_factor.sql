-- +goose Up
-- +goose StatementBegin
UPDATE rating_configs
SET coefficients = coefficients || '{"retire_elo_factor": 0.5}'::jsonb
WHERE NOT (coefficients ? 'retire_elo_factor');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE rating_configs
SET coefficients = coefficients - 'retire_elo_factor'
WHERE coefficients ? 'retire_elo_factor';
-- +goose StatementEnd
