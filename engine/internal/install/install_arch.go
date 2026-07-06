package install

import (
	"strings"

	etypes "github.com/gluestick-sh/core/engine/types"
	"github.com/gluestick-sh/core/manifest"
)

func installArchitecture(req *etypes.InstallRequest, m *manifest.Manifest) string {
	if req == nil || m == nil {
		return ""
	}
	if req.Options != nil {
		if arch := strings.TrimSpace(req.Options["architecture"]); arch != "" {
			return m.ArchitectureForInstall(arch)
		}
	}
	return m.SelectedArchitecture()
}
