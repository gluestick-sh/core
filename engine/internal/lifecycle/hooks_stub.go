//go:build !windows

package lifecycle

import "strings"

func DecodePowerShellOutput(b []byte) string {
	return strings.TrimSpace(string(b))
}
