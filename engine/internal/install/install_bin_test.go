package install

import "testing"

func TestShimNameForBin(t *testing.T) {
	tests := []struct {
		exe, alias string
		want       string
	}{
		{"git.exe", "", "git"},
		{"bin\\git.exe", "git", "git"},
		{"busybox.exe", "bb", "bb"},
		{"tool.exe", "tool.exe", "tool"},
	}

	for _, tt := range tests {
		got := shimNameForBin(tt.exe, tt.alias)
		if got != tt.want {
			t.Errorf("shimNameForBin(%q, %q) = %q, want %q", tt.exe, tt.alias, got, tt.want)
		}
	}
}

