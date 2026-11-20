package mail

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

type memSender struct {
	mu   sync.Mutex
	sent []Message
	err  error
}

func (m *memSender) Send(to string, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

func testStore(t *testing.T) *db.Store {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := db.Connect(ctx, url)
	if err != nil {
		t.Skipf("db unavailable: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestProcessOnceSendsQueued(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	_, err := store.Queries.EnqueueEmail(ctx, sqlc.EnqueueEmailParams{
		To:       "player@example.com",
		Template: "confirm_email",
		Payload:  []byte(`{"first_name":"Тест","token":"abc"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	sender := &memSender{}
	w := &Worker{
		Store:       store,
		Cfg:         config.Config{SMTPHost: "localhost", SMTPFrom: "liga@example.com"},
		Sender:      sender,
		BatchSize:   10,
		MaxAttempts: 5,
	}
	n, err := w.ProcessOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("sent=%d", n)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	last := sender.sent[len(sender.sent)-1]
	if !strings.Contains(last.TextBody, "Тест") {
		t.Fatalf("body=%q", last.TextBody)
	}
}
