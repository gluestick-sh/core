package install

import (
	"context"
	"path/filepath"

	"github.com/gluestick-sh/core/engine/internal/lifecycle"
	"github.com/gluestick-sh/core/engine/internal/runtime"
	"github.com/gluestick-sh/core/manifest"
)

func runManifestPostInstall(e *runtime.Services,
	ctx context.Context,
	pkgRef, pkgName, installDir, downloadName, version, installArch string,
	m *manifest.Manifest,
	timings *installPhaseTimings,
) error {
	hooks := m.PostInstallHooksForInstall(installArch)
	if len(hooks) == 0 {
		return nil
	}
	hooks = patchInstallHookPaths(installDir, hooks)
	persistDir := filepath.Join(e.Config.RootDir, "persist", pkgName)
	if err := lifecycle.PrepareInstallDirForPostInstallHooks(installDir, persistDir, m.PersistEntriesForInstall(installArch)); err != nil {
		return err
	}
	sevenZip, dark, err := lifecycle.ResolveHookHelpers(e, ctx, hooks, pkgName, timings.runBootstrap)
	if err != nil {
		return err
	}
	env := lifecycle.NewHookScriptEnv(e, installDir, downloadName, version, pkgRef, pkgName, installArch, hooks)
	env.SevenZip = sevenZip
	env.Dark = dark
	return lifecycle.RunPostInstallHooks(env)
}
