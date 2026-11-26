package uuidx_test

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/uuidx"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestStringCanonical(t *testing.T) {
	id, err := uuidx.Parse("A1B2C3D4-E5F6-4789-A012-3456789ABCDE")
	if err != nil {
		t.Fatal(err)
	}
	got := uuidx.String(id)
	want := "a1b2c3d4-e5f6-4789-a012-3456789abcde"
	if got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
	if uuidx.String(pgtype.UUID{}) != "" {
		t.Fatal("invalid UUID must stringify to empty")
	}
}

func TestParseTrimCase(t *testing.T) {
	id, err := uuidx.Parse("  A1B2C3D4-E5F6-4789-A012-3456789ABCDE  ")
	if err != nil {
		t.Fatal(err)
	}
	if !id.Valid {
		t.Fatal("expected valid")
	}
	if _, err := uuidx.Parse(""); err == nil {
		t.Fatal("empty must error")
	}
	if _, err := uuidx.Parse("not-a-uuid"); err == nil {
		t.Fatal("garbage must error")
	}
	if uuidx.ParseOrZero("").Valid {
		t.Fatal("ParseOrZero empty must be invalid")
	}
}

func TestEqual(t *testing.T) {
	a, _ := uuidx.Parse("a1b2c3d4-e5f6-4789-a012-3456789abcde")
	b, _ := uuidx.Parse("A1B2C3D4-E5F6-4789-A012-3456789ABCDE")
	c, _ := uuidx.Parse("00000000-0000-4000-8000-000000000001")
	if !uuidx.Equal(a, b) {
		t.Fatal("same bytes must Equal")
	}
	if uuidx.Equal(a, c) {
		t.Fatal("different must not Equal")
	}
	if uuidx.Equal(a, pgtype.UUID{}) || uuidx.Equal(pgtype.UUID{}, pgtype.UUID{}) {
		t.Fatal("invalid must not Equal")
	}
	if !uuidx.EqualString(
		"  A1B2C3D4-E5F6-4789-A012-3456789ABCDE ",
		"a1b2c3d4-e5f6-4789-a012-3456789abcde",
	) {
		t.Fatal("EqualString must normalize")
	}
	if uuidx.EqualString("", "") {
		t.Fatal("empty strings must not equal")
	}
}

func TestNewV4(t *testing.T) {
	id, err := uuidx.New()
	if err != nil {
		t.Fatal(err)
	}
	s := uuidx.String(id)
	if len(s) != 36 {
		t.Fatalf("len=%d s=%q", len(s), s)
	}
	// version nibble '4'
	if s[14] != '4' {
		t.Fatalf("version want 4, got %q in %s", s[14], s)
	}
	// variant 8/9/a/b
	switch s[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("variant nibble want 8|9|a|b, got %q in %s", s[19], s)
	}
}

func TestTextArrayForSQLBind(t *testing.T) {
	a, _ := uuidx.Parse("a1b2c3d4-e5f6-4789-a012-3456789abcde")
	b, _ := uuidx.Parse("A1B2C3D4-E5F6-4789-A012-3456789ABCDE") // dup
	c, _ := uuidx.Parse("00000000-0000-4000-8000-000000000002")
	got := uuidx.TextArray([]pgtype.UUID{a, pgtype.UUID{}, b, c})
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0] != "a1b2c3d4-e5f6-4789-a012-3456789abcde" {
		t.Fatalf("first=%q", got[0])
	}
	if got[1] != "00000000-0000-4000-8000-000000000002" {
		t.Fatalf("second=%q", got[1])
	}
}
