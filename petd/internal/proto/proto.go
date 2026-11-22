// Package proto defines the wire messages shared by petd, the render client
// (GNOME Shell extension) and plugins. Transport is newline-delimited JSON.
package proto

import "strings"

// Move asks the renderer to walk somewhere.
type Move struct {
	X     *int   `json:"x,omitempty"`     // absolute screen x of the pet's left edge
	To    string `json:"to,omitempty"`    // left | right | center | random
	Speed int    `json:"speed,omitempty"` // px/s, 0 = default
}

// Act is a request to make the pet do something.
type Act struct {
	Anim   string   `json:"anim,omitempty"`
	Loop   *bool    `json:"loop,omitempty"`
	Say    string   `json:"say,omitempty"`
	SayAny []string `json:"say_any,omitempty"` // pick one at random
	TTL    int      `json:"ttl,omitempty"`     // bubble lifetime, ms
	Sound  string   `json:"sound,omitempty"`
	Move   *Move    `json:"move,omitempty"`
	Prio   int      `json:"prio,omitempty"` // arbitration priority, default depends on source
	Hold   int      `json:"hold,omitempty"` // keep the animation locked this long, ms
	// Channel names the "speech channel" this act belongs to. The user can
	// switch channels off from the right-click menu; acts on a disabled channel
	// are dropped entirely. Empty = not switchable.
	Channel string `json:"channel,omitempty"`
}

// Channel is a user-switchable source of speech (a periodic report, a reminder, ...).
type Channel struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Group   string `json:"group,omitempty"`
	Default bool   `json:"default"`
	Plugin  string `json:"-"` // owner; empty for core/rule channels
}

// Event is something that happened (click, idle, time, plugin.x.y ...).
type Event struct {
	Name string         `json:"name"`
	Data map[string]any `json:"data,omitempty"`
}

// MenuItem is an entry of the pet's right-click menu.
type MenuItem struct {
	ID       string     `json:"id,omitempty"`
	Label    string     `json:"label"`
	Type     string     `json:"type,omitempty"`    // "" action | "switch" | "header"
	Checked  bool       `json:"checked,omitempty"` // for switches
	Local    string     `json:"local,omitempty"`   // handled inside the renderer, e.g. "chat"
	Children []MenuItem `json:"children,omitempty"`
	Plugin   string     `json:"-"`
}

// In is any message sent by a client to petd.
type In struct {
	T         string         `json:"t"`
	Role      string         `json:"role,omitempty"` // hello: render | plugin
	Name      string         `json:"name,omitempty"` // hello: plugin name; event: event name
	Token     string         `json:"token,omitempty"`
	Subscribe []string       `json:"subscribe,omitempty"`
	Caps      []string       `json:"caps,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	ID        string         `json:"id,omitempty"`
	Label     string         `json:"label,omitempty"`
	Group     string         `json:"group,omitempty"`   // channel.add
	Enabled   *bool          `json:"enabled,omitempty"` // channel.add: default state
	Act
}

// Match reports whether an event name matches a pattern. Patterns are exact
// names, "*" (everything) or a prefix ending in ".*" / "*".
func Match(pattern, name string) bool {
	if pattern == "*" || pattern == name {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
	}
	return false
}
