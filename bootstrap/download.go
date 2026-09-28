package bootstrap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gluestick-sh/core/verbose"
)

// downloadToFile streams the first reachable URL into destPath. Bytes land in
// destPath+".part" and are renamed into place only after a complete, non-empty
// body arrives, so a failed or cancelled mirror never leaves a partial file that
// a later "already installed" check would trust.
func (b *Bootstrap) downloadToFile(ctx context.Context, urls []string, destPath, description string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("download %s: no URLs to try", description)
	}

	partPath := destPath + ".part"
	defer removeWithRetry(partPath)

	var lastErr error
	for i, dlURL := range urls {
		if err := ctx.Err(); err != nil {
			return err
		}
		if i > 0 {
			verbose.Progressf("  Trying mirror %d/%d: %s\n", i+1, len(urls), shortenURL(dlURL))
		}
		err := b.downloadToFileURL(ctx, dlURL, partPath, destPath, description)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			// The caller went away; another mirror would not help.
			return err
		}
		verbose.Fprintf("Mirror %d/%d failed (%s): %v\n", i+1, len(urls), shortenURL(dlURL), err)
		lastErr = err
	}
	return fmt.Errorf("all mirrors failed, last error: %w", lastErr)
}

// downloadToFileURL performs one mirror attempt. A non-200 status, a truncated
// body, or an empty body all count as failures so the caller tries the next URL.
func (b *Bootstrap) downloadToFileURL(ctx context.Context, url, partPath, destPath, description string) error {
	attemptCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), dataFileMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(destPath), err)
	}
	if err := streamToFile(partPath, resp.Body, resp.ContentLength, description); err != nil {
		return err
	}
	if err := replaceFile(partPath, destPath); err != nil {
		return fmt.Errorf("install %s: %w", filepath.Base(destPath), err)
	}
	return nil
}

// streamToFile writes body to path, rendering a progress bar when the response
// reports a length, and rejects short or empty responses.
func streamToFile(path string, body io.Reader, contentLength int64, description string) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, dataFileMode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()

	var dst io.Writer = file
	if contentLength > 0 {
		bar := makeBootstrapDownloadBar(description, contentLength)
		defer finishBootstrapDownloadBar(bar)
		dst = io.MultiWriter(file, bar)
	}

	bufPtr := copyBufPool.Get().(*[]byte)
	written, err := io.CopyBuffer(dst, body, *bufPtr)
	copyBufPool.Put(bufPtr)
	if err != nil {
		return err
	}
	if written == 0 {
		return fmt.Errorf("empty response body")
	}
	if contentLength > 0 && written != contentLength {
		return fmt.Errorf("incomplete download: got %d bytes, want %d", written, contentLength)
	}
	return file.Sync()
}
