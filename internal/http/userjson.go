package httpapi

import (
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type userJSON struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	FirstName     string  `json:"first_name"`
	LastName      string  `json:"last_name"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	EmailVerified bool    `json:"email_verified"`
	Level         string  `json:"level"`
	Phone         *string `json:"phone,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
}

func sessionUserJSON(u *auth.SessionUser) userJSON {
	var phone *string
	if u.Phone != nil {
		phone = publicPhoneString(*u.Phone)
	}
	return userJSON{
		ID:            uuidString(u.ID),
		Email:         u.Email,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		Role:          string(u.Role),
		Status:        string(u.Status),
		EmailVerified: u.EmailVerifiedAt.Valid,
		Level:         u.Level,
		Phone:         phone,
	}
}

func userWithProfileJSON(u sqlc.User, level string, phone pgtype.Text, created, updated pgtype.Timestamptz) userJSON {
	out := userJSON{
		ID:            uuidString(u.ID),
		Email:         emailString(u.Email),
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		Role:          string(u.Role),
		Status:        string(u.Status),
		EmailVerified: u.EmailVerifiedAt.Valid,
		Level:         level,
		Phone:         publicPhone(phone),
	}
	if created.Valid {
		out.CreatedAt = created.Time.UTC().Format(time.RFC3339)
	}
	if updated.Valid {
		out.UpdatedAt = updated.Time.UTC().Format(time.RFC3339)
	}
	return out
}

type applicationJSON struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	FirstName     string  `json:"first_name"`
	LastName      string  `json:"last_name"`
	Status        string  `json:"status"`
	Level         string  `json:"level"`
	Phone         *string `json:"phone,omitempty"`
	EmailVerified bool    `json:"email_verified"`
	SubmittedAt   string  `json:"submitted_at"`
	ReviewedAt    *string `json:"reviewed_at,omitempty"`
}

func applicationFromRow(row sqlc.ListApplicationsRow) applicationJSON {
	out := applicationJSON{
		ID:            uuidString(row.ID),
		Email:         emailString(row.Email),
		FirstName:     row.FirstName,
		LastName:      row.LastName,
		Status:        string(row.Status),
		Level:         row.Level,
		Phone:         publicPhone(row.Phone),
		EmailVerified: row.EmailVerifiedAt.Valid,
		SubmittedAt:   row.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if row.Status != sqlc.UserStatusPending && row.UpdatedAt.Valid {
		ts := row.UpdatedAt.Time.UTC().Format(time.RFC3339)
		out.ReviewedAt = &ts
	}
	return out
}
