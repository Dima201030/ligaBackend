package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	email := strings.ToLower(strings.TrimSpace(os.Getenv("SEED_ORGANIZER_EMAIL")))
	password := os.Getenv("SEED_ORGANIZER_PASSWORD")
	firstName := strings.TrimSpace(envOr("SEED_ORGANIZER_FIRST_NAME", "Организатор"))
	lastName := strings.TrimSpace(envOr("SEED_ORGANIZER_LAST_NAME", "Лиги"))
	level := strings.TrimSpace(envOr("SEED_ORGANIZER_LEVEL", "rookies"))

	if email == "" || password == "" {
		log.Fatal("SEED_ORGANIZER_EMAIL and SEED_ORGANIZER_PASSWORD are required")
	}
	if len([]rune(password)) < 8 {
		log.Fatal("SEED_ORGANIZER_PASSWORD must be at least 8 characters")
	}

	ctx := context.Background()
	store, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()

	hash, err := crypto.HashPassword(password)
	if err != nil {
		log.Fatalf("hash: %v", err)
	}

	existing, err := store.Queries.GetUserByEmail(ctx, email)
	if err == nil {
		reset := strings.TrimSpace(os.Getenv("SEED_ORGANIZER_RESET")) == "1"
		if !reset {
			fmt.Printf("organizer already exists: id=%s email=%s status=%s role=%s\n",
				uuidString(existing.ID), existing.Email, existing.Status, existing.Role)
			fmt.Println("set SEED_ORGANIZER_RESET=1 to update password/role/status")
			return
		}
		_, err = store.Pool.Exec(ctx, `
			UPDATE users
			SET password_hash = $1,
			    first_name = $2,
			    last_name = $3,
			    role = 'organizer',
			    status = 'approved',
			    email_verified_at = COALESCE(email_verified_at, now()),
			    updated_at = now()
			WHERE id = $4
		`, hash, firstName, lastName, existing.ID)
		if err != nil {
			log.Fatalf("reset organizer: %v", err)
		}
		fmt.Printf("updated organizer: id=%s email=%s role=organizer status=approved\n",
			uuidString(existing.ID), existing.Email)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("lookup: %v", err)
	}

	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	user, err := q.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email:        email,
		PasswordHash: hash,
		FirstName:    firstName,
		LastName:     lastName,
		Role:         sqlc.UserRoleOrganizer,
		Status:       sqlc.UserStatusApproved,
	})
	if err != nil {
		log.Fatalf("create user: %v", err)
	}
	clubID := clubs.DefaultUUID()
	if _, err := q.CreateProfile(ctx, rating.NewProfileParams(
		user.ID, clubID, pgtype.Text{}, level, firstName, lastName,
	)); err != nil {
		log.Fatalf("create profile: %v", err)
	}
	if _, err := q.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: clubID,
		UserID: user.ID,
		Role:   sqlc.ClubMemberRoleOrganizer,
		Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		log.Fatalf("create membership: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}

	fmt.Printf("seeded organizer: id=%s email=%s role=%s status=%s\n",
		uuidString(user.ID), user.Email, user.Role, user.Status)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
