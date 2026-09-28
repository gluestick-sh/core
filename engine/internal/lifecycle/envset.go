package lifecycle

import (
	"github.com/gluestick-sh/core/envset"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/verbose"
)

// RemovePackageEnvSet deletes manifest env_set keys from the user environment.
// Used when the last version of a package is uninstalled.
func RemovePackageEnvSet(m *manifest.Manifest, installArch string) error {
	raw := m.EnvSetForInstall(installArch)
	if len(raw) == 0 {
		return nil
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	if err := envset.RemoveUser(names); err != nil {
		return err
	}
	for _, name := range names {
		verbose.Progressf("    %s removed env_set %s\n", verbose.SuccessMark(), name)
	}
	return nil
}
