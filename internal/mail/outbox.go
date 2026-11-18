package mail

import (
	"context"
	"encoding/json"
	"log"

	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

// Enqueue writes a message to email_outbox. Missing SMTP never fails the caller:
// the row stays pending for a future worker.
func Enqueue(ctx context.Context, store *db.Store, cfg config.Config, to, template string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = store.Queries.EnqueueEmail(ctx, sqlc.EnqueueEmailParams{
		To:       to,
		Template: template,
		Payload:  raw,
	})
	if err != nil {
		return err
	}
	if !cfg.HasSMTP() {
		log.Printf("email_outbox: queued template=%s (smtp not configured)", template)
		return nil
	}
	log.Printf("email_outbox: queued template=%s (smtp configured, delivery deferred)", template)
	return nil
}
