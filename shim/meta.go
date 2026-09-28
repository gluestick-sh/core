package shim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// HasShimsForPackage reports whether shims-meta still references the package.
func HasShimsForPackage(shimsMetaDir, pkgName string) bool {
	entries, err := os.ReadDir(shimsMetaDir)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(shimsMetaDir, ent.Name()))
		if err != nil {
			continue
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if BelongsToPackage(cfg.Path, pkgName) {
			return true
		}
	}
	return false
}

// BelongsToPackage reports whether a shim target path points into pkgName's apps directory.
func BelongsToPackage(cfgPath, pkgName string) bool {
	return packageNameFromAppsPath(cfgPath) == pkgName
}

// OwnedByOtherPackage reports whether the named shim already belongs to a different package.
// A non-empty owner with conflict=true means the shim must not be re-created by pkgName.
func OwnedByOtherPackage(shimsMetaDir, shimName, pkgName string) (owner string, conflict bool) {
	owner = shimOwnerPackage(shimsMetaDir, shimName)
	if owner == "" || owner == pkgName {
		return "", false
	}
	return owner, true
}

func shimOwnerPackage(shimsMetaDir, shimName string) string {
	configPath := filepath.Join(shimsMetaDir, shimName+".json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return packageNameFromAppsPath(cfg.Path)
}

// packageNameFromAppsPath extracts the package name from a glue apps path
// (…/apps/<pkg>/…); empty when the path is not inside an apps directory.
func packageNameFromAppsPath(cfgPath string) string {
	normalized := filepath.ToSlash(cfgPath)
	const marker = "/apps/"
	idx := strings.Index(normalized, marker)
	if idx < 0 {
		return ""
	}
	rest := normalized[idx+len(marker):]
	if end := strings.Index(rest, "/"); end >= 0 {
		return rest[:end]
	}
	return ""
}
