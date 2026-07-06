// Package override applies per-package manifest JSON and download-URL overrides
// on top of the bucket manifest to produce the effective manifest for install.
package override

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gluestick-sh/core/config"
	"github.com/gluestick-sh/core/engine/internal/catalog"
	"github.com/gluestick-sh/core/engine/internal/runtime"
	etypes "github.com/gluestick-sh/core/engine/types"
	"github.com/gluestick-sh/core/manifest"
)

type manifestOverrideState struct {
	effectiveM *manifest.Manifest
	jsonActive bool
	jsonStale  bool
	urlActive  bool
}

// ManifestOverrideState describes active manifest overrides for inspect/plan UIs.
type ManifestOverrideState struct {
	EffectiveM *manifest.Manifest
	JSONActive bool
	JSONStale  bool
	URLActive  bool
}

func toManifestOverrideState(s manifestOverrideState) ManifestOverrideState {
	return ManifestOverrideState{
		EffectiveM: s.effectiveM,
		JSONActive: s.jsonActive,
		JSONStale:  s.jsonStale,
		URLActive:  s.urlActive,
	}
}

// ResolveManifestOverrides returns the effective manifest and override flags.
func ResolveManifestOverrides(e *runtime.Engine, pkgRef, manifestPath string, bucketM *manifest.Manifest, installArch string, req *etypes.InstallRequest) (ManifestOverrideState, error) {
	state, err := resolveManifestOverrides(e, pkgRef, manifestPath, bucketM, installArch, req)
	if err != nil {
		return ManifestOverrideState{}, err
	}
	return toManifestOverrideState(state), nil
}

func manifestJSONOverride(e *runtime.Engine, pkgRef string) (item config.ManifestJSONOverride, ok bool) {
	if e == nil || e.Config == nil || e.Config.RootDir == "" {
		return config.ManifestJSONOverride{}, false
	}
	overrides, err := config.ReadConfigManifestJSONOverrides(e.Config.RootDir)
	if err != nil {
		return config.ManifestJSONOverride{}, false
	}
	item, ok = overrides[config.NormalizeManifestOverrideKey(pkgRef)]
	if !ok || strings.TrimSpace(item.JSON) == "" {
		return config.ManifestJSONOverride{}, false
	}
	return item, true
}

func resolveManifestOverrides(e *runtime.Engine, pkgRef, manifestPath string, bucketM *manifest.Manifest, installArch string, req *etypes.InstallRequest) (manifestOverrideState, error) {
	state := manifestOverrideState{effectiveM: bucketM}
	if bucketM == nil {
		return state, nil
	}

	if item, ok := manifestJSONOverride(e, pkgRef); ok {
		currentHash, err := manifest.HashFile(manifestPath)
		if err != nil || currentHash != item.BaseHash {
			state.jsonStale = true
		} else {
			parsed, err := manifest.Parse(strings.NewReader(item.JSON))
			if err != nil {
				return state, fmt.Errorf("parse manifest override: %w", err)
			}
			state.effectiveM = parsed
			state.jsonActive = true
		}
	}

	if state.jsonActive {
		_, _, state.urlActive = manifestDownloadOverride(e, pkgRef)
		return state, nil
	}

	overridden, err := applyManifestDownloadOverrides(e, pkgRef, state.effectiveM, installArch, req)
	if err != nil {
		return state, err
	}
	state.effectiveM = overridden
	_, _, state.urlActive = manifestDownloadOverride(e, pkgRef)
	if req != nil && len(req.DownloadURLOverrides) > 0 {
		state.urlActive = true
	}
	return state, nil
}

// ApplyManifestOverrides returns the effective manifest after config/request overrides.
func ApplyManifestOverrides(e *runtime.Engine, pkgRef, manifestPath string, m *manifest.Manifest, installArch string, req *etypes.InstallRequest) (*manifest.Manifest, error) {
	state, err := resolveManifestOverrides(e, pkgRef, manifestPath, m, installArch, req)
	if err != nil {
		return nil, err
	}
	return state.effectiveM, nil
}

func manifestDownloadOverride(e *runtime.Engine, pkgRef string) (urls, hashes []string, ok bool) {
	if e == nil || e.Config == nil || e.Config.RootDir == "" {
		return nil, nil, false
	}
	overrides, err := config.ReadConfigManifestDownloadOverrides(e.Config.RootDir)
	if err != nil {
		return nil, nil, false
	}
	item, ok := overrides[config.NormalizeManifestOverrideKey(pkgRef)]
	if !ok || len(item.URLs) == 0 {
		return nil, nil, false
	}
	return append([]string(nil), item.URLs...), append([]string(nil), item.Hashes...), true
}

func applyManifestDownloadOverrides(e *runtime.Engine, pkgRef string, m *manifest.Manifest, installArch string, req *etypes.InstallRequest) (*manifest.Manifest, error) {
	if m == nil {
		return nil, nil
	}
	urls, hashes, ok := manifestDownloadOverride(e, pkgRef)
	if req != nil && len(req.DownloadURLOverrides) > 0 {
		urls = append([]string(nil), req.DownloadURLOverrides...)
		hashes = append([]string(nil), req.DownloadHashOverrides...)
		ok = true
	}
	if !ok {
		return m, nil
	}
	return manifest.ApplyDownloadOverride(m, installArch, urls, hashes)
}

// SetManifestDownloadOverride persists a per-package download URL override.
func SetManifestDownloadOverride(e *runtime.Engine, pkgRef string, urls, hashes []string) error {
	if e == nil || e.Config == nil {
		return nil
	}
	return config.SetConfigManifestDownloadOverride(e.Config.RootDir, pkgRef, urls, hashes)
}

// ClearManifestDownloadOverride removes a per-package download URL override.
func ClearManifestDownloadOverride(e *runtime.Engine, pkgRef string) error {
	return SetManifestDownloadOverride(e, pkgRef, nil, nil)
}

// SetManifestJSONOverride persists a per-package manifest JSON override.
func SetManifestJSONOverride(e *runtime.Engine, pkgRef, manifestPath, jsonText string) error {
	if e == nil || e.Config == nil {
		return nil
	}
	jsonText = strings.TrimSpace(jsonText)
	if jsonText == "" {
		return config.SetConfigManifestJSONOverride(e.Config.RootDir, pkgRef, "", "")
	}
	bucketM, err := manifest.ParseFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read bucket manifest: %w", err)
	}
	userM, err := manifest.Parse(strings.NewReader(jsonText))
	if err != nil {
		return fmt.Errorf("invalid manifest JSON: %w", err)
	}
	if manifestsEquivalent(bucketM, userM) {
		return config.SetConfigManifestJSONOverride(e.Config.RootDir, pkgRef, "", "")
	}
	baseHash, err := manifest.HashFile(manifestPath)
	if err != nil {
		return fmt.Errorf("hash bucket manifest: %w", err)
	}
	return config.SetConfigManifestJSONOverride(e.Config.RootDir, pkgRef, jsonText, baseHash)
}

func manifestsEquivalent(a, b *manifest.Manifest) bool {
	if a == nil || b == nil {
		return a == b
	}
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

// ClearManifestJSONOverride removes a per-package manifest JSON override.
func ClearManifestJSONOverride(e *runtime.Engine, pkgRef string) error {
	if e == nil || e.Config == nil {
		return nil
	}
	return config.SetConfigManifestJSONOverride(e.Config.RootDir, pkgRef, "", "")
}

// SetManifestJSONOverrideForRef resolves pkgRef and saves a manifest JSON override.
func SetManifestJSONOverrideForRef(e *runtime.Engine, ctx context.Context, pkgRef, jsonText string) error {
	resolved, err := catalog.ResolveInstallRef(e, ctx, pkgRef)
	if err != nil {
		return err
	}
	lookupRef := runtime.ManifestLookupRef(resolved)
	if err := catalog.EnsureBucketForInstall(e, ctx, lookupRef, nil); err != nil {
		return err
	}
	manifestPath, _, err := e.BucketRegistry.GetManifestPath(lookupRef)
	if err != nil {
		return fmt.Errorf("find manifest: %w", err)
	}
	return SetManifestJSONOverride(e, resolved, manifestPath, jsonText)
}
