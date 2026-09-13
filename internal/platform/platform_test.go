package platform

import (
	"path/filepath"
	"testing"
)

func TestOverridesWin(t *testing.T) {
	t.Setenv("PULSE_DATA_DIR", filepath.Join("x", "data"))
	t.Setenv("PULSE_CONFIG_DIR", filepath.Join("x", "cfg"))
	if d, _ := DataDir(); d != filepath.Join("x", "data") {
		t.Fatalf("data dir = %s", d)
	}
	f, _ := DefaultConfigFile()
	if f != filepath.Join("x", "cfg", "config.yaml") {
		t.Fatalf("config file = %s", f)
	}
}

func TestDefaultsAreAbsolute(t *testing.T) {
	t.Setenv("PULSE_DATA_DIR", "")
	t.Setenv("PULSE_CONFIG_DIR", "")
	for _, fn := range []func() (string, error){DataDir, ConfigDir} {
		d, err := fn()
		if err != nil {
			t.Skipf("no home directory in this environment: %v", err)
		}
		if !filepath.IsAbs(d) {
			t.Fatalf("%s is not absolute", d)
		}
	}
}
