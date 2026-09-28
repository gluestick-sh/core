package lifecycle

import (
	"fmt"
	"path/filepath"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/engine/internal/runtime"
	"github.com/gluestick-sh/core/manifest"
)

// LoadManifestForReset returns the manifest for a reset, preferring the saved install
// record and falling back to the bucket registry.
func LoadManifestForReset(e *runtime.Services, installDir, pkgName string) (*manifest.Manifest, error) {
	if rec, err := apps.LoadInstallRecord(installDir); err == nil && rec.Manifest != nil {
		return rec.Manifest, nil
	}
	_, m, err := e.BucketRegistry.GetManifestPath(pkgName)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	return m, nil
}

// DownloadNameFromManifest extracts the download filename from a manifest.
// This function parses the first URL in the manifest and returns the
// local filename that should be used for the download.
// Parameters:
//   - m: Package manifest
//
// Returns the download filename, or empty string if no URLs are defined.
func DownloadNameFromManifest(m *manifest.Manifest) string {
	urls := m.GetURLs()
	if len(urls) == 0 {
		return ""
	}
	return downloadFilename(urls[0])
}

// downloadFilename returns the archive filename from a download URL.
// Handles Scoop's URL fragment syntax: "url#/newname.ext"
// Parameters:
//   - url: Download URL (may contain fragment)
//
// Returns the local filename for the download.
func downloadFilename(url string) string {
	parsed, err := manifest.ParseURL(url)
	if err != nil {
		return filepath.Base(url)
	}
	return parsed.LocalName
}
