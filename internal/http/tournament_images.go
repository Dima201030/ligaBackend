package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxTournamentImages = 12

type tournamentImageJSON struct {
	ID        string `json:"id"`
	FileID    string `json:"file_id,omitempty"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

// listTournamentImages returns the gallery for a tournament, oldest first.
func (s *Server) listTournamentImages(r *http.Request, tournamentID pgtype.UUID) ([]tournamentImageJSON, error) {
	rows, err := s.store.Pool.Query(r.Context(), `
		SELECT id, file_id, url, created_at
		FROM tournament_images
		WHERE club_id = $1 AND tournament_id = $2
		ORDER BY sort_order, created_at`, s.currentClubID(r), tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]tournamentImageJSON, 0, 4)
	for rows.Next() {
		var id, url string
		var fileID pgtype.UUID
		var created time.Time
		if err := rows.Scan(&id, &fileID, &url, &created); err != nil {
			return nil, err
		}
		item := tournamentImageJSON{ID: id, URL: url, CreatedAt: created.UTC().Format(time.RFC3339)}
		if fileID.Valid {
			item.FileID = uuidString(fileID)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Server) handleUploadTournamentImage(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	if s.files == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads_disabled", "Загрузка файлов недоступна")
		return
	}
	tournamentID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор турнира")
		return
	}
	if _, err := s.loadClubTournament(r, tournamentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турнир")
		return
	}

	var count int64
	if err := s.store.Pool.QueryRow(r.Context(),
		`SELECT count(*) FROM tournament_images WHERE club_id = $1 AND tournament_id = $2`,
		s.currentClubID(r), tournamentID).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить галерею")
		return
	}
	if count >= int64(maxTournamentImages) {
		writeError(w, http.StatusConflict, "limit_reached", "В галерее уже максимальное число изображений")
		return
	}

	fileRow, url, err := s.uploadAndCreate(r, actor.ID)
	if err != nil {
		mapUploadError(w, err)
		return
	}

	var id string
	var created time.Time
	err = s.store.Pool.QueryRow(r.Context(), `
		INSERT INTO tournament_images (club_id, tournament_id, file_id, url, sort_order)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		s.currentClubID(r), tournamentID, fileRow.ID, url, count).Scan(&id, &created)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить изображение")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"image": tournamentImageJSON{
			ID:        id,
			FileID:    uuidString(fileRow.ID),
			URL:       url,
			CreatedAt: created.UTC().Format(time.RFC3339),
		},
	})
}

func (s *Server) handleDeleteTournamentImage(w http.ResponseWriter, r *http.Request) {
	tournamentID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор турнира")
		return
	}
	imageID, err := parseUUID(chi.URLParam(r, "imageID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор изображения")
		return
	}
	tag, err := s.store.Pool.Exec(r.Context(),
		`DELETE FROM tournament_images WHERE club_id = $1 AND tournament_id = $2 AND id = $3`,
		s.currentClubID(r), tournamentID, imageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить изображение")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "Изображение не найдено")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
