package lifecycle

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gluestick-sh/core/engine/internal/runtime"
)

// NewHookScriptEnv builds the environment (paths, arch, hook lines) passed to Scoop
// pre_install/post_install hook scripts for a package install.
func NewHookScriptEnv(e *runtime.Services,
	installDir, downloadName, version, pkgRef, pkgName, installArch string,
	hooks []string,
) HookScriptEnv {
	root := e.Config.RootDir
	return HookScriptEnv{
		InstallDir:   installDir,
		DownloadName: downloadName,
		Version:      version,
		App:          pkgName,
		Bucket:       runtime.PackageBucketName(pkgRef),
		BucketsDir:   filepath.Join(root, "buckets"),
		PersistDir:   filepath.Join(root, "persist", pkgName),
		GlueRoot:     root,
		Arch:         installArch,
		Hooks:        hooks,
	}
}

// ResolveHookHelpers ensures the external helpers a hook needs are present, returning the
// resolved 7z.exe and dark/wix paths (empty when the hooks do not require them).
// timer, when non-nil, wraps the bootstrap downloads so the caller can attribute their
// duration (install passes its per-phase timings runner; uninstall passes nil).
func ResolveHookHelpers(e *runtime.Services, ctx context.Context, hooks []string, pkgName string, timer func(func() error) error) (sevenZip, dark string, err error) {
	if hookHelpersNeed7z(hooks) {
		if err := Ensure7Zip(e, ctx, pkgName, timer); err != nil {
			return "", "", fmt.Errorf("ensure 7z for hook: %w", err)
		}
		sevenZip = e.Extractor.SevenZipPath()
		if sevenZip == "" {
			return "", "", fmt.Errorf("hook requires 7z.exe (install 7zip first)")
		}
	}
	dark, err = EnsureDark(e, ctx, hooks, pkgName, timer)
	if err != nil {
		return "", "", err
	}
	return sevenZip, dark, nil
}
