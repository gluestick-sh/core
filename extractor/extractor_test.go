package extractor

import "testing"

func TestExtractStages(t *testing.T) {
	tests := []struct {
		name        string
		archivePath string
		archiveType string
		want        int
	}{
		{"zip is a single pass", `C:\tmp\pkg.zip`, "zip", 1},
		{"7z is a single pass", `C:\tmp\pkg.7z`, "7z", 1},
		{"plain tar is a single pass", `C:\tmp\pkg.tar`, "tar", 1},
		{"tar.gz name needs a second pass", `C:\tmp\pkg.tar.gz`, "", 2},
		{"tgz name needs a second pass", `C:\tmp\pkg.tgz`, "", 2},
		{"tar.bz2 name needs a second pass", `C:\tmp\pkg.tar.bz2`, "", 2},
		{"gz type needs a second pass", `C:\tmp\pkg.gz`, "gz", 2},
		{"bz2 type needs a second pass", `C:\tmp\pkg.bz2`, "bz2", 2},
		{"xz type needs a second pass", `C:\tmp\pkg.xz`, "xz", 2},
		{"compound name still wins over a plain type", `C:\tmp\pkg.tar.xz`, "7z", 2},
	}
	for _, tc := range tests {
		if got := extractStages(tc.archivePath, tc.archiveType); got != tc.want {
			t.Errorf("%s: extractStages(%q, %q) = %d, want %d", tc.name, tc.archivePath, tc.archiveType, got, tc.want)
		}
	}
}

func TestSevenZipBinary_defaultsThenOverride(t *testing.T) {
	ext := NewExtractor(nil)
	if got := ext.sevenZipBinary(); got != "7z.exe" {
		t.Fatalf("sevenZipBinary() = %q, want 7z.exe", got)
	}

	ext.Set7zPath(`C:\tools\7z.exe`)
	if got := ext.sevenZipBinary(); got != `C:\tools\7z.exe` {
		t.Fatalf("sevenZipBinary() = %q, want the configured path", got)
	}
}
