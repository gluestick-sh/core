package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"

	"github.com/gluestick-sh/core/humanize"
)

// stderrOut wraps stderr so progress bar updates flush immediately on Windows.
var stderrOut io.Writer = &flushWriter{w: os.Stderr}

// progressBarOut clears the current stderr row before each bar render. progressbar with
// OptionUseANSICodes(true) skips pre-clear and only appends on Windows, which garbles output.
var progressBarOut io.Writer = &progressBarWriter{w: stderrOut}

type flushWriter struct {
	w io.Writer
}

func (f *flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if file, ok := f.w.(*os.File); ok {
		_ = file.Sync()
	}
	return n, err
}

type progressBarWriter struct {
	w io.Writer
}

func (p *progressBarWriter) Write(b []byte) (int, error) {
	if len(b) > 0 && string(b) != "\r" {
		clearProgressLine(p.w)
	}
	return p.w.Write(b)
}

type partialMeta struct {
	URL       string `json:"url"`
	SourceURL string `json:"source_url,omitempty"` // actual URL used (mirror or direct)
	TotalSize int64  `json:"total_size,omitempty"`
}

func (d *Downloader) partialPaths(task Task) (partPath, metaPath string) {
	id := partialID(task.URL)
	dir := filepath.Join(d.store.Path(), ".partial")
	return filepath.Join(dir, id+".part"), filepath.Join(dir, id+".meta.json")
}

func partialID(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:16])
}

func loadPartialMeta(metaPath string) (partialMeta, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return partialMeta{}, err
	}
	var meta partialMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return partialMeta{}, err
	}
	return meta, nil
}

func savePartialMeta(metaPath string, meta partialMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0644)
}

func parseContentRange(header string) (start, end, total int64, ok bool) {
	// bytes 500-999/1000  or  bytes 500-999/*
	if !strings.HasPrefix(header, "bytes ") {
		return 0, 0, 0, false
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return 0, 0, 0, false
	}
	rangeAndTotal := strings.SplitN(parts[1], "/", 2)
	if len(rangeAndTotal) != 2 {
		return 0, 0, 0, false
	}
	rangeParts := strings.SplitN(rangeAndTotal[0], "-", 2)
	if len(rangeParts) != 2 {
		return 0, 0, 0, false
	}
	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	end, err = strconv.ParseInt(rangeParts[1], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	if rangeAndTotal[1] != "*" {
		total, err = strconv.ParseInt(rangeAndTotal[1], 10, 64)
		if err != nil {
			return 0, 0, 0, false
		}
	}
	return start, end, total, true
}

// downloadToPartial downloads task.URL into a resumable .part file.
// On success the caller must remove the partial files after ingesting into cache store.
func (d *Downloader) downloadToPartial(ctx context.Context, task Task) (partPath string, totalSize int64, err error) {
	partPath, metaPath := d.partialPaths(task)
	if err := d.ensurePartialDir(partPath); err != nil {
		return "", 0, err
	}

	if isFTPURL(task.URL) {
		removePartial(partPath, metaPath)
		totalSize, err := d.downloadFTPToPartial(ctx, task, partPath, metaPath)
		return partPath, totalSize, err
	}

	st, err := d.loadPartialState(task, partPath, metaPath)
	if err != nil {
		return "", 0, err
	}
	if st.complete() {
		return st.partPath, st.meta.TotalSize, nil
	}

	urls := urlsForResume(task.URL, d.ghProxies, st.meta.SourceURL, st.offset)
	var lastErr error
	for i, fetchURL := range urls {
		if i > 0 {
			// A mirror can only serve us from byte zero: partial offsets are not portable.
			st.reset()
		}

		if total, ok := d.tryParallelDownload(ctx, task, &st, fetchURL); ok {
			return st.partPath, total, nil
		}

		totalSize, usedURL, err := d.fetchToPartialURL(ctx, task, fetchURL, &st)
		if err == nil {
			st.commit(usedURL, totalSize)
			return st.partPath, totalSize, nil
		}
		lastErr = err
		if st.offset > 0 {
			// Resuming pins us to the source URL the bytes came from, so there is
			// no other mirror left to try.
			break
		}
	}
	return "", 0, lastErr
}

func effectiveBarMax(maxSize, offset int64) int64 {
	if maxSize > 0 {
		return maxSize
	}
	if offset > 0 {
		// Unknown total while resuming: avoid a 1 GiB placeholder that shows ~1%.
		return offset * 8
	}
	return 1024 * 1024 * 1024
}

func makeDownloadBar(description string, maxSize, offset int64) *progressbar.ProgressBar {
	maxSize = effectiveBarMax(maxSize, offset)
	return progressbar.NewOptions64(maxSize,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWriter(progressBarOut),
		progressbar.OptionShowCount(),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionClearOnFinish(),
	)
}

// completeDownloadBar removes the in-place progress line after download finishes.
func completeDownloadBar(bar *progressbar.ProgressBar) {
	if bar == nil {
		return
	}
	_ = bar.Clear()
	clearProgressLine(stderrOut)
}

// clearProgressLine erases a carriage-return progress row. progressbar.Clear only
// writes "\r" on Windows, so a longer bar leaves suffix text when the next line is shorter.
func clearProgressLine(w io.Writer) {
	_, _ = io.WriteString(w, "\r\033[2K")
}

func setDownloadBarProgress(bar *progressbar.ProgressBar, maxSize, offset int64) {
	if bar == nil {
		return
	}
	if maxSize > 0 {
		bar.ChangeMax64(maxSize)
	}
	if offset > 0 {
		bar.Reset()
		bar.Add64(offset)
	} else {
		bar.Add64(0)
	}
}

func setDownloadBarProgressLocked(mu *sync.Mutex, bar *progressbar.ProgressBar, maxSize, offset int64) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	setDownloadBarProgress(bar, maxSize, offset)
}

func (d *Downloader) probeContentLength(ctx context.Context, canonicalURL string) int64 {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	for _, url := range d.urlsForTask(canonicalURL) {
		if total := d.headContentLength(probeCtx, url); total > 0 {
			return total
		}
		if total := d.rangeProbeContentLength(probeCtx, url); total > 0 {
			return total
		}
	}
	return 0
}

func (d *Downloader) headContentLength(ctx context.Context, url string) int64 {
	resp, _, err := d.doRequestWithFallback(ctx, http.MethodHead, []string{url}, nil)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.ContentLength > 0 {
		return resp.ContentLength
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func (d *Downloader) rangeProbeContentLength(ctx context.Context, url string) int64 {
	resp, _, err := d.doGETWithFallback(ctx, []string{url}, func(req *http.Request) {
		req.Header.Set("Range", "bytes=0-0")
	})
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent {
		if _, _, total, ok := parseContentRange(resp.Header.Get("Content-Range")); ok && total > 0 {
			return total
		}
	}
	if resp.ContentLength > 0 {
		return resp.ContentLength
	}
	return 0
}

func doRequestWaitWithBar(ctx context.Context, client *http.Client, req *http.Request, bar *progressbar.ProgressBar, barMu *sync.Mutex) (*http.Response, error) {
	_ = ctx
	_ = bar
	_ = barMu
	return client.Do(req)
}

func downloadBarDescription(filename string, offset int64) string {
	if offset > 0 {
		return fmt.Sprintf("%s (resume %s)", filename, humanize.FormatBytes(offset))
	}
	return filename
}

// responseOutcome is the decision derived from a download response: the offset
// the body should be written at, and the total size the server claims.
type responseOutcome struct {
	offset     int64
	totalSize  int64
	restarts   bool // server refused the range: retry the whole file from zero
	shortRange bool // offset is already at/past totalSize: nothing left to fetch
}

// resolveResponse inspects a download response and normalizes it into the
// offset to write at and the total size of the artifact.
func resolveResponse(resp *http.Response, meta partialMeta, offset int64) (responseOutcome, error) {
	switch resp.StatusCode {
	case http.StatusPartialContent:
		start, _, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if ok {
			if start != offset {
				return responseOutcome{}, fmt.Errorf("unexpected Content-Range start %d, expected %d", start, offset)
			}
			if total > 0 {
				meta.TotalSize = total
			}
		}
	case http.StatusOK:
		// The server ignored Range and is sending the whole file, so any bytes
		// already on disk must be discarded rather than appended to.
		offset = 0
	case http.StatusRequestedRangeNotSatisfiable:
		if meta.TotalSize > 0 && offset >= meta.TotalSize {
			return responseOutcome{offset: offset, totalSize: meta.TotalSize, shortRange: true}, nil
		}
		return responseOutcome{restarts: true}, nil
	default:
		return responseOutcome{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// A 206 keeps whatever total we already had when Content-Range omits it;
	// other statuses advertise the size directly, but only override when the
	// server actually gave one.
	total := meta.TotalSize
	if resp.StatusCode != http.StatusPartialContent {
		if n, ok := responseContentLength(resp); ok {
			total = n
		}
	}
	return responseOutcome{offset: offset, totalSize: total}, nil
}

// responseContentLength reads the body size a server advertised, falling back
// to the Content-Length header for responses that do not populate it. The
// second result reports whether the server stated a size at all.
func responseContentLength(resp *http.Response) (int64, bool) {
	if resp.ContentLength > 0 {
		return resp.ContentLength, true
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// fetchToPartialURL downloads fetchURL over a single connection into the
// partial file, appending to any bytes already transferred when resuming. It
// updates st with the size the server reported and returns the source URL used.
func (d *Downloader) fetchToPartialURL(ctx context.Context, task Task, fetchURL string, st *partialState) (int64, string, error) {
	offset, total := st.offset, st.meta.TotalSize

	bar := makeDownloadBar(downloadBarDescription(task.Filename, offset), total, offset)
	defer completeDownloadBar(bar)

	var barMu sync.Mutex
	setDownloadBarProgressLocked(&barMu, bar, total, offset)
	pc := newProgressCounter(ctx, task.Filename, total, offset, bar, &barMu)
	pc.emit()

	// A resumed download whose total size was never recorded would show a
	// placeholder bar, so probe for the real size while the body streams in.
	if offset > 0 && total <= 0 {
		go d.probeTotalWhileResuming(ctx, task.URL, fetchURL, st, bar, &barMu, pc)
	}

	resp, err := d.getWithRange(ctx, fetchURL, offset)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	outcome, err := resolveResponse(resp, st.meta, offset)
	if err != nil {
		return 0, "", err
	}
	if outcome.restarts {
		// The server refused our range, so the partial is useless: start over.
		st.reset()
		return d.fetchToPartialURL(ctx, task, fetchURL, st)
	}
	if outcome.shortRange {
		return outcome.totalSize, fetchURL, nil
	}
	if outcome.offset == 0 {
		// The server ignored Range and resent the whole file; drop stale bytes
		// and the probe results that described them.
		_ = os.Truncate(st.partPath, 0)
		bar.Reset()
	}

	st.meta.TotalSize = outcome.totalSize
	setDownloadBarProgressLocked(&barMu, bar, outcome.totalSize, outcome.offset)
	pc.setTotal(outcome.totalSize)
	if outcome.totalSize > 0 {
		st.commit(fetchURL, outcome.totalSize)
	}

	finalSize, err := d.appendResponseBody(st.partPath, resp.Body, outcome.offset, pc)
	if err != nil {
		return 0, "", err
	}
	if st.meta.TotalSize <= 0 {
		st.meta.TotalSize = finalSize
	}
	if err := savePartialMeta(st.metaPath, st.meta); err != nil {
		return 0, "", fmt.Errorf("save partial meta: %w", err)
	}
	if st.meta.TotalSize > 0 && finalSize != st.meta.TotalSize {
		return 0, "", fmt.Errorf("incomplete download: got %d bytes, expected %d", finalSize, st.meta.TotalSize)
	}
	return st.meta.TotalSize, fetchURL, nil
}

// getWithRange issues the GET for a single-connection download, asking for the
// remainder of the file when resuming.
func (d *Downloader) getWithRange(ctx context.Context, fetchURL string, offset int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", d.userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := doRequestWaitWithBar(ctx, d.client, req, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	return resp, nil
}

// probeTotalWhileResuming looks up the artifact size for a resumed download
// that has no recorded total, updating the progress display and the sidecar
// when the lookup succeeds. It is advisory: failures are ignored.
func (d *Downloader) probeTotalWhileResuming(
	ctx context.Context,
	taskURL, fetchURL string,
	st *partialState,
	bar *progressbar.ProgressBar,
	barMu *sync.Mutex,
	pc *progressCounter,
) {
	total := d.probeContentLength(ctx, taskURL)
	if total <= 0 {
		return
	}
	setDownloadBarProgressLocked(barMu, bar, total, st.offset)
	pc.setTotal(total)
	_ = savePartialMeta(st.metaPath, partialMeta{URL: taskURL, SourceURL: fetchURL, TotalSize: total})
}
// at offset when resuming and truncating when starting over. It returns the
// final size of the file in bytes.
func (d *Downloader) appendResponseBody(partPath string, body io.Reader, offset int64, pc *progressCounter) (int64, error) {
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("open partial file: %w", err)
	}
	defer file.Close()

	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return 0, fmt.Errorf("seek partial file: %w", err)
		}
	} else if err := file.Truncate(0); err != nil {
		return 0, fmt.Errorf("truncate partial file: %w", err)
	}

	buf := d.acquireCopyBuf()
	written, err := io.CopyBuffer(file, pc.reader(body), buf)
	d.releaseCopyBuf(buf)
	if err != nil {
		// Flush what did land so the next run can resume from here.
		_ = file.Sync()
		return 0, fmt.Errorf("write partial file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return 0, fmt.Errorf("sync partial file: %w", err)
	}
	return offset + written, nil
}

func removePartial(partPath, metaPath string) {
	_ = os.Remove(partPath)
	_ = os.Remove(metaPath)
}

// ClearPartial removes resumable .part and .meta.json files for a download task.
func (d *Downloader) ClearPartial(task Task) {
	partPath, metaPath := d.partialPaths(task)
	removePartial(partPath, metaPath)
}

// ClearAllPartials removes every in-progress download under the store
// (resumable .part/.meta.json files and abandoned zip spool temps).
// Returns how many files were deleted and total bytes freed.
func (d *Downloader) ClearAllPartials() (removed int, freedBytes int64, err error) {
	dir := filepath.Join(d.store.Path(), ".partial")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, infoErr := entry.Info()
		size := int64(0)
		if infoErr == nil {
			size = info.Size()
		}
		if rmErr := os.Remove(path); rmErr == nil {
			removed++
			freedBytes += size
		}
	}
	return removed, freedBytes, nil
}
