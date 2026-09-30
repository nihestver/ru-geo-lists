// Package fetch downloads upstream files with retries, size and checksum checks.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Options configures Download.
type Options struct {
	URL string
	// ChecksumURL points to a "<sha256 hex>  <name>" file. Empty disables verification.
	ChecksumURL string
	// MinSize makes downloads smaller than this many bytes fail.
	MinSize int64
	// Attempts is the total number of tries (default 4).
	Attempts int
	// Backoff is the delay before the second attempt; it doubles every time (default 2s).
	Backoff time.Duration
	// Client defaults to an http.Client with a 5-minute timeout.
	Client *http.Client
	// Logf receives progress messages; nil disables logging.
	Logf func(format string, args ...any)
}

// Result describes a successful download.
type Result struct {
	Size     int64
	SHA256   string
	Attempts int
}

// Download fetches opt.URL into dst. Every attempt is verified (HTTP status,
// minimum size, checksum) before the file is moved into place, so dst is
// either untouched or complete and verified.
func Download(ctx context.Context, dst string, opt Options) (Result, error) {
	if opt.Attempts <= 0 {
		opt.Attempts = 4
	}
	if opt.Backoff <= 0 {
		opt.Backoff = 2 * time.Second
	}
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	var errs []error
	delay := opt.Backoff
	for attempt := 1; attempt <= opt.Attempts; attempt++ {
		res, err := attemptDownload(ctx, dst, opt)
		if err == nil {
			res.Attempts = attempt
			return res, nil
		}
		errs = append(errs, fmt.Errorf("attempt %d: %w", attempt, err))
		opt.Logf("download %s: attempt %d/%d failed: %v", opt.URL, attempt, opt.Attempts, err)
		if attempt == opt.Attempts {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, errors.Join(append(errs, ctx.Err())...)
		case <-time.After(delay):
		}
		delay *= 2
	}
	return Result{}, fmt.Errorf("download %s failed after %d attempts: %w", opt.URL, opt.Attempts, errors.Join(errs...))
}

func attemptDownload(ctx context.Context, dst string, opt Options) (Result, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Result{}, err
	}
	tmp := dst + ".part"
	defer os.Remove(tmp)

	f, err := os.Create(tmp)
	if err != nil {
		return Result{}, err
	}
	size, sum, err := fetchTo(ctx, opt.Client, opt.URL, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Result{}, err
	}
	if size < opt.MinSize {
		return Result{}, fmt.Errorf("downloaded %d bytes, less than the minimum of %d (suspiciously small)", size, opt.MinSize)
	}
	if opt.ChecksumURL != "" {
		want, err := fetchChecksum(ctx, opt.Client, opt.ChecksumURL)
		if err != nil {
			return Result{}, err
		}
		if want != sum {
			return Result{}, fmt.Errorf("sha256 mismatch: file %s, expected %s", sum, want)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		return Result{}, err
	}
	return Result{Size: size, SHA256: sum}, nil
}

func fetchTo(ctx context.Context, client *http.Client, url string, w io.Writer) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("User-Agent", "geo2list (+https://github.com/nihestver/ru-geo-lists)")
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("GET %s: %w", url, err)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func fetchChecksum(ctx context.Context, client *http.Client, url string) (string, error) {
	var b strings.Builder
	if _, _, err := fetchTo(ctx, client, url, &b); err != nil {
		return "", err
	}
	return ParseChecksum(b.String())
}

// ParseChecksum extracts the hex digest from "<sha256 hex>  <name>" content.
func ParseChecksum(content string) (string, error) {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return "", errors.New("checksum file is empty")
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != sha256.Size*2 {
		return "", fmt.Errorf("checksum %q is not a sha256 hex digest", fields[0])
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("checksum %q is not hex: %w", fields[0], err)
	}
	return sum, nil
}
