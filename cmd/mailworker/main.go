package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/mail"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !cfg.HasSMTP() {
		log.Fatal("SMTP_HOST and SMTP_FROM are required for mailworker")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()

	w := mail.NewWorker(store, cfg)
	if v := strings.TrimSpace(os.Getenv("MAIL_POLL_SECONDS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Fatalf("MAIL_POLL_SECONDS: invalid %q", v)
		}
		w.PollEvery = time.Duration(n) * time.Second
	}
	if v := strings.TrimSpace(os.Getenv("MAIL_BATCH_SIZE")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Fatalf("MAIL_BATCH_SIZE: invalid %q", v)
		}
		w.BatchSize = int32(n)
	}

	if err := w.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("worker: %v", err)
	}
}
