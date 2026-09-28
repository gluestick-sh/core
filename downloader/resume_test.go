package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gluestick-sh/core/store"
)

func httpServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

// newTestState builds a partialState over a temp store seeded with prefix
// bytes, so resume paths can be exercised without the mirror loop.
func newTestState(t *testing.T, url string, prefix []byte, meta partialMeta) (*Downloader, *partialState) {
	t.Helper()
	st, err := store.NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	d := NewDownloader(st)
	dir := filepath.Join(st.Path(), ".partial")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	partPath := filepath.Join(dir, "x.part")
	metaPath := filepath.Join(dir, "x.meta.json")
	if len(prefix) > 0 {
		if err := os.WriteFile(partPath, prefix, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meta.URL = url
	if err := savePartialMeta(metaPath, meta); err != nil {
		t.Fatal(err)
	}
	return d, &partialState{
		partPath: partPath,
		metaPath: metaPath,
		taskURL:  url,
		meta:     meta,
		offset:   int64(len(prefix)),
	}
}

func TestFetchToPartialURL_appendsRemainingBytes(t *testing.T) {
	const total = "hello world"
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=6-" {
			t.Errorf("Range = %q, want bytes=6-", got)
		}
		w.Header().Set("Content-Range", "bytes 6-10/11")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(total[6:]))
	})

	url := ts.URL + "/a.txt"
	d, st := newTestState(t, url, []byte("hello "), partialMeta{SourceURL: url, TotalSize: 11})

	size, used, err := d.fetchToPartialURL(context.Background(), Task{URL: url, Filename: "a.txt"}, url, st)
	if err != nil {
		t.Fatalf("fetchToPartialURL: %v", err)
	}
	if size != 11 || used != url {
		t.Fatalf("size=%d used=%q, want 11 and %q", size, used, url)
	}
	got, err := os.ReadFile(st.partPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != total {
		t.Fatalf("part = %q, want %q", got, total)
	}
}

func TestFetchToPartialURL_serverIgnoresRangeRestarts(t *testing.T) {
	const total = "fresh payload"
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		// No Content-Range: the server resent the whole file.
		w.Header().Set("Content-Length", strconv.Itoa(len(total)))
		_, _ = w.Write([]byte(total))
	})

	url := ts.URL + "/b.txt"
	d, st := newTestState(t, url, []byte("stale junk that must be dropped"), partialMeta{SourceURL: url, TotalSize: 999})

	size, _, err := d.fetchToPartialURL(context.Background(), Task{URL: url, Filename: "b.txt"}, url, st)
	if err != nil {
		t.Fatalf("fetchToPartialURL: %v", err)
	}
	if size != int64(len(total)) {
		t.Fatalf("size = %d, want %d", size, len(total))
	}
	got, err := os.ReadFile(st.partPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != total {
		t.Fatalf("part = %q, want %q (stale bytes must not survive)", got, total)
	}
}

func TestFetchToPartialURL_rangeNotSatisfiableRestartsFromZero(t *testing.T) {
	const total = "restarted"
	var attempts int
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Header.Get("Range") != "" {
			http.Error(w, "range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(total)))
		_, _ = w.Write([]byte(total))
	})

	url := ts.URL + "/c.txt"
	d, st := newTestState(t, url, []byte("bytes that no longer fit"), partialMeta{SourceURL: url, TotalSize: 4096})

	size, _, err := d.fetchToPartialURL(context.Background(), Task{URL: url, Filename: "c.txt"}, url, st)
	if err != nil {
		t.Fatalf("fetchToPartialURL: %v", err)
	}
	if size != int64(len(total)) {
		t.Fatalf("size = %d, want %d", size, len(total))
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (refused range then full retry)", attempts)
	}
	got, err := os.ReadFile(st.partPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != total {
		t.Fatalf("part = %q, want %q", got, total)
	}
}

func TestFetchToPartialURL_rangeNotSatisfiableWhenAlreadyComplete(t *testing.T) {
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "range", http.StatusRequestedRangeNotSatisfiable)
	})

	url := ts.URL + "/d.txt"
	d, st := newTestState(t, url, []byte("done"), partialMeta{SourceURL: url, TotalSize: 4})

	size, _, err := d.fetchToPartialURL(context.Background(), Task{URL: url, Filename: "d.txt"}, url, st)
	if err != nil {
		t.Fatalf("already-complete partial must not error: %v", err)
	}
	if size != 4 {
		t.Fatalf("size = %d, want 4", size)
	}
}

func TestFetchToPartialURL_incompleteBodyIsError(t *testing.T) {
	ts := httpServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("only a few bytes"))
	})

	url := ts.URL + "/e.txt"
	d, st := newTestState(t, url, nil, partialMeta{SourceURL: url})

	if _, _, err := d.fetchToPartialURL(context.Background(), Task{URL: url, Filename: "e.txt"}, url, st); err == nil {
		t.Fatal("want error for truncated body, got nil")
	}
}

func TestResolveResponse(t *testing.T) {
	resp := func(code int, headers map[string]string) *http.Response {
		r := &http.Response{StatusCode: code, Header: http.Header{}}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	tests := []struct {
		name        string
		resp        *http.Response
		meta        partialMeta
		offset      int64
		wantOffset  int64
		wantTotal   int64
		wantRestart bool
		wantShort   bool
		wantErr     bool
	}{
		{
			name:       "206 keeps recorded total when Content-Range omits it",
			resp:       resp(http.StatusPartialContent, map[string]string{"Content-Range": "bytes 4-9/*"}),
			meta:       partialMeta{TotalSize: 77},
			offset:     4,
			wantOffset: 4,
			wantTotal:  77,
		},
		{
			name:       "200 discards resume offset",
			resp:       resp(http.StatusOK, map[string]string{"Content-Length": "10"}),
			meta:       partialMeta{TotalSize: 999},
			offset:     4,
			wantOffset: 0,
			wantTotal:  10,
		},
		{
			name:    "206 start mismatch is rejected",
			resp:    resp(http.StatusPartialContent, map[string]string{"Content-Range": "bytes 5-9/10"}),
			offset:  4,
			wantErr: true,
		},
		{
			name:        "416 restarts when more bytes are expected",
			resp:        resp(http.StatusRequestedRangeNotSatisfiable, nil),
			meta:        partialMeta{TotalSize: 100},
			offset:      10,
			wantRestart: true,
		},
		{
			// offset is not consulted on this path: the caller returns totalSize
			// immediately without touching the partial file.
			name:      "416 is a no-op when offset already reached total",
			resp:      resp(http.StatusRequestedRangeNotSatisfiable, nil),
			meta:      partialMeta{TotalSize: 100},
			offset:    100,
			wantTotal: 100,
			wantShort: true,
		},
		{
			name:    "other statuses are errors",
			resp:    resp(http.StatusNotFound, nil),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveResponse(tc.resp, tc.meta, tc.offset)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveResponse: %v", err)
			}
			if got.totalSize != tc.wantTotal ||
				got.restarts != tc.wantRestart || got.shortRange != tc.wantShort {
				t.Fatalf("got %+v, want total=%d restart=%v short=%v",
					got, tc.wantTotal, tc.wantRestart, tc.wantShort)
			}
			if !got.shortRange && got.offset != tc.wantOffset {
				t.Fatalf("got offset %d, want %d", got.offset, tc.wantOffset)
			}
		})
	}
}

func TestPartialState_resetClearsMeta(t *testing.T) {
	url := "https://example.com/pkg.zip"
	st := &partialState{
		partPath: filepath.Join(t.TempDir(), "x.part"),
		metaPath: filepath.Join(t.TempDir(), "x.meta.json"),
		taskURL:  url,
		meta:     partialMeta{URL: url, SourceURL: "https://mirror/pkg.zip", TotalSize: 42},
		offset:   42,
	}
	st.reset()

	if st.offset != 0 {
		t.Fatalf("offset = %d, want 0", st.offset)
	}
	if st.meta.SourceURL != "" || st.meta.TotalSize != 0 {
		t.Fatalf("meta not cleared: %+v", st.meta)
	}
	if st.meta.URL != url {
		t.Fatalf("meta.URL = %q, want task URL %q", st.meta.URL, url)
	}
	if st.complete() {
		t.Fatal("reset state must not report complete")
	}
}

func TestPartialState_complete(t *testing.T) {
	tests := []struct {
		name   string
		total  int64
		offset int64
		want   bool
	}{
		{"unknown total", 0, 500, false},
		{"partially done", 1000, 500, false},
		{"exactly complete", 1000, 1000, true},
		{"overshot", 1000, 1200, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &partialState{meta: partialMeta{TotalSize: tc.total}, offset: tc.offset}
			if got := st.complete(); got != tc.want {
				t.Fatalf("complete() = %v, want %v", got, tc.want)
			}
		})
	}
}
