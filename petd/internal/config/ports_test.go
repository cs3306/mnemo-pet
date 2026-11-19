package config

import "testing"

func TestPortEnvBeatsStaleConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PET_PORT", "")
	t.Setenv("PET_HTTP_PORT", "")
	t.Setenv("PET_LISTEN", "")
	t.Setenv("PET_HTTP_LISTEN", "")
	c, err := Load(dir) // seeds config.json with the built-in default
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen == ":7890" || c.HTTPListen == ":7891" {
		t.Fatalf("old well-known ports must not be the default: %s %s", c.Listen, c.HTTPListen)
	}

	t.Setenv("PET_PORT", "24680")
	t.Setenv("PET_HTTP_PORT", "24681")
	c, _ = Load(dir)
	if c.Listen != ":24680" || c.HTTPListen != ":24681" {
		t.Fatalf("PET_PORT/PET_HTTP_PORT must win over config.json: %s %s", c.Listen, c.HTTPListen)
	}

	t.Setenv("PET_LISTEN", "127.0.0.1:30000")
	c, _ = Load(dir)
	if c.Listen != "127.0.0.1:30000" || c.HTTPListen != ":24681" {
		t.Fatalf("PET_LISTEN must win over PET_PORT: %s %s", c.Listen, c.HTTPListen)
	}
}
