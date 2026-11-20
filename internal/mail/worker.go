package mail

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	DefaultBatchSize   = 20
	DefaultMaxAttempts = 8
	DefaultPollEvery   = 15 * time.Second
)

// Worker drains email_outbox via SMTP (or a test Sender).
type Worker struct {
	Store       *db.Store
	Cfg         config.Config
	Sender      Sender
	BatchSize   int32
	MaxAttempts int32
	PollEvery   time.Duration
	Log         *log.Logger
}

// NewWorker builds a worker with SMTP sender when configured.
func NewWorker(store *db.Store, cfg config.Config) *Worker {
	var sender Sender
	if cfg.HasSMTP() {
		sender = NewSMTPSender(cfg)
	}
	return &Worker{
		Store:       store,
		Cfg:         cfg,
		Sender:      sender,
		BatchSize:   DefaultBatchSize,
		MaxAttempts: DefaultMaxAttempts,
		PollEvery:   DefaultPollEvery,
		Log:         log.Default(),
	}
}

// Run polls until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	if w.Sender == nil {
		return fmt.Errorf("mail worker: SMTP not configured (set SMTP_HOST and SMTP_FROM)")
	}
	if w.BatchSize <= 0 {
		w.BatchSize = DefaultBatchSize
	}
	if w.MaxAttempts <= 0 {
		w.MaxAttempts = DefaultMaxAttempts
	}
	if w.PollEvery <= 0 {
		w.PollEvery = DefaultPollEvery
	}
	w.logf("mail worker started (batch=%d poll=%s)", w.BatchSize, w.PollEvery)

	ticker := time.NewTicker(w.PollEvery)
	defer ticker.Stop()

	if _, err := w.ProcessOnce(ctx); err != nil {
		w.logf("process: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			w.logf("mail worker stopped")
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.ProcessOnce(ctx); err != nil {
				w.logf("process: %v", err)
			}
		}
	}
}

// ProcessOnce claims and sends one batch. Returns sent count.
func (w *Worker) ProcessOnce(ctx context.Context) (int, error) {
	if w.Sender == nil {
		return 0, fmt.Errorf("sender not set")
	}
	tx, err := w.Store.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := w.Store.Queries.WithTx(tx)

	rows, err := q.ClaimPendingEmails(ctx, sqlc.ClaimPendingEmailsParams{
		Attempts: w.MaxAttempts, // max attempts threshold ($1)
		Limit:    w.BatchSize,
	})
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	sent := 0
	for _, row := range rows {
		msg, err := Render(row.Template, row.Payload)
		if err != nil {
			_, _ = w.Store.Queries.MarkEmailFailed(ctx, sqlc.MarkEmailFailedParams{
				ID:       row.ID,
				Attempts: w.MaxAttempts,
				Error:    pgtype.Text{String: err.Error(), Valid: true},
			})
			continue
		}
		if err := w.Sender.Send(row.To, msg); err != nil {
			_, _ = w.Store.Queries.MarkEmailFailed(ctx, sqlc.MarkEmailFailedParams{
				ID:       row.ID,
				Attempts: w.MaxAttempts,
				Error:    pgtype.Text{String: truncateErr(err.Error(), 500), Valid: true},
			})
			w.logf("send failed id=%s template=%s: %v", uuidShort(row.ID), row.Template, err)
			continue
		}
		if _, err := w.Store.Queries.MarkEmailSent(ctx, row.ID); err != nil {
			w.logf("mark sent id=%s: %v", uuidShort(row.ID), err)
			continue
		}
		sent++
		w.logf("sent id=%s template=%s to=%s", uuidShort(row.ID), row.Template, row.To)
	}
	return sent, nil
}

func (w *Worker) logf(format string, args ...any) {
	if w.Log != nil {
		w.Log.Printf(format, args...)
	}
}

func truncateErr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func uuidShort(id pgtype.UUID) string {
	if !id.Valid {
		return "?"
	}
	return fmt.Sprintf("%x", id.Bytes[0:4])
}

// PendingCount is a small helper for health/status.
func PendingCount(ctx context.Context, store *db.Store) (int32, error) {
	n, err := store.Queries.CountOutboxByStatus(ctx, sqlc.EmailOutboxStatusPending)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}
