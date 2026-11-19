// Package skin loads skin manifests (pet.json).
package skin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"mnemo-pet/internal/wardrobe"
)

type Anim struct {
	Frames []string `json:"frames"`
	FPS    float64  `json:"fps"`
	Loop   bool     `json:"loop"`
}

type Manifest struct {
	Name  string          `json:"name"`
	Label string          `json:"label,omitempty"` // display name in the menu
	Size  [2]int          `json:"size"`
	Anims map[string]Anim `json:"anims"`

	// A skin may carry its own schedule: drop the folder in and it wears itself
	// at the right time. Priority defaults to 10 when When is set.
	When     *wardrobe.When `json:"when,omitempty"`
	Priority int            `json:"priority,omitempty"`
}

// Load looks for <dir>/<name>/pet.json in each dir, in order.
func Load(dirs []string, name string) (*Manifest, string, error) {
	var lastErr error
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, name, "pet.json")
		b, err := os.ReadFile(p)
		if err != nil {
			lastErr = err
			continue
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, "", fmt.Errorf("%s: %w", p, err)
		}
		if len(m.Anims) == 0 {
			return nil, "", fmt.Errorf("%s: no anims", p)
		}
		if m.Name == "" {
			m.Name = name
		}
		return &m, filepath.Dir(p), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("skin %q not found", name)
	}
	return nil, "", lastErr
}

// Fallback is used when no manifest can be read; it only carries names and
// loop flags, which is all the core needs.
func Fallback() *Manifest {
	loop := map[string]bool{
		"idle": true, "walk": true, "sit": true, "sleep": true,
		"think": true, "drag": true,
	}
	m := &Manifest{Name: "builtin", Size: [2]int{128, 128}, Anims: map[string]Anim{}}
	for _, n := range []string{"idle", "blink", "walk", "sit", "sleep", "wave", "happy",
		"think", "apologize", "drag", "react", "notify"} {
		m.Anims[n] = Anim{Loop: loop[n], FPS: 4}
	}
	return m
}

func (m *Manifest) Has(name string) bool { _, ok := m.Anims[name]; return ok }

func (m *Manifest) Names() []string {
	out := make([]string, 0, len(m.Anims))
	for n := range m.Anims {
		out = append(out, n)
	}
	return out
}

// Info describes one skin found on disk.
type Info struct {
	Name     string // folder name
	Label    string
	Dir      string
	Manifest *Manifest
}

// Library is the set of skins available to the core.
type Library struct {
	Skins    map[string]*Info
	Order    []string // sorted by name
	Warnings []string
}

// Scan lists every <dir>/<name>/pet.json. When a name exists in several dirs
// the first dir wins. Broken skins are skipped and reported in Warnings.
func Scan(dirs []string) *Library {
	lib := &Library{Skins: map[string]*Info{}}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || lib.Skins[e.Name()] != nil {
				continue
			}
			m, dir, err := Load([]string{d}, e.Name())
			if err != nil {
				if _, statErr := os.Stat(filepath.Join(d, e.Name(), "pet.json")); statErr == nil {
					lib.Warnings = append(lib.Warnings, fmt.Sprintf("skin %q skipped: %v", e.Name(), err))
				}
				continue
			}
			if m.When != nil {
				if err := m.When.Validate(); err != nil {
					lib.Warnings = append(lib.Warnings, fmt.Sprintf("skin %q: ignoring its schedule: %v", e.Name(), err))
					m.When = nil
				}
			}
			label := m.Label
			if label == "" {
				label = e.Name()
			}
			lib.Skins[e.Name()] = &Info{Name: e.Name(), Label: label, Dir: dir, Manifest: m}
		}
	}
	for n := range lib.Skins {
		lib.Order = append(lib.Order, n)
	}
	sort.Strings(lib.Order)
	return lib
}
