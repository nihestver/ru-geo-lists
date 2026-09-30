package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func serve(t *testing.T, body string, failFirst int, checksum string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/file.dat", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if int(n) <= failFirst {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/file.dat.sha256sum", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksum + "  file.dat\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestDownloadWithChecksum(t *testing.T) {
	body := strings.Repeat("x", 1000)
	srv, _ := serve(t, body, 0, sum(body))
	dst := filepath.Join(t.TempDir(), "sub", "file.dat")
	res, err := Download(context.Background(), dst, Options{
		URL: srv.URL + "/file.dat", ChecksumURL: srv.URL + "/file.dat.sha256sum", MinSize: 500, Backoff: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Size != 1000 || res.SHA256 != sum(body) || res.Attempts != 1 {
		t.Fatalf("unexpected result %+v", res)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != body {
		t.Fatalf("file content mismatch: %v", err)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
}

func TestDownloadRetries(t *testing.T) {
	body := "hello world"
	srv, calls := serve(t, body, 2, sum(body))
	dst := filepath.Join(t.TempDir(), "file.dat")
	res, err := Download(context.Background(), dst, Options{
		URL: srv.URL + "/file.dat", ChecksumURL: srv.URL + "/file.dat.sha256sum", Attempts: 3, Backoff: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 3 || atomic.LoadInt32(calls) != 3 {
		t.Fatalf("expected 3 attempts, got %d (calls %d)", res.Attempts, *calls)
	}
}

func TestDownloadGivesUp(t *testing.T) {
	srv, calls := serve(t, "x", 100, "")
	dst := filepath.Join(t.TempDir(), "file.dat")
	_, err := Download(context.Background(), dst, Options{URL: srv.URL + "/file.dat", Attempts: 2, Backoff: time.Millisecond})
	if err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", *calls)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("destination must not exist after failure")
	}
}

func TestDownloadTooSmall(t *testing.T) {
	srv, _ := serve(t, "tiny", 0, sum("tiny"))
	dst := filepath.Join(t.TempDir(), "file.dat")
	_, err := Download(context.Background(), dst, Options{URL: srv.URL + "/file.dat", MinSize: 100, Attempts: 1})
	if err == nil || !strings.Contains(err.Error(), "suspiciously small") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	srv, _ := serve(t, "content", 0, sum("other"))
	dst := filepath.Join(t.TempDir(), "file.dat")
	_, err := Download(context.Background(), dst, Options{
		URL: srv.URL + "/file.dat", ChecksumURL: srv.URL + "/file.dat.sha256sum", Attempts: 2, Backoff: time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected checksum error, got %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("destination must not exist after checksum failure")
	}
}

func TestDownloadNotFound(t *testing.T) {
	srv, _ := serve(t, "x", 0, "")
	dst := filepath.Join(t.TempDir(), "file.dat")
	_, err := Download(context.Background(), dst, Options{URL: srv.URL + "/missing", Attempts: 1})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestDownloadCanceled(t *testing.T) {
	srv, _ := serve(t, "x", 100, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Download(ctx, filepath.Join(t.TempDir(), "f"), Options{URL: srv.URL + "/file.dat", Attempts: 5, Backoff: time.Hour})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseChecksum(t *testing.T) {
	want := sum("abc")
	for _, in := range []string{want + "  geoip.dat\n", strings.ToUpper(want) + " *geoip.dat", want} {
		got, err := ParseChecksum(in)
		if err != nil || got != want {
			t.Errorf("ParseChecksum(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc  file", "zz" + want[2:] + "  file"} {
		if _, err := ParseChecksum(in); err == nil {
			t.Errorf("ParseChecksum(%q) should fail", in)
		}
	}
}
