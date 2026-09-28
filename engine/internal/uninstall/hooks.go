package uninstall

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/engine/internal/lifecycle"
	"github.com/gluestick-sh/core/engine/internal/runtime"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/verbose"
)

func loadUninstallManifestContext(e *runtime.Services, installDir, pkgName string) (*manifest.Manifest, string, string) {
	if rec, err := apps.LoadInstallRecord(installDir); err == nil && rec.Manifest != nil {
		bucket := rec.Bucket
		if bucket == "" {
			bucket = "main"
		}
		arch := rec.Manifest.SelectedArchitecture()
		return rec.Manifest, bucket, arch
	}
	m, err := lifecycle.LoadManifestForReset(e, installDir, pkgName)
	if err != nil || m == nil {
		return nil, "main", ""
	}
	return m, "main", m.SelectedArchitecture()
}

// runManifestUninstallHooks runs Scoop pre_uninstall and uninstaller.script before file removal.
// Service teardown hooks must run before process checks and deleting installDir (Scoop order).
func runManifestUninstallHooks(e *runtime.Services,
	ctx context.Context,
	bucketName, pkgName, installDir, targetVer, installArch string,
	m *manifest.Manifest,
) error {
	if m == nil {
		return nil
	}
	downloadName := lifecycle.DownloadNameFromManifest(m)
	if bucketName == "" {
		bucketName = "main"
	}

	preHooks := m.PreUninstallHooksForInstall(installArch)
	uninstallerHooks := m.UninstallerScriptHooksForInstall(installArch)
	if len(preHooks) == 0 && len(uninstallerHooks) == 0 {
		return nil
	}

	allHooks := append(append([]string{}, preHooks...), uninstallerHooks...)
	sevenZip, dark, err := lifecycle.ResolveHookHelpers(e, ctx, allHooks, pkgName, nil)
	if err != nil {
		return fmt.Errorf("uninstall hooks: %w", err)
	}

	run := func(kind string, hooks []string, fn func(lifecycle.HookScriptEnv) error) error {
		if len(hooks) == 0 {
			return nil
		}
		verbose.Progressf("  Running %s...\n", kind)
		env := lifecycle.NewHookScriptEnv(e, installDir, downloadName, targetVer, bucketName+"/"+pkgName, pkgName, installArch, hooks)
		env.SevenZip = sevenZip
		env.Dark = dark
		return fn(env)
	}

	if err := run("pre_uninstall", preHooks, lifecycle.RunPreUninstallHooks); err != nil {
		return err
	}
	if err := run("uninstaller.script", uninstallerHooks, lifecycle.RunUninstallerHooks); err != nil {
		return err
	}
	if uninstallHooksNeedSettleTime(allHooks) {
		time.Sleep(1500 * time.Millisecond)
	}
	return nil
}

func runManifestPostUninstallHooks(e *runtime.Services,
	ctx context.Context,
	bucketName, pkgName, installDir, targetVer, installArch string,
	m *manifest.Manifest,
) error {
	if m == nil {
		return nil
	}
	hooks := m.PostUninstallHooksForInstall(installArch)
	if len(hooks) == 0 {
		return nil
	}
	verbose.Progressf("  Running post_uninstall...\n")
	downloadName := lifecycle.DownloadNameFromManifest(m)
	if bucketName == "" {
		bucketName = "main"
	}
	sevenZip, dark, err := lifecycle.ResolveHookHelpers(e, ctx, hooks, pkgName, nil)
	if err != nil {
		return fmt.Errorf("post_uninstall: %w", err)
	}
	env := lifecycle.NewHookScriptEnv(e, installDir, downloadName, targetVer, bucketName+"/"+pkgName, pkgName, installArch, hooks)
	env.SevenZip = sevenZip
	env.Dark = dark
	return lifecycle.RunPostUninstallHooks(env)
}

func uninstallHooksNeedSettleTime(hooks []string) bool {
	body := strings.ToLower(strings.Join(hooks, "\n"))
	return strings.Contains(body, "stop-service") ||
		strings.Contains(body, "stop-process") ||
		strings.Contains(body, "uninstall-system-daemon") ||
		strings.Contains(body, "sc.exe stop")
}
