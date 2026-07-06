package engine

import (
	"context"

	"github.com/gluestick-sh/core/engine/internal/override"
)

// SetManifestDownloadOverride persists a per-package download URL override.
func (e *Engine) SetManifestDownloadOverride(pkgRef string, urls, hashes []string) error {
	return override.SetManifestDownloadOverride(e.Engine, pkgRef, urls, hashes)
}

// ClearManifestDownloadOverride removes a per-package download URL override.
func (e *Engine) ClearManifestDownloadOverride(pkgRef string) error {
	return override.ClearManifestDownloadOverride(e.Engine, pkgRef)
}

// SetManifestJSONOverride persists a per-package manifest JSON override.
func (e *Engine) SetManifestJSONOverride(pkgRef, manifestPath, jsonText string) error {
	return override.SetManifestJSONOverride(e.Engine, pkgRef, manifestPath, jsonText)
}

// ClearManifestJSONOverride removes a per-package manifest JSON override.
func (e *Engine) ClearManifestJSONOverride(pkgRef string) error {
	return override.ClearManifestJSONOverride(e.Engine, pkgRef)
}

// SetManifestJSONOverrideForRef resolves pkgRef and saves a manifest JSON override.
func (e *Engine) SetManifestJSONOverrideForRef(ctx context.Context, pkgRef, jsonText string) error {
	return override.SetManifestJSONOverrideForRef(e.Engine, ctx, pkgRef, jsonText)
}
