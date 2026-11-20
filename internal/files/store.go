package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/crypto"
)

var (
	ErrTooLarge     = errors.New("file too large")
	ErrUnsupported  = errors.New("unsupported image type")
	ErrEmpty        = errors.New("empty file")
	ErrInvalidPath  = errors.New("invalid storage path")
)

// AllowedMIME is the upload allow-list.
var AllowedMIME = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Meta describes a validated upload ready to persist.
type Meta struct {
	Bucket       string
	RelPath      string
	OriginalName string
	MIME         string
	Size         int64
	Width        int
	Height       int
	Checksum     []byte
	AbsPath      string
}

// DiskStore writes uploads under root.
type DiskStore struct {
	Root     string
	MaxBytes int64
	Bucket   string
}

// NewDiskStore prepares the uploads directory.
func NewDiskStore(root string, maxBytes int64) (*DiskStore, error) {
	if maxBytes <= 0 {
		maxBytes = 5 << 20
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &DiskStore{Root: root, MaxBytes: maxBytes, Bucket: "uploads"}, nil
}

// Save validates and writes an image; returns metadata (file not yet in DB).
func (s *DiskStore) Save(r io.Reader, originalName string) (Meta, error) {
	limited := io.LimitReader(r, s.MaxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return Meta{}, err
	}
	if len(raw) == 0 {
		return Meta{}, ErrEmpty
	}
	if int64(len(raw)) > s.MaxBytes {
		return Meta{}, ErrTooLarge
	}

	mimeType := http.DetectContentType(raw)
	if mimeType == "application/octet-stream" || mimeType == "text/plain; charset=utf-8" {
		if ext := strings.ToLower(filepath.Ext(originalName)); ext != "" {
			if byExt := mime.TypeByExtension(ext); byExt != "" {
				mimeType = byExt
			}
		}
	}
	// normalize jpeg
	if mimeType == "image/jpg" {
		mimeType = "image/jpeg"
	}
	ext, ok := AllowedMIME[mimeType]
	if !ok {
		// webp: DetectContentType may not know it — sniff RIFF/WEBP
		if isWebP(raw) {
			mimeType = "image/webp"
			ext = ".webp"
			ok = true
		}
	}
	if !ok {
		return Meta{}, ErrUnsupported
	}

	width, height := 0, 0
	if mimeType != "image/webp" {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err == nil {
			width, height = cfg.Width, cfg.Height
		}
	}

	sum := sha256.Sum256(raw)
	now := time.Now().UTC()
	idBytes, err := crypto.RandomBytes(16)
	if err != nil {
		return Meta{}, err
	}
	rel := filepath.ToSlash(filepath.Join(
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
		hex.EncodeToString(idBytes)+ext,
	))
	abs := filepath.Join(s.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Meta{}, err
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return Meta{}, err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return Meta{}, err
	}

	return Meta{
		Bucket:       s.Bucket,
		RelPath:      rel,
		OriginalName: sanitizeName(originalName),
		MIME:         mimeType,
		Size:         int64(len(raw)),
		Width:        width,
		Height:       height,
		Checksum:     sum[:],
		AbsPath:      abs,
	}, nil
}

// Open resolves a relative path under the store root (no traversal).
func (s *DiskStore) Open(rel string) (*os.File, error) {
	clean := filepath.Clean("/" + strings.TrimPrefix(rel, "/"))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || strings.Contains(clean, "..") {
		return nil, ErrInvalidPath
	}
	abs := filepath.Join(s.Root, filepath.FromSlash(clean))
	relToRoot, err := filepath.Rel(s.Root, abs)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
		return nil, ErrInvalidPath
	}
	return os.Open(abs)
}

// ChecksumHex formats checksum for JSON.
func ChecksumHex(sum []byte) string {
	return hex.EncodeToString(sum)
}

func sanitizeName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\x00", "")
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}

func isWebP(b []byte) bool {
	return len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}
