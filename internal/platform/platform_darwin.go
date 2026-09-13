//go:build darwin

package platform

import "path/filepath"

const OSName = "macOS"

const LoadAvgSupported = true

func configDir() (string, error) { return appSupport() }

func dataDir() (string, error) { return appSupport() }

func appSupport() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "Library", "Application Support", "Pulse"), nil
}
