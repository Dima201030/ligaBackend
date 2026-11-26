package files

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"os"
	"testing"
)

func TestSaveAndOpenPNG(t *testing.T) {
	root := t.TempDir()
	store, err := NewDiskStore(root, 1<<20, testKey())
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	meta, err := store.Save(&buf, "cover.PNG")
	if err != nil {
		t.Fatal(err)
	}
	if meta.MIME != "image/png" {
		t.Fatalf("mime=%s", meta.MIME)
	}
	if meta.Width != 8 || meta.Height != 6 {
		t.Fatalf("size=%dx%d", meta.Width, meta.Height)
	}
	if _, err := os.Stat(meta.AbsPath); err != nil {
		t.Fatal(err)
	}

	f, err := store.Open(meta.RelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := store.Open("/etc/passwd"); err == nil {
		t.Fatal("expected absolute path rejection")
	}
	if _, err := store.Open(".."); err == nil {
		t.Fatal("expected parent dir rejection")
	}
}

func TestRejectTooLarge(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), 32, testKey())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Save(bytes.NewReader(bytes.Repeat([]byte("x"), 64)), "x.bin")
	if err != ErrTooLarge {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectUnsupported(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), 1<<20, testKey())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Save(bytes.NewReader([]byte("%PDF-1.4 fake")), "doc.pdf")
	if err != ErrUnsupported {
		t.Fatalf("err=%v", err)
	}
}

func testKey() []byte {
	return bytes.Repeat([]byte("k"), 32)
}

func TestUploadsAreEncryptedAtRest(t *testing.T) {
	root := t.TempDir()
	store, err := NewDiskStore(root, 1<<20, testKey())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	plain := buf.Bytes()
	meta, err := store.Save(bytes.NewReader(plain), "x.png")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(meta.AbsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, encMagic) || bytes.Contains(raw, plain) {
		t.Fatal("stored file is not encrypted")
	}
	f, err := store.Open(meta.RelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("decrypted file differs")
	}
}
