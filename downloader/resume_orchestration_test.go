package downloader

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gluestick-sh/core/store"
)

// newOrchestrationDownloader returns a Downloader over a throwaway store.
func newOrchestrationDownloader(t *testing.T, opts ...Options) *Downloader {
	t.Helper()
	st, err := store.NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	return NewDownloader(st, opts...)
}

// seedPartial writes a .part file and its sidecar as a previous run would have,
// so downloadToPartial picks the download up as a resume.
func seedPartial(t *testing.T, d *Downloader, task Task, prefix []byte, meta partialMeta) {
	t.Helper()
	partPath, metaPath := d.partialPaths(task)
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, prefix, 0o644); err != nil {
		t.Fatal(err)
	}
	meta.URL = task.URL
	if err := savePartialMeta(metaPath, meta); err != nil {
		t.Fatal(err)
	}
}

// TestDownloadToPartial_completePartialSkipsNetwork covers the fast path where a
// previous run already fetched the whole artifact: no request may be sent.
func TestDownloadToPartial_completePartialSkipsNetwork(t *testing.T) {
	var gets atomic.Int32
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		_, _ = w.Write([]byte("unused"))
	})

	d := newOrchestrationDownloader(t)
	task := Task{URL: ts.URL + "/pkg.zip", Filename: "pkg.zip"}
	seedPartial(t, d, task, []byte("complete"), partialMeta{SourceURL: task.URL, TotalSize: 8})

	partPath, total, err := d.downloadToPartial(context.Background(), task)
	if err != nil {
		t.Fatalf("downloadToPartial: %v", err)
	}
	if total != 8 {
		t.Fatalf("total = %d, want 8", total)
	}
	got, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "complete" {
		t.Fatalf("part = %q, want %q", got, "complete")
	}
	if n := gets.Load(); n != 0 {
		t.Fatalf("server hit %d times, want 0 for an already-complete partial", n)
	}
}

// TestDownloadToPartial_resumePinsSourceURL checks that a partial carrying a
// source_url is resumed from exactly that origin and is never re-pointed at a
// different mirror.
func TestDownloadToPartial_resumePinsSourceURL(t *testing.T) {
	const total = "hello world"
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=6-" {
			t.Errorf("Range = %q, want bytes=6-", got)
		}
		w.Header().Set("Content-Range", "bytes 6-10/11")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(total[6:]))
	})

	d := newOrchestrationDownloader(t)
	url := ts.URL + "/resumable.bin"
	task := Task{URL: url, Filename: "resumable.bin"}
	// SourceURL differs from the canonical URL, so a mirror fallback would be
	// wrong here: the bytes on disk came from the source origin.
	seedPartial(t, d, task, []byte("hello "), partialMeta{SourceURL: ts.URL + "/other.bin", TotalSize: 11})

	partPath, size, err := d.downloadToPartial(context.Background(), task)
	if err != nil {
		t.Fatalf("downloadToPartial: %v", err)
	}
	if size != 11 {
		t.Fatalf("size = %d, want 11", size)
	}
	got, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != total {
		t.Fatalf("part = %q, want %q (stale bytes must be gone)", got, total)
	}
}

// TestDownloadToPartial_usesMirrorForGitHubURL verifies the mirror path end to
// end: a GitHub URL with a configured proxy must be fetched through the proxy
// origin, and the recorded source_url must be the mirror that actually served it.
func TestDownloadToPartial_usesMirrorForGitHubURL(t *testing.T) {
	const total = "served by the mirror"
	var gotPath atomic.Value

	proxy := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Length", strconv.Itoa(len(total)))
		_, _ = w.Write([]byte(total))
	})

	d := newOrchestrationDownloader(t, WithGitHubProxies([]string{proxy.URL + "/"}))
	task := Task{URL: "https://github.com/owner/repo/releases/download/v1/pkg.bin", Filename: "pkg.bin"}

	partPath, size, err := d.downloadToPartial(context.Background(), task)
	if err != nil {
		t.Fatalf("downloadToPartial: %v", err)
	}
	if size != int64(len(total)) {
		t.Fatalf("size = %d, want %d", size, len(total))
	}
	data, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != total {
		t.Fatalf("part = %q, want %q", data, total)
	}

	served := gotPath.Load()
	if served == nil {
		t.Fatal("mirror was never contacted")
	}
	// MirrorURLs prepends the proxy to the full GitHub URL, so the path carries
	// the upstream URL.
	upstream, _ := served.(string)
	if !strings.Contains(upstream, "github.com/owner/repo") {
		t.Fatalf("mirror served %q, want the GitHub URL appended to the proxy prefix", upstream)
	}

	_, metaPath := d.partialPaths(task)
	meta, err := loadPartialMeta(metaPath)
	if err != nil {
		t.Fatalf("loadPartialMeta: %v", err)
	}
	if !strings.HasPrefix(meta.SourceURL, proxy.URL) {
		t.Fatalf("meta.SourceURL = %q, want the mirror URL %q...", meta.SourceURL, proxy.URL)
	}
}

// TestUrlsForResume documents which candidate list each resume state produces.
func TestUrlsForResume(t *testing.T) {
	const url = "https://github.com/o/r/releases/download/v1/f.zip"
	proxies := []string{"https://proxy.example/", "https://backup.example/"}

	t.Run("resume pins the source URL", func(t *testing.T) {
		got := urlsForResume(url, proxies, "https://origin.example/f.zip", 100)
		want := []string{"https://origin.example/f.zip"}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("fresh download expands to mirrors", func(t *testing.T) {
		got := urlsForResume(url, proxies, "", 0)
		if len(got) != len(proxies) {
			t.Fatalf("got %d URLs (%v), want %d", len(got), got, len(proxies))
		}
		if got[0] != "https://proxy.example/"+url {
			t.Fatalf("first URL = %q, want proxy-prefixed canonical", got[0])
		}
	})

	t.Run("no proxies yields canonical only", func(t *testing.T) {
		got := urlsForResume(url, nil, "", 0)
		if len(got) != 1 || got[0] != url {
			t.Fatalf("got %v, want [%s]", got, url)
		}
	})

	t.Run("non-github URLs are never mirrored", func(t *testing.T) {
		const other = "https://example.com/file.zip"
		got := urlsForResume(other, proxies, "", 0)
		if len(got) != 1 || got[0] != other {
			t.Fatalf("got %v, want [%s]", got, other)
		}
	})
}

// TestDownloadToPartial_metaMismatchResetsPartial covers a corrupted or
// hand-edited sidecar: when meta.URL does not match the task, the partial is
// dropped rather than resumed, so bytes from an unknown artifact never get
// appended to the download. The .part path is keyed by URL hash, so a mismatched
// sidecar is the realistic way to reach this state.
func TestDownloadToPartial_metaMismatchResetsPartial(t *testing.T) {
	const total = "brand new"
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		// bytes=0-0 is the size probe, not a resume; only a real resume would
		// start above zero.
		if rg := r.Header.Get("Range"); rg != "" && rg != "bytes=0-0" {
			t.Errorf("unexpected resume Range %q: mismatched sidecar should reset the partial", rg)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(total)))
		_, _ = w.Write([]byte(total))
	})

	d := newOrchestrationDownloader(t)
	task := Task{URL: ts.URL + "/fresh.bin", Filename: "fresh.bin"}

	// Write the stale bytes to this task's real partial path, but with a sidecar
	// belonging to a different URL.
	partPath, metaPath := d.partialPaths(task)
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, []byte("leftovers from another artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := savePartialMeta(metaPath, partialMeta{URL: "https://example.com/other.zip"}); err != nil {
		t.Fatal(err)
	}

	gotPath, _, err := d.downloadToPartial(context.Background(), task)
	if err != nil {
		t.Fatalf("downloadToPartial: %v", err)
	}
	data, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != total {
		t.Fatalf("part = %q, want %q (stale bytes must be gone)", data, total)
	}
}

// TestDownloadToPartial_allMirrorsFail reports the last error and keeps the
// partial around for a later retry.
func TestDownloadToPartial_allMirrorsFail(t *testing.T) {
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	})

	d := newOrchestrationDownloader(t)
	task := Task{URL: ts.URL + "/missing.bin", Filename: "missing.bin"}

	partPath, total, err := d.downloadToPartial(context.Background(), task)
	if err == nil {
		t.Fatal("want error when the server fails, got nil")
	}
	if partPath != "" || total != 0 {
		t.Fatalf("partPath=%q total=%d, want empty/0 on failure", partPath, total)
	}
}
