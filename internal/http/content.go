package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/content"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/tournaments"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type newsJSON struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Excerpt      string   `json:"excerpt"`
	Body         []string `json:"body"`
	CoverURL     *string  `json:"cover_url,omitempty"`
	Tag          string   `json:"tag"`
	Author       string   `json:"author"`
	Status       string   `json:"status"`
	PublishedAt  *string  `json:"published_at,omitempty"`
	ReadingTime  int32    `json:"reading_time"`
	CreatedAt    string   `json:"created_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

type eventJSON struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Title       string  `json:"title"`
	Kind        string  `json:"kind"`
	Description string  `json:"description"`
	StartsAt    string  `json:"starts_at"`
	Location    string  `json:"location"`
	CoverURL    *string `json:"cover_url,omitempty"`
	SeatsLeft   *int32  `json:"seats_left,omitempty"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at,omitempty"`
	UpdatedAt   string  `json:"updated_at,omitempty"`
}

type pageJSON struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	BodyMarkdown string `json:"body_markdown"`
	UpdatedAt    string `json:"updated_at"`
}

const (
	contentTitleMin      = 3
	contentTitleMax      = 120
	newsExcerptMax       = 500
	newsBodyTotalMax     = 20000
	eventDescriptionMax  = 4000
	eventLocationMax     = 200
	pageTitleMin         = 2
	pageTitleMax         = 120
	pageBodyMarkdownMax  = 50000
	contentStartMaxAhead = 6 // months
	eventSeatsMax        = 500
)

type newsWriteRequest struct {
	Title       string   `json:"title"`
	Slug        string   `json:"slug"`
	Excerpt     string   `json:"excerpt"`
	Body        []string `json:"body"`
	CoverURL    *string  `json:"cover_url"`
	Tag         string   `json:"tag"`
	AuthorName  string   `json:"author_name"`
	Status      string   `json:"status"`
	PublishedAt *string  `json:"published_at"`
}

type eventWriteRequest struct {
	Title       string  `json:"title"`
	Slug        string  `json:"slug"`
	Kind        string  `json:"kind"`
	Description string  `json:"description"`
	StartsAt    string  `json:"starts_at"`
	Location    string  `json:"location"`
	CoverURL    *string `json:"cover_url"`
	SeatsLeft   *int32  `json:"seats_left"`
	Status      string  `json:"status"`
}

type pageWriteRequest struct {
	Title        string `json:"title"`
	BodyMarkdown string `json:"body_markdown"`
}

func (s *Server) handleListPublicNews(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	limit := queryInt(r, "limit", 20, 1, 100)
	offset := queryInt(r, "offset", 0, 0, 100000)
	rows, err := s.store.Queries.ListPublishedNews(r.Context(), sqlc.ListPublishedNewsParams{
		
		ClubID: s.currentClubID(r),Tag:         optionalText(tag),
		LimitCount:  int32(limit),
		OffsetCount: int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить новости")
		return
	}
	out := make([]newsJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, newsFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"news": out})
}

func (s *Server) handleGetPublicNews(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	row, err := s.store.Queries.GetNewsPostBySlug(r.Context(), sqlc.GetNewsPostBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Новость не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить новость")
		return
	}
	if row.Status != sqlc.ContentStatusPublished {
		writeError(w, http.StatusNotFound, "not_found", "Новость не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"news": newsFromRow(row)})
}

func (s *Server) handleListOrganizerNews(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := queryInt(r, "limit", 50, 1, 200)
	offset := queryInt(r, "offset", 0, 0, 100000)
	rows, err := s.store.Queries.ListOrganizerNews(r.Context(), sqlc.ListOrganizerNewsParams{
		
		ClubID: s.currentClubID(r),Status:      optionalText(status),
		LimitCount:  int32(limit),
		OffsetCount: int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить новости")
		return
	}
	out := make([]newsJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, newsFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"news": out})
}

func (s *Server) handleCreateNews(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	var req newsWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	params, err := s.buildNewsCreate(r, actor, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	row, err := s.store.Queries.CreateNewsPost(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "slug_taken", "Slug уже занят")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать новость")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"news": newsFromRow(row)})
}

func (s *Server) handleUpdateNews(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	existing, err := s.loadClubNews(r, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Новость не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить новость")
		return
	}
	var req newsWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	params, err := buildNewsUpdate(existing, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	row, err := s.store.Queries.UpdateNewsPost(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить новость")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"news": newsFromRow(row)})
}

func (s *Server) handleListPublicEvents(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	limit := queryInt(r, "limit", 50, 1, 200)
	offset := queryInt(r, "offset", 0, 0, 100000)
	rows, err := s.store.Queries.ListPublishedEvents(r.Context(), sqlc.ListPublishedEventsParams{
		
		ClubID: s.currentClubID(r),Kind:        optionalText(kind),
		LimitCount:  int32(limit),
		OffsetCount: int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить события")
		return
	}
	out := make([]eventJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, eventFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) handleGetPublicEvent(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	row, err := s.store.Queries.GetClubEventBySlug(r.Context(), sqlc.GetClubEventBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Событие не найдено")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить событие")
		return
	}
	if row.Status != sqlc.ContentStatusPublished {
		writeError(w, http.StatusNotFound, "not_found", "Событие не найдено")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event": eventFromRow(row)})
}

func (s *Server) handleListOrganizerEvents(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := queryInt(r, "limit", 50, 1, 200)
	offset := queryInt(r, "offset", 0, 0, 100000)
	rows, err := s.store.Queries.ListOrganizerEvents(r.Context(), sqlc.ListOrganizerEventsParams{
		
		ClubID: s.currentClubID(r),Status:      optionalText(status),
		LimitCount:  int32(limit),
		OffsetCount: int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить события")
		return
	}
	out := make([]eventJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, eventFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	var req eventWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	params, err := s.buildEventCreate(r, actor, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	row, err := s.store.Queries.CreateClubEvent(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "slug_taken", "Slug уже занят")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать событие")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": eventFromRow(row)})
}

func (s *Server) handleUpdateEvent(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	existing, err := s.loadClubEvent(r, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Событие не найдено")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить событие")
		return
	}
	var req eventWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	params, err := buildEventUpdate(existing, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	row, err := s.store.Queries.UpdateClubEvent(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить событие")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event": eventFromRow(row)})
}

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Queries.ListPages(r.Context(), s.currentClubID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить страницы")
		return
	}
	out := make([]pageJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, pageFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": out})
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	row, err := s.store.Queries.GetPageBySlug(r.Context(), sqlc.GetPageBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Страница не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить страницу")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": pageFromRow(row)})
}

func (s *Server) handleUpsertPage(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if !validPageSlug(slug) {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный slug страницы")
		return
	}
	var req pageWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) < pageTitleMin {
		writeError(w, http.StatusBadRequest, "validation", "Заголовок слишком короткий")
		return
	}
	if utf8.RuneCountInString(title) > pageTitleMax {
		writeError(w, http.StatusBadRequest, "validation", "Заголовок слишком длинный — максимум 120 символов")
		return
	}
	bodyMarkdown := strings.TrimSpace(req.BodyMarkdown)
	if utf8.RuneCountInString(bodyMarkdown) > pageBodyMarkdownMax {
		writeError(w, http.StatusBadRequest, "validation", "Текст страницы слишком длинный — максимум 50000 символов")
		return
	}
	row, err := s.store.Queries.UpsertPage(r.Context(), sqlc.UpsertPageParams{
		
		ClubID: s.currentClubID(r),Slug:         slug,
		Title:        title,
		BodyMarkdown: bodyMarkdown,
		UpdatedBy:    actor.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить страницу")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": pageFromRow(row)})
}

func (s *Server) buildNewsCreate(r *http.Request, actor *auth.SessionUser, req newsWriteRequest) (sqlc.CreateNewsPostParams, error) {
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) < contentTitleMin {
		return sqlc.CreateNewsPostParams{
		ClubID: s.currentClubID(r),}, errors.New("Заголовок слишком короткий")
	}
	if utf8.RuneCountInString(title) > contentTitleMax {
		return sqlc.CreateNewsPostParams{
		ClubID: s.currentClubID(r),}, errors.New("Заголовок слишком длинный — максимум 120 символов")
	}
	excerpt := strings.TrimSpace(req.Excerpt)
	if utf8.RuneCountInString(excerpt) > newsExcerptMax {
		return sqlc.CreateNewsPostParams{
		ClubID: s.currentClubID(r),}, errors.New("Лид слишком длинный — максимум 500 символов")
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = tournaments.Slugify(title)
	}
	slug = tournaments.Slugify(slug)
	status, err := parseContentStatus(req.Status, sqlc.ContentStatusDraft)
	if err != nil {
		return sqlc.CreateNewsPostParams{
		ClubID: s.currentClubID(r),}, err
	}
	body := normalizeBody(req.Body)
	if err := validateNewsBody(body); err != nil {
		return sqlc.CreateNewsPostParams{
		ClubID: s.currentClubID(r),}, err
	}
	bodyJSON, _ := json.Marshal(body)
	author := strings.TrimSpace(req.AuthorName)
	if author == "" {
		author = strings.TrimSpace(actor.FirstName + " " + actor.LastName)
	}
	publishedAt, err := resolvePublishedAt(status, req.PublishedAt, pgtype.Timestamptz{})
	if err != nil {
		return sqlc.CreateNewsPostParams{
		ClubID: s.currentClubID(r),}, err
	}
	cover := optionalTextPtr(req.CoverURL)
	return sqlc.CreateNewsPostParams{
		
		ClubID: s.currentClubID(r),Slug:        slug,
		Title:       title,
		Excerpt:     excerpt,
		BodyJson:    bodyJSON,
		CoverUrl:    cover,
		Tag:         strings.TrimSpace(req.Tag),
		AuthorName:  author,
		AuthorID:    actor.ID,
		Status:      status,
		PublishedAt: publishedAt,
		ReadingTime: int32(content.ReadingTimeMinutes(body)),
	}, nil
}

func buildNewsUpdate(existing sqlc.NewsPost, req newsWriteRequest) (sqlc.UpdateNewsPostParams, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = existing.Title
	}
	if utf8.RuneCountInString(title) < contentTitleMin {
		return sqlc.UpdateNewsPostParams{}, errors.New("Заголовок слишком короткий")
	}
	if utf8.RuneCountInString(title) > contentTitleMax {
		return sqlc.UpdateNewsPostParams{}, errors.New("Заголовок слишком длинный — максимум 120 символов")
	}
	status := existing.Status
	if strings.TrimSpace(req.Status) != "" {
		parsed, err := parseContentStatus(req.Status, existing.Status)
		if err != nil {
			return sqlc.UpdateNewsPostParams{}, err
		}
		status = parsed
	}
	body := existingBody(existing.BodyJson)
	if req.Body != nil {
		body = normalizeBody(req.Body)
		if err := validateNewsBody(body); err != nil {
			return sqlc.UpdateNewsPostParams{}, err
		}
	}
	bodyJSON, _ := json.Marshal(body)
	excerpt := existing.Excerpt
	if req.Excerpt != "" || req.Body != nil {
		excerpt = strings.TrimSpace(req.Excerpt)
		if utf8.RuneCountInString(excerpt) > newsExcerptMax {
			return sqlc.UpdateNewsPostParams{}, errors.New("Лид слишком длинный — максимум 500 символов")
		}
	}
	tag := existing.Tag
	if req.Tag != "" {
		tag = strings.TrimSpace(req.Tag)
	}
	author := existing.AuthorName
	if strings.TrimSpace(req.AuthorName) != "" {
		author = strings.TrimSpace(req.AuthorName)
	}
	cover := existing.CoverUrl
	if req.CoverURL != nil {
		cover = optionalTextPtr(req.CoverURL)
	}
	publishedAt, err := resolvePublishedAt(status, req.PublishedAt, existing.PublishedAt)
	if err != nil {
		return sqlc.UpdateNewsPostParams{}, err
	}
	return sqlc.UpdateNewsPostParams{
		ID:          existing.ID,
		Title:       title,
		Excerpt:     excerpt,
		BodyJson:    bodyJSON,
		CoverUrl:    cover,
		Tag:         tag,
		AuthorName:  author,
		Status:      status,
		PublishedAt: publishedAt,
		ReadingTime: int32(content.ReadingTimeMinutes(body)),
	}, nil
}

func (s *Server) buildEventCreate(r *http.Request, actor *auth.SessionUser, req eventWriteRequest) (sqlc.CreateClubEventParams, error) {
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) < contentTitleMin {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("Заголовок слишком короткий")
	}
	if utf8.RuneCountInString(title) > contentTitleMax {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("Заголовок слишком длинный — максимум 120 символов")
	}
	description := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(description) > eventDescriptionMax {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("Описание слишком длинное — максимум 4000 символов")
	}
	location := strings.TrimSpace(req.Location)
	if utf8.RuneCountInString(location) > eventLocationMax {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("Место слишком длинное — максимум 200 символов")
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = tournaments.Slugify(title)
	}
	slug = tournaments.Slugify(slug)
	kind, err := parseEventKind(req.Kind)
	if err != nil {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, err
	}
	status, err := parseContentStatus(req.Status, sqlc.ContentStatusDraft)
	if err != nil {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, err
	}
	starts, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartsAt))
	if err != nil {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("Некорректная дата starts_at")
	}
	if starts.UTC().After(time.Now().UTC().AddDate(0, contentStartMaxAhead, 0)) {
		return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("Дата события не может быть больше чем через 6 месяцев")
	}
	var seats pgtype.Int4
	if req.SeatsLeft != nil {
		if *req.SeatsLeft < 0 {
			return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("seats_left не может быть отрицательным")
		}
		if *req.SeatsLeft > eventSeatsMax {
			return sqlc.CreateClubEventParams{
		ClubID: s.currentClubID(r),}, errors.New("Число мест слишком большое — максимум 500")
		}
		seats = pgtype.Int4{Int32: *req.SeatsLeft, Valid: true}
	}
	return sqlc.CreateClubEventParams{
		
		ClubID: s.currentClubID(r),Slug:        slug,
		Title:       title,
		Kind:        kind,
		Description: description,
		StartsAt:    pgtype.Timestamptz{Time: starts.UTC(), Valid: true},
		Location:    location,
		CoverUrl:    optionalTextPtr(req.CoverURL),
		SeatsLeft:   seats,
		Status:      status,
		CreatedBy:   actor.ID,
	}, nil
}

func buildEventUpdate(existing sqlc.ClubEvent, req eventWriteRequest) (sqlc.UpdateClubEventParams, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = existing.Title
	}
	if utf8.RuneCountInString(title) < contentTitleMin {
		return sqlc.UpdateClubEventParams{}, errors.New("Заголовок слишком короткий")
	}
	if utf8.RuneCountInString(title) > contentTitleMax {
		return sqlc.UpdateClubEventParams{}, errors.New("Заголовок слишком длинный — максимум 120 символов")
	}
	kind := existing.Kind
	if strings.TrimSpace(req.Kind) != "" {
		parsed, err := parseEventKind(req.Kind)
		if err != nil {
			return sqlc.UpdateClubEventParams{}, err
		}
		kind = parsed
	}
	status := existing.Status
	if strings.TrimSpace(req.Status) != "" {
		parsed, err := parseContentStatus(req.Status, existing.Status)
		if err != nil {
			return sqlc.UpdateClubEventParams{}, err
		}
		status = parsed
	}
	starts := existing.StartsAt
	if strings.TrimSpace(req.StartsAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartsAt))
		if err != nil {
			return sqlc.UpdateClubEventParams{}, errors.New("Некорректная дата starts_at")
		}
		if t.UTC().After(time.Now().UTC().AddDate(0, contentStartMaxAhead, 0)) {
			return sqlc.UpdateClubEventParams{}, errors.New("Дата события не может быть больше чем через 6 месяцев")
		}
		starts = pgtype.Timestamptz{Time: t.UTC(), Valid: true}
	}
	desc := existing.Description
	if req.Description != "" {
		desc = strings.TrimSpace(req.Description)
		if utf8.RuneCountInString(desc) > eventDescriptionMax {
			return sqlc.UpdateClubEventParams{}, errors.New("Описание слишком длинное — максимум 4000 символов")
		}
	}
	loc := existing.Location
	if req.Location != "" {
		loc = strings.TrimSpace(req.Location)
		if utf8.RuneCountInString(loc) > eventLocationMax {
			return sqlc.UpdateClubEventParams{}, errors.New("Место слишком длинное — максимум 200 символов")
		}
	}
	cover := existing.CoverUrl
	if req.CoverURL != nil {
		cover = optionalTextPtr(req.CoverURL)
	}
	seats := existing.SeatsLeft
	if req.SeatsLeft != nil {
		if *req.SeatsLeft < 0 {
			return sqlc.UpdateClubEventParams{}, errors.New("seats_left не может быть отрицательным")
		}
		if *req.SeatsLeft > eventSeatsMax {
			return sqlc.UpdateClubEventParams{}, errors.New("Число мест слишком большое — максимум 500")
		}
		seats = pgtype.Int4{Int32: *req.SeatsLeft, Valid: true}
	}
	return sqlc.UpdateClubEventParams{
		ID:          existing.ID,
		Title:       title,
		Kind:        kind,
		Description: desc,
		StartsAt:    starts,
		Location:    loc,
		CoverUrl:    cover,
		SeatsLeft:   seats,
		Status:      status,
	}, nil
}

func newsFromRow(row sqlc.NewsPost) newsJSON {
	out := newsJSON{
		ID:          uuidString(row.ID),
		Slug:        row.Slug,
		Title:       row.Title,
		Excerpt:     row.Excerpt,
		Body:        existingBody(row.BodyJson),
		Tag:         row.Tag,
		Author:      row.AuthorName,
		Status:      string(row.Status),
		ReadingTime: row.ReadingTime,
		CreatedAt:   formatTS(row.CreatedAt),
		UpdatedAt:   formatTS(row.UpdatedAt),
		CoverURL:    textPtr(row.CoverUrl),
	}
	if row.PublishedAt.Valid {
		ts := row.PublishedAt.Time.UTC().Format(time.RFC3339)
		out.PublishedAt = &ts
	}
	return out
}

func eventFromRow(row sqlc.ClubEvent) eventJSON {
	out := eventJSON{
		ID:          uuidString(row.ID),
		Slug:        row.Slug,
		Title:       row.Title,
		Kind:        string(row.Kind),
		Description: row.Description,
		StartsAt:    formatTS(row.StartsAt),
		Location:    row.Location,
		Status:      string(row.Status),
		CreatedAt:   formatTS(row.CreatedAt),
		UpdatedAt:   formatTS(row.UpdatedAt),
		CoverURL:    textPtr(row.CoverUrl),
	}
	if row.SeatsLeft.Valid {
		v := row.SeatsLeft.Int32
		out.SeatsLeft = &v
	}
	return out
}

func pageFromRow(row sqlc.Page) pageJSON {
	return pageJSON{
		Slug:         row.Slug,
		Title:        row.Title,
		BodyMarkdown: row.BodyMarkdown,
		UpdatedAt:    formatTS(row.UpdatedAt),
	}
}

func parseContentStatus(raw string, fallback sqlc.ContentStatus) (sqlc.ContentStatus, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	switch sqlc.ContentStatus(s) {
	case sqlc.ContentStatusDraft, sqlc.ContentStatusPublished, sqlc.ContentStatusArchived:
		return sqlc.ContentStatus(s), nil
	default:
		return "", errors.New("Некорректный status")
	}
}

func parseEventKind(raw string) (sqlc.ClubEventKind, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return sqlc.ClubEventKindClub, nil
	}
	switch sqlc.ClubEventKind(s) {
	case sqlc.ClubEventKindTournament, sqlc.ClubEventKindCamp, sqlc.ClubEventKindSchool, sqlc.ClubEventKindClub:
		return sqlc.ClubEventKind(s), nil
	default:
		return "", errors.New("Некорректный kind")
	}
}

func normalizeBody(body []string) []string {
	out := make([]string, 0, len(body))
	for _, p := range body {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validateNewsBody(body []string) error {
	total := 0
	for _, part := range body {
		total += utf8.RuneCountInString(part)
		if total > newsBodyTotalMax {
			return errors.New("Текст новости слишком длинный — максимум 20000 символов")
		}
	}
	return nil
}

func existingBody(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return []string{}
	}
	return out
}

func resolvePublishedAt(status sqlc.ContentStatus, raw *string, current pgtype.Timestamptz) (pgtype.Timestamptz, error) {
	if raw != nil && strings.TrimSpace(*raw) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
		if err != nil {
			return pgtype.Timestamptz{}, errors.New("Некорректная дата published_at")
		}
		return pgtype.Timestamptz{Time: t.UTC(), Valid: true}, nil
	}
	switch status {
	case sqlc.ContentStatusPublished:
		if current.Valid {
			return current, nil
		}
		return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}, nil
	case sqlc.ContentStatusDraft, sqlc.ContentStatusArchived:
		return pgtype.Timestamptz{}, nil
	default:
		return current, nil
	}
}

func validPageSlug(slug string) bool {
	if slug == "" || len(slug) > 64 {
		return false
	}
	for i, r := range slug {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r == '-' && i > 0)
		if !ok {
			return false
		}
	}
	return !strings.HasSuffix(slug, "-")
}
