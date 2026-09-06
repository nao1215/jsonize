package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name string
	body string
	typ  byte
	link string
}

func buildArchive(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: typ, Linkname: e.link}
		if typ == tar.TypeDir {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type server struct {
	srv      *httptest.Server
	archive  []byte
	checksum string
	status   int
}

func newServer(t *testing.T, archive []byte, checksum string) *server {
	t.Helper()
	s := &server{archive: archive, checksum: checksum, status: http.StatusOK}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.tar.gz":
			w.WriteHeader(s.status)
			_, _ = w.Write(s.archive)
		case "/registry.tar.gz.sha256":
			_, _ = w.Write([]byte(s.checksum))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *server) opts(dest string) Options {
	return Options{URL: s.srv.URL + "/registry.tar.gz", Dest: dest, Client: s.srv.Client()}
}

var good = []entry{
	{name: "jsonize-registry/", typ: tar.TypeDir},
	{name: "jsonize-registry/registry.yaml", body: "format: 1\nname: official\n"},
	{name: "jsonize-registry/parsers/x/y/parser.yaml", body: "format: 1\ncommand: x\nvariant: y\nparse: {type: kv}\n"},
}

func TestUpdateInstallsAndReplaces(t *testing.T) {
	t.Parallel()
	archive := buildArchive(t, good)
	s := newServer(t, archive, digest(archive)+"  jsonize-registry.tar.gz\n")
	dest := filepath.Join(t.TempDir(), "cache", "official")
	validated := ""
	o := s.opts(dest)
	o.Validate = func(dir string) error { validated = dir; return nil }
	rep, err := Update(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 2 || rep.Replaced || rep.SHA256 != digest(archive) || validated == "" {
		t.Errorf("report = %+v validated=%q", rep, validated)
	}
	if _, err := os.Stat(filepath.Join(dest, "parsers", "x", "y", "parser.yaml")); err != nil {
		t.Errorf("not installed: %v", err)
	}
	// second run replaces and leaves no leftovers
	rep, err = Update(context.Background(), s.opts(dest))
	if err != nil || !rep.Replaced {
		t.Fatalf("replace: %+v %v", rep, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
	// bare digest checksum file also works
	s.checksum = digest(archive)
	if _, err := Update(context.Background(), s.opts(dest)); err != nil {
		t.Errorf("bare digest: %v", err)
	}
	// archive without a wrapping directory
	flat := buildArchive(t, []entry{{name: "./registry.yaml", body: "format: 1\n"}, {name: "parsers/a/b/parser.yaml", body: "x"}})
	s.archive, s.checksum = flat, digest(flat)
	if _, err := Update(context.Background(), s.opts(dest)); err != nil {
		t.Errorf("flat archive: %v", err)
	}
}

func TestUpdateFailuresLeaveDestIntact(t *testing.T) {
	t.Parallel()
	archive := buildArchive(t, good)
	s := newServer(t, archive, digest(archive))
	dest := filepath.Join(t.TempDir(), "official")
	if _, err := Update(context.Background(), s.opts(dest)); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dest, "registry.yaml")
	orig, _ := os.ReadFile(marker)

	check := func(name string, mutate func(), wantErr error, wantMsg string) {
		t.Helper()
		saved := *s
		mutate()
		_, err := Update(context.Background(), s.opts(dest))
		*s = saved
		if err == nil {
			t.Errorf("%s: expected error", name)
			return
		}
		if wantErr != nil && !errors.Is(err, wantErr) {
			t.Errorf("%s: got %v, want %v", name, err, wantErr)
		}
		if wantMsg != "" && !strings.Contains(err.Error(), wantMsg) {
			t.Errorf("%s: got %v, want %q", name, err, wantMsg)
		}
		if now, _ := os.ReadFile(marker); !bytes.Equal(now, orig) {
			t.Errorf("%s: destination was modified", name)
		}
	}
	check("checksum mismatch", func() { s.checksum = strings.Repeat("0", 64) }, ErrChecksumMismatch, "")
	check("bad checksum file", func() { s.checksum = "not a digest" }, nil, "no SHA-256 digest")
	check("http error", func() { s.status = http.StatusInternalServerError }, nil, "HTTP 500")
	check("not gzip", func() { s.archive = []byte("plain"); s.checksum = digest(s.archive) }, nil, "not gzip")
	traversal := buildArchive(t, []entry{{name: "../evil", body: "x"}})
	check("traversal", func() { s.archive, s.checksum = traversal, digest(traversal) }, ErrUnsafePath, "")
	abs := buildArchive(t, []entry{{name: "/etc/passwd", body: "x"}})
	check("absolute", func() { s.archive, s.checksum = abs, digest(abs) }, ErrUnsafePath, "")
	symlink := buildArchive(t, []entry{{name: "registry.yaml", body: "format: 1\n"}, {name: "link", typ: tar.TypeSymlink, link: "/etc"}})
	check("symlink", func() { s.archive, s.checksum = symlink, digest(symlink) }, ErrUnsafePath, "")
	noManifest := buildArchive(t, []entry{{name: "parsers/a/b/parser.yaml", body: "x"}})
	check("no manifest", func() { s.archive, s.checksum = noManifest, digest(noManifest) }, ErrNoManifest, "")
	// validation error path needs a Validate func: run separately
	o := s.opts(dest)
	o.Validate = func(string) error { return errors.New("nope") }
	if _, err := Update(context.Background(), o); err == nil || !strings.Contains(err.Error(), "downloaded registry is invalid: nope") {
		t.Errorf("validate: %v", err)
	}
	big := buildArchive(t, []entry{{name: "registry.yaml", body: strings.Repeat("x", 5000)}})
	o = s.opts(dest)
	s.archive, s.checksum = big, digest(big)
	o.MaxExtractedSize = 1000
	if _, err := Update(context.Background(), o); !errors.Is(err, ErrExtractTooLarge) {
		t.Errorf("extract limit: %v", err)
	}
	o = s.opts(dest)
	o.MaxArchiveSize = 10
	if _, err := Update(context.Background(), o); !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("archive limit: %v", err)
	}
	many := buildArchive(t, []entry{{name: "registry.yaml", body: "a"}, {name: "b", body: "b"}, {name: "c", body: "c"}})
	o = s.opts(dest)
	s.archive, s.checksum = many, digest(many)
	o.MaxFiles = 2
	if _, err := Update(context.Background(), o); !errors.Is(err, ErrTooManyFiles) {
		t.Errorf("file limit: %v", err)
	}
}

func TestUpdateOptionErrors(t *testing.T) {
	t.Parallel()
	if _, err := Update(context.Background(), Options{URL: "http://example.com/x.tar.gz", Dest: t.TempDir()}); !errors.Is(err, ErrInsecureURL) {
		t.Errorf("insecure: %v", err)
	}
	if _, err := Update(context.Background(), Options{URL: "https://example.com/x.tar.gz", ChecksumURL: "ftp://x", Dest: t.TempDir()}); !errors.Is(err, ErrInsecureURL) {
		t.Errorf("insecure checksum: %v", err)
	}
	if _, err := Update(context.Background(), Options{URL: "::bad", Dest: t.TempDir()}); err == nil {
		t.Error("bad URL")
	}
	if _, err := Update(context.Background(), Options{URL: "https://example.com/x.tar.gz"}); err == nil || !strings.Contains(err.Error(), "destination") {
		t.Errorf("missing dest: %v", err)
	}
	// http allowed when explicitly insecure (plain httptest server)
	archive := buildArchive(t, good)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(digest(archive)))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	if _, err := Update(context.Background(), Options{URL: srv.URL + "/r.tar.gz", Dest: filepath.Join(t.TempDir(), "d"), AllowInsecure: true}); err != nil {
		t.Errorf("allow insecure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Update(ctx, Options{URL: srv.URL + "/r.tar.gz", Dest: filepath.Join(t.TempDir(), "d"), AllowInsecure: true}); err == nil {
		t.Error("cancelled context should fail")
	}
}

func TestInstallRestoresOnFailure(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// root does not exist -> rename fails -> old restored
	if _, err := install(filepath.Join(base, "missing-root"), dest); err == nil {
		t.Error("expected failure")
	}
	if _, err := os.Stat(filepath.Join(dest, "keep")); err != nil {
		t.Errorf("previous registry not restored: %v", err)
	}
}
