//go:build windows

package platform

import (
	"os"
	"path/filepath"
)

const OSName = "Windows"

const LoadAvgSupported = false

func configDir() (string, error) {
	if d := os.Getenv("APPDATA"); d != "" {
		return filepath.Join(d, "Pulse"), nil
	}
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "AppData", "Roaming", "Pulse"), nil
}

func dataDir() (string, error) {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "Pulse"), nil
	}
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "AppData", "Local", "Pulse"), nil
}
