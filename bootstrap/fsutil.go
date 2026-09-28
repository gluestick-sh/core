package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gluestick-sh/core/verbose"
)

// installFile copies src over dst through a temporary file, so a failed copy
// never replaces a working binary, and marks the result executable.
func installFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), dataFileMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer removeWithRetry(tmpPath)

	if err := copyStream(tmp, src); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, execFileMode); err != nil {
		return err
	}
	return replaceFile(tmpPath, dst)
}

// copyFile copies src to dst, streaming instead of loading the whole file into
// memory. Unlike installFile it leaves a partially written dst behind on
// failure, so it is only used for payload files extracted from an archive.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), dataFileMode); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, dataFileMode)
	if err != nil {
		return err
	}
	return copyStream(out, src)
}

// copyStream writes src into the open file dst and closes dst.
func copyStream(dst *os.File, src string) error {
	in, err := os.Open(src)
	if err != nil {
		dst.Close()
		return err
	}
	defer in.Close()

	bufPtr := copyBufPool.Get().(*[]byte)
	_, copyErr := io.CopyBuffer(dst, in, *bufPtr)
	copyBufPool.Put(bufPtr)
	if copyErr != nil {
		dst.Close()
		return copyErr
	}
	return dst.Close()
}

// replaceFile renames src over dst. Windows refuses to rename onto an existing
// file, so the destination is removed first; removeWithRetry absorbs the short
// window where a scanner still holds the old file open.
func replaceFile(src, dst string) error {
	removeWithRetry(dst)
	return os.Rename(src, dst)
}

// replaceDir swaps staging into place, replacing any previous installation.
func replaceDir(staging, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), dataFileMode); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("remove old %s: %w", filepath.Base(dst), err)
	}
	if err := os.Rename(staging, dst); err != nil {
		return fmt.Errorf("install %s: %w", filepath.Base(dst), err)
	}
	return nil
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// findExtracted locates name at the root of dir or one level below it, which is
// how both the 7-Zip Extra package and the SFX installer lay out their payload.
func findExtracted(dir, name string) string {
	for _, pattern := range []string{
		filepath.Join(dir, name),
		filepath.Join(dir, "*", name),
	} {
		if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

// removeWithRetry deletes path, retrying briefly: antivirus and the search
// indexer can hold a transient handle on freshly written files.
func removeWithRetry(path string) {
	for i := 0; i < removeRetries; i++ {
		err := os.Remove(path)
		if err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(removeRetryDelay)
	}
}

// ensureArchive downloads name into bin unless a usable copy is already there
// and reports its path. Callers own removing the archive once extraction ends.
func (b *Bootstrap) ensureArchive(ctx context.Context, name string, urls []string, description string) (string, error) {
	path := filepath.Join(b.binDir(), name)
	if fileExists(path) {
		return path, nil
	}
	verbose.Progressf("Bootstrapping: downloading %s...\n", description)
	if err := b.downloadToFile(ctx, urls, path, description); err != nil {
		return "", fmt.Errorf("download %s: %w", description, err)
	}
	verbose.Progressf("  Downloaded %s\n", description)
	return path, nil
}
