package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstRunSeedsFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PET_TOKEN", "")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"config.json", "rules.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not seeded", f)
		}
	}
	if c.Skin != "default" || c.Behavior.WalkSpeed <= 0 {
		t.Fatalf("%+v", c)
	}
}

func TestUserFileOverlaysDefaultsAndEnvWins(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"scale":2,"token":"a","behavior":{"wander":false}}`), 0o644)
	t.Setenv("PET_TOKEN", "fromenv")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Scale != 2 || c.Behavior.Wander {
		t.Fatalf("overlay failed: %+v", c)
	}
	if c.Behavior.WalkSpeed != 70 { // untouched default survives
		t.Fatalf("defaults lost: %+v", c.Behavior)
	}
	if c.Token != "fromenv" {
		t.Fatalf("env must win, got %q", c.Token)
	}
}

func TestBrokenConfigIsAnError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{oops`), 0o644)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error")
	}
}
