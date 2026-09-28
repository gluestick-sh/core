package shim

import "testing"

func TestPackageNameFromAppsPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{`C:\Users\test\.glue\apps\nodejs\22.0.0\current\node.exe`, "nodejs"},
		{`/home/user/.glue/apps/nodejs-lts/20.0.0/current/node.exe`, "nodejs-lts"},
		{`C:\Users\test\.glue\apps\nodejs\22.0.0\node.exe`, "nodejs"},
		{`C:\other\node.exe`, ""},
	}
	for _, tt := range tests {
		if got := packageNameFromAppsPath(tt.path); got != tt.want {
			t.Errorf("packageNameFromAppsPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestShimMetaBelongsToPackage_exactMatch(t *testing.T) {
	nodejsPath := `C:\glue\apps\nodejs\22.0.0\current\node.exe`
	if BelongsToPackage(nodejsPath, "node") {
		t.Fatal("nodejs path must not belong to package node")
	}
	if !BelongsToPackage(nodejsPath, "nodejs") {
		t.Fatal("nodejs path must belong to package nodejs")
	}
}
