package engine

import (
	"context"

	"github.com/gluestick-sh/core/engine/internal/override"
)

// SetManifestDownloadOverride persists a per-package download URL override.
// Prefer SetManifestDownloadOverrideForRef so the override is tied to the bucket manifest hash.
func (e *Engine) SetManifestDownloadOverride(pkgRef string, urls, hashes []string) error {
	return override.SetManifestDownloadOverride(e.Services, pkgRef, urls, hashes, "")
}

// SetManifestDownloadOverrideForRef resolves pkgRef and saves a URL override tied to the bucket manifest hash.
func (e *Engine) SetManifestDownloadOverrideForRef(ctx context.Context, pkgRef string, urls, hashes []string) error {
	return override.SetManifestDownloadOverrideForRef(e.Services, ctx, pkgRef, urls, hashes)
}

// ClearManifestDownloadOverride removes a per-package download URL override.
func (e *Engine) ClearManifestDownloadOverride(pkgRef string) error {
	return override.ClearManifestDownloadOverride(e.Services, pkgRef)
}

// PruneStaleManifestDownloadOverrides removes download URL overrides that no longer
// match the current bucket manifests (typically after a bucket update).
func (e *Engine) PruneStaleManifestDownloadOverrides() ([]string, error) {
	return override.PruneStaleManifestDownloadOverrides(e.Services)
}

// SetManifestJSONOverride persists a per-package manifest JSON override.
func (e *Engine) SetManifestJSONOverride(pkgRef, manifestPath, jsonText string) error {
	return override.SetManifestJSONOverride(e.Services, pkgRef, manifestPath, jsonText)
}

// ClearManifestJSONOverride removes a per-package manifest JSON override.
func (e *Engine) ClearManifestJSONOverride(pkgRef string) error {
	return override.ClearManifestJSONOverride(e.Services, pkgRef)
}

// SetManifestJSONOverrideForRef resolves pkgRef and saves a manifest JSON override.
func (e *Engine) SetManifestJSONOverrideForRef(ctx context.Context, pkgRef, jsonText string) error {
	return override.SetManifestJSONOverrideForRef(e.Services, ctx, pkgRef, jsonText)
}
