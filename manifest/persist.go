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
// Paths whose basename contains a dot are not always files: Tor Browser persists
// profile.default as a directory. Prefer known file extensions; otherwise treat
// as a directory so missing paths are created on install.
func (e PersistEntry) LooksLikeFile() bool {
	base := filepath.Base(e.InstallName())
	if !strings.Contains(base, ".") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".ini", ".xml", ".json", ".txt", ".conf", ".cfg", ".yml", ".yaml", ".toml",
		".js", ".css", ".html", ".htm", ".log", ".dat", ".db", ".sqlite", ".sqlite3",
		".bat", ".cmd", ".ps1", ".reg", ".properties", ".config", ".vnc":
		return true
	default:
		return false
	}
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

// parsePersistEntries normalizes a Scoop persist field into entries. The field
// may be a path or an array mixing paths and [install, data] pairs.
func parsePersistEntries(raw interface{}) []PersistEntry {
	switch v := raw.(type) {
	case string:
		if entry, ok := persistEntryFromPath(v); ok {
			return []PersistEntry{entry}
		}
		return nil
	case []interface{}:
		return persistEntriesFromArray(v)
	default:
		return nil
	}
}

// persistEntriesFromArray converts persist array elements, dropping elements
// that carry no install path.
func persistEntriesFromArray(items []interface{}) []PersistEntry {
	var out []PersistEntry
	for _, item := range items {
		switch element := item.(type) {
		case string:
			if entry, ok := persistEntryFromPath(element); ok {
				out = append(out, entry)
			}
		case []interface{}:
			if entry, ok := persistEntryFromPair(element); ok {
				out = append(out, entry)
			}
		}
	}
	return out
}

// persistEntryFromPath converts a single persist path. An empty path yields no
// entry; otherwise the path is both the install and the data name.
func persistEntryFromPath(path string) (PersistEntry, bool) {
	if path == "" {
		return PersistEntry{}, false
	}
	return PersistEntry{Install: path, Data: path}, true
}

// persistEntryFromPair converts an [install, data] pair. A missing, non-string,
// or empty data path falls back to the install path.
func persistEntryFromPair(pair []interface{}) (PersistEntry, bool) {
	if len(pair) == 0 {
		return PersistEntry{}, false
	}
	install, _ := pair[0].(string)
	if install == "" {
		return PersistEntry{}, false
	}
	data := install
	if len(pair) > 1 {
		if d, ok := pair[1].(string); ok && d != "" {
			data = d
		}
	}
	return PersistEntry{Install: install, Data: data}, true
}
