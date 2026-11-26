package httpapi

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestFilePublicURL(t *testing.T) {
	t.Parallel()
	id := pgtype.UUID{Bytes: [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}, Valid: true}
	got := filePublicURL(id)
	want := "/api/files/" + uuidString(id)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got == "/api/files/" {
		t.Fatal("empty uuid path")
	}
}

func TestCoverURLPointer(t *testing.T) {
	t.Parallel()
	if textPtr(pgtype.Text{}) != nil {
		t.Fatal("invalid text should yield nil pointer")
	}
	p := textPtr(pgtype.Text{String: "/api/files/abc", Valid: true})
	if p == nil || *p != "/api/files/abc" {
		t.Fatalf("got %#v", p)
	}
}
