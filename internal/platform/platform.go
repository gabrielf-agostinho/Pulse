package platform

import (
	"errors"
	"os"
	"path/filepath"
)

const AppName = "pulse"

func ConfigDir() (string, error) {
	if dir := os.Getenv("PULSE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	return configDir()
}

func DataDir() (string, error) {
	if dir := os.Getenv("PULSE_DATA_DIR"); dir != "" {
		return dir, nil
	}
	return dataDir()
}

func DefaultConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func home() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "", errors.New("cannot determine home directory; set PULSE_DATA_DIR and PULSE_CONFIG_DIR")
	}
	return h, nil
}
