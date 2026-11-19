// Package config loads petd's configuration (JSON, standard library only).
package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed defaults/*.json
var defaults embed.FS

type Sensors struct {
	Focus      bool `json:"focus"`
	FocusTitle bool `json:"focus_title"`
	Notify     bool `json:"notify"`
	NotifyBody bool `json:"notify_body"`
}

type Behavior struct {
	Wander     bool           `json:"wander"`
	MinGap     int            `json:"min_gap_sec"`
	MaxGap     int            `json:"max_gap_sec"`
	WalkSpeed  int            `json:"walk_speed"`
	SleepAfter int            `json:"sleep_after_sec"`
	Weights    map[string]int `json:"weights"`
}

type Plugins struct {
	Disabled []string                     `json:"disabled"`
	Env      map[string]map[string]string `json:"env"`
}

// Location is used for sunrise/sunset based skin schedules.
type Location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type Config struct {
	Listen         string            `json:"listen"`
	HTTPListen     string            `json:"http_listen"`
	Token          string            `json:"token"`
	Skin           string            `json:"skin"`
	Scale          float64           `json:"scale"`
	Gravity        bool              `json:"gravity"`
	FloorMargin    int               `json:"floor_margin"`
	BubbleTTL      int               `json:"bubble_ttl"`
	IdleMarks      []int             `json:"idle_marks"`
	Sensors        Sensors           `json:"sensors"`
	Sounds         map[string]string `json:"sounds"`
	Behavior       Behavior          `json:"behavior"`
	NoBrainReplies []string          `json:"no_brain_replies"`
	Plugins        Plugins           `json:"plugins"`
	Location       *Location         `json:"location"` // null = sunrise/sunset schedules unavailable

	// Runtime paths (not read from the file).
	DataDir      string `json:"-"`
	SkinsDir     string `json:"-"`
	PluginsDir   string `json:"-"`
	RulesPath    string `json:"-"`
	WardrobePath string `json:"-"`
	StatePath    string `json:"-"`
}

// Default returns the built-in defaults.
func Default() *Config {
	b, _ := defaults.ReadFile("defaults/config.json")
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		panic(err) // embedded file, cannot fail in a correct build
	}
	return &c
}

// DefaultWardrobe returns the embedded default wardrobe file.
func DefaultWardrobe() []byte {
	b, _ := defaults.ReadFile("defaults/wardrobe.json")
	return b
}

// DefaultRules returns the embedded default rules file.
func DefaultRules() []byte {
	b, _ := defaults.ReadFile("defaults/rules.json")
	return b
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load reads <dataDir>/config.json (writing the defaults on first run), seeds
// <dataDir>/rules.json, then applies environment overrides.
func Load(dataDir string) (*Config, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	cfgPath := filepath.Join(dataDir, "config.json")
	c := Default()
	if b, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(b, c); err != nil { // overlay on defaults
			return nil, fmt.Errorf("%s: %w", cfgPath, err)
		}
	} else if os.IsNotExist(err) {
		d, _ := defaults.ReadFile("defaults/config.json")
		if err := os.WriteFile(cfgPath, d, 0o644); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
	} else {
		return nil, err
	}

	c.DataDir = dataDir
	c.RulesPath = filepath.Join(dataDir, "rules.json")
	if _, err := os.Stat(c.RulesPath); os.IsNotExist(err) {
		if err := os.WriteFile(c.RulesPath, DefaultRules(), 0o644); err != nil {
			return nil, fmt.Errorf("write default rules: %w", err)
		}
	}

	c.WardrobePath = filepath.Join(dataDir, "wardrobe.json")
	c.StatePath = filepath.Join(dataDir, "state.json")
	if _, err := os.Stat(c.WardrobePath); os.IsNotExist(err) {
		if err := os.WriteFile(c.WardrobePath, DefaultWardrobe(), 0o644); err != nil {
			return nil, fmt.Errorf("write default wardrobe: %w", err)
		}
	}

	if v := os.Getenv("PET_TOKEN"); v != "" {
		c.Token = v
	}
	// Ports: PET_LISTEN / PET_HTTP_LISTEN (full address) beat PET_PORT /
	// PET_HTTP_PORT (port only), which beat config.json. The port variables are
	// what .env sets, so a stale config.json can never pin an old port.
	if p := os.Getenv("PET_PORT"); p != "" {
		c.Listen = ":" + p
	}
	if p := os.Getenv("PET_HTTP_PORT"); p != "" {
		c.HTTPListen = ":" + p
	}
	c.Listen = env("PET_LISTEN", c.Listen)
	c.HTTPListen = env("PET_HTTP_LISTEN", c.HTTPListen)
	c.Skin = env("PET_SKIN", c.Skin)
	c.SkinsDir = env("PET_SKINS_DIR", "./skins")
	c.PluginsDir = env("PET_PLUGINS_DIR", "./plugins")
	return c, c.validate()
}

func (c *Config) validate() error {
	if c.Behavior.MinGap < 1 {
		c.Behavior.MinGap = 1
	}
	if c.Behavior.MaxGap < c.Behavior.MinGap {
		c.Behavior.MaxGap = c.Behavior.MinGap
	}
	if c.Behavior.WalkSpeed <= 0 {
		c.Behavior.WalkSpeed = 70
	}
	if c.BubbleTTL <= 0 {
		c.BubbleTTL = 6000
	}
	if c.Scale <= 0 {
		c.Scale = 1
	}
	if strings.TrimSpace(c.Skin) == "" {
		return fmt.Errorf("skin must not be empty")
	}
	if l := c.Location; l != nil && (l.Lat < -90 || l.Lat > 90 || l.Lon < -180 || l.Lon > 180) {
		return fmt.Errorf("location out of range (lat -90..90, lon -180..180)")
	}
	return nil
}

// PluginDisabled reports whether a plugin is switched off in the config.
func (c *Config) PluginDisabled(name string) bool {
	for _, n := range c.Plugins.Disabled {
		if n == name {
			return true
		}
	}
	return false
}
