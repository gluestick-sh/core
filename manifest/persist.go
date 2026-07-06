package manifest

import (
	"path/filepath"
	"strings"
)

// PersistEntry maps an install-dir path to a persist-dir path (Scoop persist).
type PersistEntry struct {
	Install string
	Data    string
}

// InstallName is the path relative to the app version directory.
func (e PersistEntry) InstallName() string {
	if e.Install == "" {
		return e.Data
	}
	return e.Install
}

// DataName is the path relative to persist/<app>.
func (e PersistEntry) DataName() string {
	if e.Data == "" {
		return e.Install
	}
	return e.Data
}

// LooksLikeFile reports Scoop-style file persist paths (e.g. config.xml).
// Directory persist entries (plugins, User Data) have no dotted base name.
func (e PersistEntry) LooksLikeFile() bool {
	return strings.Contains(filepath.Base(e.InstallName()), ".")
}

// PersistEntries returns manifest persist paths.
func (m *Manifest) PersistEntries() []PersistEntry {
	return m.PersistEntriesForInstall("")
}

// PersistEntriesForInstall returns persist for override architecture when set.
func (m *Manifest) PersistEntriesForInstall(override string) []PersistEntry {
	if m == nil {
		return nil
	}
	if block := m.archBlockForInstall(override); block != nil {
		if entries := parsePersistEntries(block["persist"]); len(entries) > 0 {
			return entries
		}
	}
	return parsePersistEntries(m.Persist)
}

func parsePersistEntries(raw interface{}) []PersistEntry {
	switch v := raw.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []PersistEntry{{Install: v, Data: v}}
	case []interface{}:
		var out []PersistEntry
		for _, item := range v {
			switch entry := item.(type) {
			case string:
				if entry != "" {
					out = append(out, PersistEntry{Install: entry, Data: entry})
				}
			case []interface{}:
				if len(entry) == 0 {
					continue
				}
				install, _ := entry[0].(string)
				data := install
				if len(entry) > 1 {
					if d, ok := entry[1].(string); ok && d != "" {
						data = d
					}
				}
				if install != "" {
					out = append(out, PersistEntry{Install: install, Data: data})
				}
			}
		}
		return out
	default:
		return nil
	}
}
