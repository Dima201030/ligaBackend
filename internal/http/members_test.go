package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestParseClubMemberRole(t *testing.T) {
	if role, ok := parseClubMemberRole("Owner"); !ok || role != sqlc.ClubMemberRoleOwner {
		t.Fatalf("owner: %s %v", role, ok)
	}
	if _, ok := parseClubMemberRole("admin"); ok {
		t.Fatal("admin must fail")
	}
	if clubRoleRank(sqlc.ClubMemberRoleOwner) <= clubRoleRank(sqlc.ClubMemberRoleOrganizer) {
		t.Fatal("owner must outrank organizer")
	}
}

func TestClubMembersInviteListAndPatch(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	store, err := db.Connect(ctx, url)
	if err != nil {
		t.Skipf("db unavailable: %v", err)
	}
	t.Cleanup(store.Close)

	suffix := time.Now().UnixNano()
	clubSlug := "mem-" + itoaMem(suffix)
	club, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug:     clubSlug,
		Name:     "Members Club",
		City:     "Test",
		Status:   sqlc.ClubStatusActive,
		Settings: []byte(`{"timezone":"Europe/Moscow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM club_memberships WHERE club_id=$1`, club.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM clubs WHERE id=$1`, club.ID)
	})

	owner := createMemberUser(t, store, "owner-"+itoaMem(suffix)+"@example.com", sqlc.UserRoleOrganizer)
	player := createMemberUser(t, store, "player-"+itoaMem(suffix)+"@example.com", sqlc.UserRolePlayer)
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1 OR user_id=$2`, owner.ID, player.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1 OR id=$2`, owner.ID, player.ID)
	})

	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: club.ID, UserID: owner.ID, Role: sqlc.ClubMemberRoleOwner, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	srv := NewWithOptions(Options{Store: store, Cfg: config.Config{AppEnv: "test", DefaultClubSlug: clubSlug}})

	ownerEmail := emailString(owner.Email)
	playerEmail := emailString(player.Email)

	// list as club owner
	listReq := httptest.NewRequest(http.MethodGet, "/api/organizer/members", nil)
	listReq.Header.Set("X-Club-Slug", clubSlug)
	listReq = listReq.WithContext(auth.SetUser(listReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: ownerEmail, Role: owner.Role, Status: owner.Status,
	}))
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list=%d body=%s", listRec.Code, listRec.Body.String())
	}

	// invite player as organizer role
	body, _ := json.Marshal(map[string]string{"email": playerEmail, "role": "organizer", "status": "invited"})
	addReq := httptest.NewRequest(http.MethodPost, "/api/organizer/members", bytes.NewReader(body))
	addReq.Header.Set("Content-Type", "application/json")
	addReq.Header.Set("X-Club-Slug", clubSlug)
	addReq = addReq.WithContext(auth.SetUser(addReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: ownerEmail, Role: owner.Role, Status: owner.Status,
	}))
	addRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add=%d body=%s", addRec.Code, addRec.Body.String())
	}
	var added struct {
		Member map[string]any `json:"member"`
	}
	if err := json.Unmarshal(addRec.Body.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	if added.Member["status"] != "invited" || added.Member["role"] != "organizer" {
		t.Fatalf("member=%v", added.Member)
	}
	memberID, _ := added.Member["id"].(string)

	// promote to active player via PATCH
	patchBody, _ := json.Marshal(map[string]string{"role": "player", "status": "active"})
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/organizer/members/"+memberID, bytes.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("X-Club-Slug", clubSlug)
	patchReq = patchReq.WithContext(auth.SetUser(patchReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: ownerEmail, Role: owner.Role, Status: owner.Status,
	}))
	patchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch=%d body=%s", patchRec.Code, patchRec.Body.String())
	}

	// organizer-level actor cannot assign owner
	orgActor := createMemberUser(t, store, "orgact-"+itoaMem(suffix)+"@example.com", sqlc.UserRoleOrganizer)
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1`, orgActor.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, orgActor.ID)
	})
	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: club.ID, UserID: orgActor.ID, Role: sqlc.ClubMemberRoleOrganizer, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	forbidBody, _ := json.Marshal(map[string]string{"email": playerEmail, "role": "owner"})
	forbidReq := httptest.NewRequest(http.MethodPost, "/api/organizer/members", bytes.NewReader(forbidBody))
	forbidReq.Header.Set("Content-Type", "application/json")
	forbidReq.Header.Set("X-Club-Slug", clubSlug)
	forbidReq = forbidReq.WithContext(auth.SetUser(forbidReq.Context(), &auth.SessionUser{
		ID: orgActor.ID, Email: emailString(orgActor.Email), Role: orgActor.Role, Status: orgActor.Status,
	}))
	forbidRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(forbidRec, forbidReq)
	if forbidRec.Code != http.StatusForbidden {
		t.Fatalf("owner assign want 403, got %d %s", forbidRec.Code, forbidRec.Body.String())
	}

	// DELETE active player → left
	delReq := httptest.NewRequest(http.MethodDelete, "/api/organizer/members/"+memberID, nil)
	delReq.Header.Set("X-Club-Slug", clubSlug)
	delReq = delReq.WithContext(auth.SetUser(delReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: ownerEmail, Role: owner.Role, Status: owner.Status,
	}))
	delRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete player=%d body=%s", delRec.Code, delRec.Body.String())
	}
	var deleted struct {
		Member map[string]any `json:"member"`
	}
	if err := json.Unmarshal(delRec.Body.Bytes(), &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted.Member["status"] != "left" {
		t.Fatalf("want left, got %v", deleted.Member["status"])
	}

	// organizer cannot DELETE owner
	ownerRows, err := store.Queries.ListClubMembers(ctx, sqlc.ListClubMembersParams{
		ClubID: club.ID, Status: pgtype.Text{}, LimitCount: 20, OffsetCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	var ownerMembershipID string
	for _, row := range ownerRows {
		if uuidString(row.UserID) == uuidString(owner.ID) {
			ownerMembershipID = uuidString(row.ID)
			break
		}
	}
	if ownerMembershipID == "" {
		t.Fatal("owner membership missing")
	}
	orgDelReq := httptest.NewRequest(http.MethodDelete, "/api/organizer/members/"+ownerMembershipID, nil)
	orgDelReq.Header.Set("X-Club-Slug", clubSlug)
	orgDelReq = orgDelReq.WithContext(auth.SetUser(orgDelReq.Context(), &auth.SessionUser{
		ID: orgActor.ID, Email: emailString(orgActor.Email), Role: orgActor.Role, Status: orgActor.Status,
	}))
	orgDelRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(orgDelRec, orgDelReq)
	if orgDelRec.Code != http.StatusForbidden {
		t.Fatalf("organizer delete owner want 403, got %d %s", orgDelRec.Code, orgDelRec.Body.String())
	}

	// last active owner cannot be deleted
	lastDelReq := httptest.NewRequest(http.MethodDelete, "/api/organizer/members/"+ownerMembershipID, nil)
	lastDelReq.Header.Set("X-Club-Slug", clubSlug)
	lastDelReq = lastDelReq.WithContext(auth.SetUser(lastDelReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: ownerEmail, Role: owner.Role, Status: owner.Status,
	}))
	lastDelRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(lastDelRec, lastDelReq)
	if lastDelRec.Code != http.StatusConflict {
		t.Fatalf("last owner delete want 409, got %d %s", lastDelRec.Code, lastDelRec.Body.String())
	}

	// second owner can remove the first
	owner2 := createMemberUser(t, store, "owner2-"+itoaMem(suffix)+"@example.com", sqlc.UserRoleOrganizer)
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1`, owner2.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, owner2.ID)
	})
	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: club.ID, UserID: owner2.ID, Role: sqlc.ClubMemberRoleOwner, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	okDelReq := httptest.NewRequest(http.MethodDelete, "/api/organizer/members/"+ownerMembershipID, nil)
	okDelReq.Header.Set("X-Club-Slug", clubSlug)
	okDelReq = okDelReq.WithContext(auth.SetUser(okDelReq.Context(), &auth.SessionUser{
		ID: owner2.ID, Email: emailString(owner2.Email), Role: owner2.Role, Status: owner2.Status,
	}))
	okDelRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(okDelRec, okDelReq)
	if okDelRec.Code != http.StatusOK {
		t.Fatalf("delete owner with peer=%d body=%s", okDelRec.Code, okDelRec.Body.String())
	}
}

func createMemberUser(t *testing.T, store *db.Store, email string, role sqlc.UserRole) sqlc.User {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email: textEmail(email), PasswordHash: hash, FirstName: "Mem", LastName: "Ber",
		Role: role, Status: sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "challenger", "Mem", "Ber")); err != nil {
		t.Fatal(err)
	}
	return user
}

func itoaMem(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
