package shim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/gluestick-sh/core/verbose"
)

// RemoveShimsForPackage removes every shim whose target points into pkgName's
// apps directory and returns the removed shim names.
func RemoveShimsForPackage(mgr *Manager, shimsMetaDir, appsDir, pkgName string) ([]string, error) {
	entries, err := os.ReadDir(shimsMetaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var removed []string
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}

		configPath := filepath.Join(shimsMetaDir, ent.Name())
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		if BelongsToPackage(cfg.Path, pkgName) {
			shimName := strings.TrimSuffix(ent.Name(), ".json")
			if err := mgr.Remove(shimName); err != nil {
				verbose.Progressf("    %s Failed to remove shim %s: %v\n", verbose.FailedMark(), shimName, err)
				continue
			}
			removed = append(removed, shimName)
		}
	}

	return removed, nil
}
