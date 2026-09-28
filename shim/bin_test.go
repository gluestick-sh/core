package shim

import "testing"

func TestParseBinPattern(t *testing.T) {
	tests := []struct {
		pattern   string
		wantExe   string
		wantAlias string
	}{
		{"git.exe", "git.exe", ""},
		{"bin\\git.exe", "bin\\git.exe", ""},
		{"git.exe,git", "git.exe", "git"},
		{`["git.exe", "git"]`, "git.exe", "git"},
		{`[git.exe,git,--version]`, "git.exe", "git"},
		{"[bin/foo.exe,foo-cmd]", "bin/foo.exe", "foo-cmd"},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			exe, alias := ParseBinPattern(tt.pattern)
			if exe != tt.wantExe || alias != tt.wantAlias {
				t.Errorf("ParseBinPattern(%q) = (%q, %q), want (%q, %q)",
					tt.pattern, exe, alias, tt.wantExe, tt.wantAlias)
			}
		})
	}
}

func TestParseBinPatternParts(t *testing.T) {
	exe, alias, extra := ParseBinPatternParts(`[git.exe,git,--version]`)
	if exe != "git.exe" || alias != "git" || len(extra) != 1 || extra[0] != "--version" {
		t.Fatalf("ParseBinPatternParts() = (%q, %q, %v)", exe, alias, extra)
	}
}
