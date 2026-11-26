package files

import (
	"bytes"
	"crypto/hmac"
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
	ErrTooLarge    = errors.New("file too large")
	ErrUnsupported = errors.New("unsupported image type")
	ErrEmpty       = errors.New("empty file")
	ErrInvalidPath = errors.New("invalid storage path")
	ErrNoKey       = errors.New("uploads encryption key is required")
)

// encMagic marks files encrypted at rest. Legacy plaintext files have no
// prefix and are still served as-is.
var encMagic = []byte("PLYENC1\x00")

// ReadSeekCloser is what Open returns: an image ready for http.ServeContent.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type memFile struct {
	*bytes.Reader
}

func (memFile) Close() error { return nil }


// AllowedMIME is the upload allow-list (cover and gallery: jpeg/png/webp only).
var AllowedMIME = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
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

// DiskStore writes uploads under root, encrypted with AES-256-GCM.
type DiskStore struct {
	Root     string
	MaxBytes int64
	Bucket   string
	key      []byte
}

// NewDiskStore prepares the uploads directory and derives a dedicated
// encryption key from the application master key.
func NewDiskStore(root string, maxBytes int64, masterKey []byte) (*DiskStore, error) {
	if len(masterKey) != 32 {
		return nil, ErrNoKey
	}
	if maxBytes <= 0 {
		maxBytes = 5 << 20
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &DiskStore{Root: root, MaxBytes: maxBytes, Bucket: "uploads", key: deriveUploadKey(masterKey)}, nil
}

func deriveUploadKey(master []byte) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("pairly/uploads/v1"))
	return mac.Sum(nil)
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
	blob, err := crypto.EncryptAESGCM(s.key, raw)
	if err != nil {
		return Meta{}, err
	}
	payload := make([]byte, 0, len(encMagic)+len(blob))
	payload = append(payload, encMagic...)
	payload = append(payload, blob...)

	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
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
func (s *DiskStore) Open(rel string) (ReadSeekCloser, error) {
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
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, encMagic) {
		plain, err := crypto.DecryptAESGCM(s.key, data[len(encMagic):])
		if err != nil {
			return nil, fmt.Errorf("decrypt upload: %w", err)
		}
		data = plain
	}
	return memFile{bytes.NewReader(data)}, nil
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
