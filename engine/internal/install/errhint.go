package install

import (
	"fmt"
	"strings"
)

func phaseError(cause string, err error, hint string) error {
	if err == nil {
		return nil
	}
	if strings.TrimSpace(hint) == "" {
		return fmt.Errorf("%s: %w", cause, err)
	}
	return fmt.Errorf("%s: %w\n\n%s", cause, err, hint)
}

func downloadPhaseError(urlCount int, err error) error {
	return phaseError(
		fmt.Sprintf("download failed after trying %d URL(s)", urlCount),
		err,
		"Check your network connection or configure a GitHub mirror, then retry",
	)
}

func extractPhaseError(err error) error {
	return phaseError("extraction failed", err, "Run glue install 7zip to install 7-Zip, or reinstall the package")
}

func permissionPhaseError(cause string, err error) error {
	return phaseError(cause, err, "Check ~/.glue directory permissions or run as administrator")
}
