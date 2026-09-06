// Package remote downloads a registry archive over HTTPS, verifies its
// checksum, extracts it safely and installs it atomically.
//
// Threat model: the archive comes from a third party and may be
// malicious. Extraction therefore refuses absolute paths, parent
// references, symlinks, hard links and special files, and bounds the
// number and total size of extracted files. The install is a rename of a
// fully validated directory, so a failed update never leaves a partially
// written registry behind.
package remote

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// DefaultURL is the official registry archive published with each
// jsonize release.
const DefaultURL = "https://github.com/nao1215/jsonize/releases/latest/download/jsonize-registry.tar.gz"

// Limits.
const (
	DefaultMaxArchiveSize   = 16 * 1024 * 1024
	DefaultMaxExtractedSize = 64 * 1024 * 1024
	DefaultMaxFiles         = 5000
	DefaultTimeout          = 60 * time.Second
	maxChecksumSize         = 4096
)

// Options configures Update.
type Options struct {
	// URL of the .tar.gz archive. Must use https unless AllowInsecure.
	URL string
	// ChecksumURL of the SHA-256 file (default: URL + ".sha256").
	ChecksumURL string
	// Dest is the directory that will hold the registry after success.
	Dest string
	// Client performs requests (nil = default with DefaultTimeout).
	Client *http.Client
	// AllowInsecure permits http:// (intended for tests only).
	AllowInsecure bool
	// Validate inspects the extracted directory before it is installed.
	// Returning an error aborts the update and leaves Dest untouched.
	Validate func(dir string) error

	MaxArchiveSize   int64
	MaxExtractedSize int64
	MaxFiles         int
}

// Report summarises a successful update.
type Report struct {
	Files    int
	Bytes    int64
	SHA256   string
	Replaced bool
}

// Errors.
var (
	ErrInsecureURL      = errors.New("registry URL must use https")
	ErrChecksumMismatch = errors.New("archive checksum does not match")
	ErrArchiveTooLarge  = errors.New("archive exceeds the size limit")
	ErrUnsafePath       = errors.New("archive contains an unsafe path")
	ErrTooManyFiles     = errors.New("archive contains too many files")
	ErrExtractTooLarge  = errors.New("extracted content exceeds the size limit")
	ErrNoManifest       = errors.New("archive does not contain registry.yaml at its root")
)

func (o *Options) defaults() {
	if o.ChecksumURL == "" {
		o.ChecksumURL = o.URL + ".sha256"
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: DefaultTimeout}
	}
	if o.MaxArchiveSize <= 0 {
		o.MaxArchiveSize = DefaultMaxArchiveSize
	}
	if o.MaxExtractedSize <= 0 {
		o.MaxExtractedSize = DefaultMaxExtractedSize
	}
	if o.MaxFiles <= 0 {
		o.MaxFiles = DefaultMaxFiles
	}
}

// Update performs the download, verification, extraction and install.
func Update(ctx context.Context, opts Options) (*Report, error) {
	opts.defaults()
	if err := checkURL(opts.URL, opts.AllowInsecure); err != nil {
		return nil, err
	}
	if err := checkURL(opts.ChecksumURL, opts.AllowInsecure); err != nil {
		return nil, err
	}
	if opts.Dest == "" {
		return nil, errors.New("destination directory is required")
	}
	want, err := fetchChecksum(ctx, opts)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(opts.Dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	archive, err := os.CreateTemp(parent, ".jsonize-download-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	got, err := download(ctx, opts, archive)
	if err != nil {
		return nil, err
	}
	if got != want {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, want, got)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(parent, ".jsonize-registry-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	report, err := extract(archive, tmp, &opts)
	if err != nil {
		return nil, err
	}
	report.SHA256 = got
	root, err := archiveRoot(tmp)
	if err != nil {
		return nil, err
	}
	if opts.Validate != nil {
		if err := opts.Validate(root); err != nil {
			return nil, fmt.Errorf("downloaded registry is invalid: %w", err)
		}
	}
	replaced, err := install(root, opts.Dest)
	if err != nil {
		return nil, err
	}
	report.Replaced = replaced
	return report, nil
}

func checkURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && allowInsecure {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInsecureURL, raw)
}

func get(ctx context.Context, opts *Options, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jsonize")
	resp, err := opts.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetching %s: HTTP %s", rawURL, resp.Status)
	}
	return resp, nil
}

// fetchChecksum reads the first 64-hex-digit token of the checksum file
// ("<hex>  <name>" as written by sha256sum, or a bare digest).
func fetchChecksum(ctx context.Context, opts Options) (string, error) {
	resp, err := get(ctx, &opts, opts.ChecksumURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumSize))
	if err != nil {
		return "", err
	}
	for _, tok := range strings.Fields(string(data)) {
		if len(tok) == sha256.Size*2 {
			if _, err := hex.DecodeString(tok); err == nil {
				return strings.ToLower(tok), nil
			}
		}
	}
	return "", fmt.Errorf("checksum file %s contains no SHA-256 digest", opts.ChecksumURL)
}

// download streams the archive into w and returns its SHA-256.
func download(ctx context.Context, opts Options, w io.Writer) (string, error) {
	resp, err := get(ctx, &opts, opts.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, opts.MaxArchiveSize+1))
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", opts.URL, err)
	}
	if n > opts.MaxArchiveSize {
		return "", fmt.Errorf("%w (%d bytes)", ErrArchiveTooLarge, opts.MaxArchiveSize)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extract unpacks a tar.gz into dir with the safety checks described in
// the package documentation.
func extract(r io.Reader, dir string, opts *Options) (*Report, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("archive is not gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	report := &Report{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if name == "." {
			continue
		}
		if path.IsAbs(name) || strings.HasPrefix(name, "../") || name == ".." || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) {
			return nil, fmt.Errorf("%w: %q", ErrUnsafePath, hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if rel, err := filepath.Rel(dir, target); err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("%w: %q", ErrUnsafePath, hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			report.Files++
			if report.Files > opts.MaxFiles {
				return nil, fmt.Errorf("%w (limit %d)", ErrTooManyFiles, opts.MaxFiles)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
			if err != nil {
				return nil, err
			}
			n, err := io.Copy(f, io.LimitReader(tr, opts.MaxExtractedSize-report.Bytes+1))
			_ = f.Close()
			if err != nil {
				return nil, err
			}
			report.Bytes += n
			if report.Bytes > opts.MaxExtractedSize {
				return nil, fmt.Errorf("%w (limit %d bytes)", ErrExtractTooLarge, opts.MaxExtractedSize)
			}
		default:
			return nil, fmt.Errorf("%w: %q is not a regular file or directory", ErrUnsafePath, hdr.Name)
		}
	}
	return report, nil
}

// archiveRoot returns the directory holding registry.yaml: either tmp
// itself or its single top-level directory.
func archiveRoot(tmp string) (string, error) {
	if _, err := os.Stat(filepath.Join(tmp, "registry.yaml")); err == nil {
		return tmp, nil
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		root := filepath.Join(tmp, entries[0].Name())
		if _, err := os.Stat(filepath.Join(root, "registry.yaml")); err == nil {
			return root, nil
		}
	}
	return "", ErrNoManifest
}

// install swaps root into dest atomically, keeping the previous registry
// until the new one is in place and restoring it if the swap fails.
func install(root, dest string) (bool, error) {
	old := dest + ".old"
	_ = os.RemoveAll(old)
	replaced := false
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return false, fmt.Errorf("moving previous registry aside: %w", err)
		}
		replaced = true
	}
	if err := os.Rename(root, dest); err != nil {
		if replaced {
			_ = os.Rename(old, dest)
		}
		return false, fmt.Errorf("installing registry: %w", err)
	}
	_ = os.RemoveAll(old)
	return replaced, nil
}
