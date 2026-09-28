package engine

import "github.com/gluestick-sh/core/verbose"

// SetColorEnabled toggles ANSI color codes in engine progress output.
func SetColorEnabled(enabled bool) {
	verbose.SetColorEnabled(enabled)
}
