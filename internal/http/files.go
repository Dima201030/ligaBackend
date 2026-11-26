package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/files"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type fileJSON struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	Bucket       string  `json:"bucket"`
	Path         string  `json:"path"`
	OriginalName string  `json:"original_name"`
	MIME         string  `json:"mime"`
	Size         int64   `json:"size"`
	Width        *int32  `json:"width,omitempty"`
	Height       *int32  `json:"height,omitempty"`
	Checksum     string  `json:"checksum"`
	CreatedAt    string  `json:"created_at"`
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	if s.files == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads_disabled", "Загрузка файлов недоступна")
		return
	}

	max := s.cfg.MaxUploadBytes
	if max <= 0 {
		max = 5 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, max+4096)
	if err := r.ParseMultipartForm(max + 4096); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "Файл слишком большой — максимум 5 МБ")
			return
		}
		writeError(w, http.StatusBadRequest, "validation", "Файл слишком большой или форма некорректна")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Ожидается multipart поле file")
		return
	}
	defer file.Close()

	meta, err := s.files.Save(file, hdr.Filename)
	if err != nil {
		mapUploadError(w, err)
		return
	}

	var width, height pgtype.Int4
	if meta.Width > 0 {
		width = pgtype.Int4{Int32: int32(meta.Width), Valid: true}
	}
	if meta.Height > 0 {
		height = pgtype.Int4{Int32: int32(meta.Height), Valid: true}
	}

	row, err := s.store.Queries.CreateFile(r.Context(), sqlc.CreateFileParams{
		
		ClubID: s.currentClubID(r),Bucket:       meta.Bucket,
		Path:         meta.RelPath,
		OriginalName: meta.OriginalName,
		Mime:         meta.MIME,
		Size:         meta.Size,
		Width:        width,
		Height:       height,
		Checksum:     meta.Checksum,
		UploadedBy:   actor.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить файл")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": fileFromRow(row)})
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	row, err := s.store.Queries.GetFileByClubAndID(r.Context(), sqlc.GetFileByClubAndIDParams{ClubID: s.currentClubID(r), ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Файл не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить файл")
		return
	}
	if r.URL.Query().Get("meta") == "1" {
		writeJSON(w, http.StatusOK, map[string]any{"file": fileFromRow(row)})
		return
	}
	if s.files == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads_disabled", "Файлы недоступны")
		return
	}
	f, err := s.files.Open(row.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Файл не найден на диске")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", row.Mime)
	w.Header().Set("Content-Length", strconv.FormatInt(row.Size, 10))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", `"`+files.ChecksumHex(row.Checksum)+`"`)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, files.ChecksumHex(row.Checksum)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, row.OriginalName, time.Time{}, f)
}

func (s *Server) handleAttachTournamentCover(w http.ResponseWriter, r *http.Request) {
	s.attachCover(w, r, "tournament")
}

func (s *Server) handleClearTournamentCover(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
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
	row, err := s.store.Queries.ClearTournamentCover(r.Context(), sqlc.ClearTournamentCoverParams{
		ClubID: s.currentClubID(r),
		ID:     tournamentID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить обложку")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cover_url": textPtr(row.CoverUrl),
	})
}

func (s *Server) handleAttachNewsCover(w http.ResponseWriter, r *http.Request) {
	s.attachCover(w, r, "news")
}

func (s *Server) handleAttachEventCover(w http.ResponseWriter, r *http.Request) {
	s.attachCover(w, r, "event")
}

func (s *Server) handleAttachPlayerAvatar(w http.ResponseWriter, r *http.Request) {
	s.attachCover(w, r, "avatar")
}

func (s *Server) attachCover(w http.ResponseWriter, r *http.Request, kind string) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	if s.files == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads_disabled", "Загрузка файлов недоступна")
		return
	}

	entityID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}

	if kind == "tournament" {
		if _, err := s.loadClubTournament(r, entityID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турнир")
			return
		}
	}

	fileRow, url, err := s.uploadAndCreate(r, actor.ID)
	if err != nil {
		mapUploadError(w, err)
		return
	}

	switch kind {
	case "tournament":
		row, err := s.store.Queries.SetTournamentCover(r.Context(), sqlc.SetTournamentCoverParams{
			ClubID:      s.currentClubID(r),
			ID:          entityID,
			CoverFileID: fileRow.ID,
			CoverUrl:    pgtype.Text{String: url, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось привязать обложку")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"file":      fileFromRow(fileRow),
			"cover_url": textPtr(row.CoverUrl),
		})
	case "news":
		row, err := s.store.Queries.SetNewsCover(r.Context(), sqlc.SetNewsCoverParams{
			ID:          entityID,
			CoverFileID: fileRow.ID,
			CoverUrl:    pgtype.Text{String: url, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "Новость не найдена")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось привязать обложку")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"file":      fileFromRow(fileRow),
			"cover_url": textPtr(row.CoverUrl),
		})
	case "event":
		row, err := s.store.Queries.SetEventCover(r.Context(), sqlc.SetEventCoverParams{
			ID:          entityID,
			CoverFileID: fileRow.ID,
			CoverUrl:    pgtype.Text{String: url, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "Событие не найдено")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось привязать обложку")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"file":      fileFromRow(fileRow),
			"cover_url": textPtr(row.CoverUrl),
		})
	case "avatar":
		row, err := s.store.Queries.SetProfileAvatar(r.Context(), sqlc.SetProfileAvatarParams{
			UserID:       entityID,
			AvatarFileID: fileRow.ID,
			AvatarUrl:    pgtype.Text{String: url, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not_found", "Игрок не найден")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось привязать аватар")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"file":       fileFromRow(fileRow),
			"avatar_url": textPtr(row.AvatarUrl),
		})
	default:
		writeError(w, http.StatusInternalServerError, "internal", "unknown attach kind")
	}
}

func (s *Server) uploadAndCreate(r *http.Request, actorID pgtype.UUID) (sqlc.File, string, error) {
	max := s.cfg.MaxUploadBytes
	if max <= 0 {
		max = 5 << 20
	}
	r.Body = http.MaxBytesReader(nil, r.Body, max+4096)
	if err := r.ParseMultipartForm(max + 4096); err != nil {
		return sqlc.File{}, "", ErrBadUpload
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		return sqlc.File{}, "", ErrBadUpload
	}
	defer file.Close()
	meta, err := s.files.Save(file, hdr.Filename)
	if err != nil {
		return sqlc.File{}, "", err
	}
	var width, height pgtype.Int4
	if meta.Width > 0 {
		width = pgtype.Int4{Int32: int32(meta.Width), Valid: true}
	}
	if meta.Height > 0 {
		height = pgtype.Int4{Int32: int32(meta.Height), Valid: true}
	}
	row, err := s.store.Queries.CreateFile(r.Context(), sqlc.CreateFileParams{
		
		ClubID: s.currentClubID(r),Bucket:       meta.Bucket,
		Path:         meta.RelPath,
		OriginalName: meta.OriginalName,
		Mime:         meta.MIME,
		Size:         meta.Size,
		Width:        width,
		Height:       height,
		Checksum:     meta.Checksum,
		UploadedBy:   actorID,
	})
	if err != nil {
		return sqlc.File{}, "", err
	}
	return row, filePublicURL(row.ID), nil
}

var ErrBadUpload = errors.New("bad upload")

func mapUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, files.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "Файл слишком большой — максимум 5 МБ")
	case errors.Is(err, files.ErrUnsupported):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported", "Допустимы JPEG, PNG, WebP")
	case errors.Is(err, files.ErrEmpty), errors.Is(err, ErrBadUpload):
		writeError(w, http.StatusBadRequest, "validation", "Некорректный файл")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить файл")
	}
}

func fileFromRow(row sqlc.File) fileJSON {
	out := fileJSON{
		ID:           uuidString(row.ID),
		URL:          filePublicURL(row.ID),
		Bucket:       row.Bucket,
		Path:         row.Path,
		OriginalName: row.OriginalName,
		MIME:         row.Mime,
		Size:         row.Size,
		Checksum:     files.ChecksumHex(row.Checksum),
		CreatedAt:    formatTS(row.CreatedAt),
	}
	if row.Width.Valid {
		v := row.Width.Int32
		out.Width = &v
	}
	if row.Height.Valid {
		v := row.Height.Int32
		out.Height = &v
	}
	return out
}

func filePublicURL(id pgtype.UUID) string {
	return "/api/files/" + uuidString(id)
}
