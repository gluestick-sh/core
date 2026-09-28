// Package bootstrap downloads runtime tools into ~/.glue/bin (MinGit, 7-Zip, WiX, innounp).
//
// Downloads stream to disk through a .part file and are renamed into place only
// once the whole body arrived, so an interrupted or cancelled run never leaves a
// truncated file behind for the "already installed" checks to trust.
package bootstrap

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gluestick-sh/core/config"
)

const (
	// downloadTimeout bounds a single mirror attempt, not the whole bootstrap.
	downloadTimeout = 5 * time.Minute
	// copyBufferSize is the HTTP body copy buffer (matches downloader.copyBufferSize).
	copyBufferSize = 128 * 1024
	// userAgent identifies glue to hosts that reject the default Go agent.
	userAgent = "Glue/1.0"
	// removeRetries and removeRetryDelay tolerate transient handles left behind by
	// antivirus scanners and the Windows search indexer on freshly written files.
	removeRetries    = 5
	removeRetryDelay = 50 * time.Millisecond
	// execFileMode is used for installed executables, dataFileMode for payloads.
	execFileMode = 0755
	dataFileMode = 0644
)

var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, copyBufferSize)
		return &buf
	},
}

// Bootstrap downloads essential tools that glue needs
type Bootstrap struct {
	rootDir   string
	ghProxies []string     // GitHub proxy URLs to try
	client    *http.Client // shared so mirror retries reuse the same connections
}

// NewBootstrap creates a new bootstrap manager for the given glue root.
// Empty rootDir uses ~/.glue.
func NewBootstrap(rootDir string) *Bootstrap {
	if rootDir == "" {
		home, _ := os.UserHomeDir()
		rootDir = filepath.Join(home, ".glue")
	}
	return &Bootstrap{
		rootDir:   rootDir,
		ghProxies: config.LoadProxies(rootDir),
		client:    &http.Client{Transport: newBootstrapTransport()},
	}
}

// SetGitHubProxies replaces GitHub mirror prefixes used for bootstrap downloads.
func (b *Bootstrap) SetGitHubProxies(proxies []string) {
	if len(proxies) == 0 {
		b.ghProxies = nil
		return
	}
	b.ghProxies = append([]string(nil), proxies...)
}

// newBootstrapTransport tunes the shared client for a few large, occasionally
// retried downloads: proxy support, a TLS floor, and idle connection reuse
// across mirrors.
func newBootstrapTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
}

func (b *Bootstrap) binDir() string {
	return filepath.Join(b.rootDir, "bin")
}

// withToolLock runs fn under the tool's per-root lock. The context is normalized
// and checked before the lock is taken so a cancelled caller never queues behind
// a long download.
func withToolLock(ctx context.Context, mu *sync.Mutex, fn func(context.Context) (string, error)) (string, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	mu.Lock()
	defer mu.Unlock()
	return fn(ctx)
}

// shortenURL truncates a URL for compact progress lines.
func shortenURL(url string) string {
	const max = 60
	if len(url) <= max {
		return url
	}
	return url[:max-3] + "..."
}
