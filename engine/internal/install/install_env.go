package install

import (
	"strings"

	"github.com/gluestick-sh/core/envset"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/verbose"
)

// ExpandManifestEnvValue substitutes Scoop $dir in env values.
func ExpandManifestEnvValue(value, installDir string) string {
	return strings.ReplaceAll(value, "$dir", installDir)
}

// ShimEnvForPackage returns expanded env vars for package shims.
func ShimEnvForPackage(m *manifest.Manifest, installDir, installArch string) map[string]string {
	raw := m.EnvVarsForInstall(installArch)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = ExpandManifestEnvValue(v, installDir)
	}
	return out
}

// ApplyPackageEnvSet writes manifest env_set to the user environment.
func ApplyPackageEnvSet(m *manifest.Manifest, installDir, installArch string) error {
	raw := m.EnvSetForInstall(installArch)
	if len(raw) == 0 {
		return nil
	}
	expanded := make(map[string]string, len(raw))
	for k, v := range raw {
		expanded[k] = ExpandManifestEnvValue(v, installDir)
	}
	if err := envset.ApplyUser(expanded); err != nil {
		return err
	}
	for name := range expanded {
		verbose.Progressf("    %s env_set %s\n", verbose.SuccessMark(), name)
	}
	return nil
}

