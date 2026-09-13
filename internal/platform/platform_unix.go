//go:build !windows && !darwin

package platform

import (
	"os"
	"path/filepath"
)

const OSName = "Linux"

const LoadAvgSupported = true

func configDir() (string, error) { return xdg("XDG_CONFIG_HOME", ".config") }

func dataDir() (string, error) { return xdg("XDG_DATA_HOME", filepath.Join(".local", "share")) }

func xdg(env, fallback string) (string, error) {
	if d := os.Getenv(env); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, AppName), nil
	}
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, fallback, AppName), nil
}
