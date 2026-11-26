package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL required")
	}
	ctx := context.Background()
	store, err := db.Connect(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	if err := seedOrganizer(ctx, store); err != nil {
		log.Fatal(err)
	}

	pass := "LoadTest!Pass99"
	hash, err := crypto.HashPassword(pass)
	if err != nil {
		log.Fatal(err)
	}
	clubID := clubs.DefaultUUID()
	created := 0
	for i := 1; i <= 100; i++ {
		email := fmt.Sprintf("load%03d@pairly-load.local", i)
		_, err := store.Queries.GetUserByEmail(ctx, pgtype.Text{String: email, Valid: true})
		if err == nil {
			continue
		}
		if err != nil && err != pgx.ErrNoRows {
			// continue only on no rows; other errors from GetUserByEmail may wrap
		}
		tx, err := store.Pool.Begin(ctx)
		if err != nil {
			log.Fatal(err)
		}
		q := store.Queries.WithTx(tx)
		user, err := q.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
			Email:        pgtype.Text{String: email, Valid: true},
			PasswordHash: hash,
			FirstName:    fmt.Sprintf("Load%03d", i),
			LastName:     "Pairly",
			Role:         sqlc.UserRolePlayer,
			Status:       sqlc.UserStatusApproved,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("user %s: %v", email, err)
		}
		phone := pgtype.Text{}
		switch i % 10 {
		case 1:
			phone = pgtype.Text{String: "+79991234567", Valid: true}
		case 2:
			phone = pgtype.Text{String: "+79031234567", Valid: true}
		case 3:
			phone = pgtype.Text{String: "000-00-00", Valid: true}
		}
		if _, err := q.CreateProfile(ctx, rating.NewProfileParams(
			user.ID, clubID, phone, "rookies",
			fmt.Sprintf("Load%03d", i), "Pairly",
		)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("profile %s: %v", email, err)
		}
		if _, err := q.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
			ClubID: clubID,
			UserID: user.ID,
			Role:   sqlc.ClubMemberRolePlayer,
			Status: sqlc.ClubMemberStatusActive,
		}); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("membership %s: %v", email, err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Fatal(err)
		}
		created++
	}
	fmt.Printf("created=%d password=%s domain=@pairly-load.local\n", created, pass)
}

func seedOrganizer(ctx context.Context, store *db.Store) error {
	email := "organizer@pairly-load.local"
	pass := "LoadOrg!Pass99"
	_, err := store.Queries.GetUserByEmail(ctx, pgtype.Text{String: email, Valid: true})
	if err == nil {
		fmt.Println("organizer exists")
		return nil
	}
	hash, err := crypto.HashPassword(pass)
	if err != nil {
		return err
	}
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)
	user, err := q.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email: pgtype.Text{String: email, Valid: true}, PasswordHash: hash,
		FirstName: "Org", LastName: "Load", Role: sqlc.UserRoleOrganizer, Status: sqlc.UserStatusApproved,
	})
	if err != nil {
		return err
	}
	clubID := clubs.DefaultUUID()
	if _, err := q.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubID, pgtype.Text{}, "masters", "Org", "Load")); err != nil {
		return err
	}
	if _, err := q.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: clubID, UserID: user.ID, Role: sqlc.ClubMemberRoleOrganizer, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("organizer ok email=%s\n", email)
	return nil
}
