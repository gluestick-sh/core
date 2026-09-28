package downloader

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gluestick-sh/core/config"
	"github.com/gluestick-sh/core/humanize"
	"github.com/gluestick-sh/core/verbose"
)

// partialState tracks the resumable on-disk state of a single download task: the
// .part file holding already-transferred bytes and its .meta.json sidecar.
type partialState struct {
	partPath string
	metaPath string
	taskURL  string   // canonical task URL the partial must belong to
	meta     partialMeta
	offset   int64 // bytes already present in the .part file
}

// complete reports whether the partial file already holds the whole artifact.
func (s *partialState) complete() bool {
	return s.meta.TotalSize > 0 && s.offset >= s.meta.TotalSize
}

// reset drops the partial files and returns to a clean state for the task URL.
func (s *partialState) reset() {
	removePartial(s.partPath, s.metaPath)
	s.meta = partialMeta{URL: s.taskURL}
	s.offset = 0
}

// commit records a completed download so a later run knows which source URL the
// bytes came from and how large the artifact is. usedURL is empty when no
// concrete source URL is known, in which case the sidecar is left untouched.
func (s *partialState) commit(usedURL string, totalSize int64) {
	if usedURL == "" {
		return
	}
	s.meta.URL = s.taskURL
	s.meta.SourceURL = usedURL
	s.meta.TotalSize = totalSize
	_ = savePartialMeta(s.metaPath, s.meta)
}

// ensurePartialDir creates the directory holding .part files.
func (d *Downloader) ensurePartialDir(partPath string) error {
	if err := os.MkdirAll(filepath.Dir(partPath), 0755); err != nil {
		return fmt.Errorf("create partial dir: %w", err)
	}
	return nil
}

// loadPartialState reconciles the partial files on disk with task: files left
// behind by a different URL are discarded, and offset is set to the number of
// bytes already downloaded. Partials written before source_url was recorded
// cannot be resumed across mirror vs direct hosts, so they are either discarded
// or pinned to the task URL.
func (d *Downloader) loadPartialState(task Task, partPath, metaPath string) (partialState, error) {
	st := partialState{
		partPath: partPath,
		metaPath: metaPath,
		taskURL:  task.URL,
		meta:     partialMeta{URL: task.URL},
	}
	if existing, err := loadPartialMeta(metaPath); err == nil {
		if existing.URL == task.URL {
			st.meta = existing
		} else {
			st.reset()
		}
	}
	if info, err := os.Stat(partPath); err == nil {
		st.offset = info.Size()
	}

	if st.offset > 0 && st.meta.SourceURL == "" {
		if len(d.ghProxies) > 0 && config.IsGitHubURL(task.URL) {
			verbose.Fprintf("Discarding partial without source_url (%s)\n", humanize.FormatBytes(st.offset))
			st.reset()
		} else {
			st.meta.SourceURL = task.URL
		}
	}
	return st, nil
}