package core

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"mnemo-pet/internal/proto"
	"mnemo-pet/internal/skin"
	"mnemo-pet/internal/state"
	"mnemo-pet/internal/wardrobe"
)

// Options wires the optional persistent/wardrobe features. The zero value
// gives an in-memory state and a wardrobe that only knows config.skin.
type Options struct {
	Store        *state.Store
	SkinDirs     []string // searched in order; first dir wins on name clashes
	WardrobePath string
}

type wardrobeState struct {
	lib       *skin.Library
	file      wardrobe.File
	fileMTime time.Time
	dirs      []string
	path      string
	lastScan  time.Time
	warned    map[string]bool
}

// Configure applies Options. Call before Run.
func (c *Core) Configure(o Options) { c.do(func() { c.configure(o) }) }

func (c *Core) configure(o Options) {
	{
		if o.Store != nil {
			c.store = o.Store
		}
		c.wd = &wardrobeState{dirs: o.SkinDirs, path: o.WardrobePath, warned: map[string]bool{}}
		c.rescan(true)
		st := c.store.Get()
		if st.SkinMode == state.ModeManual && c.wd.lib.Skins[st.Skin] == nil {
			slog.Warn("pinned skin not found, returning to auto", "skin", st.Skin)
			c.store.Update(func(s *state.State) { s.SkinMode, s.Skin = state.ModeAuto, "" })
		}
		c.evalSkin(true)
	}
}

func (c *Core) warnOnce(key, msg string) {
	if c.wd == nil || c.wd.warned[key] {
		return
	}
	c.wd.warned[key] = true
	slog.Warn(msg)
}

// rescan reloads the skin library and wardrobe.json when needed (at most once a
// minute, wardrobe.json only if its mtime changed).
func (c *Core) rescan(force bool) {
	w := c.wd
	if w == nil {
		return
	}
	now := c.now()
	if !force && now.Sub(w.lastScan) < 55*time.Second {
		return
	}
	w.lastScan = now
	w.lib = skin.Scan(w.dirs)
	for _, m := range w.lib.Warnings {
		c.warnOnce("lib:"+m, m)
	}
	if w.path != "" {
		if fi, err := os.Stat(w.path); err == nil && (force || !fi.ModTime().Equal(w.fileMTime)) {
			f, err := wardrobe.LoadFile(w.path)
			if err != nil {
				c.warnOnce("file:"+err.Error(), "wardrobe.json ignored: "+err.Error())
			} else {
				w.file, w.fileMTime = f, fi.ModTime()
				slog.Info("wardrobe loaded", "entries", len(f.Entries))
			}
		}
	}
}

func (c *Core) env() wardrobe.Env {
	e := wardrobe.Env{Now: c.now()}
	if l := c.cfg.Location; l != nil {
		e.Lat, e.Lon, e.HasLoc = l.Lat, l.Lon, true
	}
	return e
}

// autoSkin picks the skin the schedule asks for right now ("" = none).
func (c *Core) autoSkin() string {
	w := c.wd
	if w == nil {
		return ""
	}
	if w.file.Enabled != nil && !*w.file.Enabled {
		return ""
	}
	var entries []wardrobe.Entry
	for _, e := range w.file.Entries {
		if e.Priority == 0 {
			e.Priority = 50
		}
		entries = append(entries, e)
	}
	if w.file.UseSkinSchedules == nil || *w.file.UseSkinSchedules {
		for _, n := range w.lib.Order {
			m := w.lib.Skins[n].Manifest
			if m.When == nil {
				continue
			}
			p := m.Priority
			if p == 0 {
				p = 10
			}
			entries = append(entries, wardrobe.Entry{Name: "skin:" + n, Skin: n, Priority: p, When: *m.When})
		}
	}
	e, ok, errs := wardrobe.Pick(entries, c.env())
	for _, err := range errs {
		c.warnOnce("pick:"+err.Error(), "wardrobe: skipped entry: "+err.Error())
	}
	if !ok {
		return ""
	}
	if w.lib.Skins[e.Skin] == nil {
		c.warnOnce("missing:"+e.Skin, fmt.Sprintf("wardrobe: entry %q wants skin %q which is not installed", e.Name, e.Skin))
		return ""
	}
	return e.Skin
}

// wantedSkin is what should be worn now given mode, schedule and config.
func (c *Core) wantedSkin() string {
	st := c.store.Get()
	if c.wd != nil && st.SkinMode == state.ModeManual && c.wd.lib.Skins[st.Skin] != nil {
		return st.Skin
	}
	if a := c.autoSkin(); a != "" {
		return a
	}
	return c.cfg.Skin
}

// evalSkin switches skin if the wanted one differs from the worn one.
func (c *Core) evalSkin(initial bool) {
	if c.wd == nil {
		return
	}
	want := c.wantedSkin()
	if want == c.curSkin {
		return
	}
	c.switchSkin(want, initial)
}

func (c *Core) switchSkin(name string, quiet bool) {
	var m *skin.Manifest
	if c.wd != nil {
		if i := c.wd.lib.Skins[name]; i != nil {
			m = i.Manifest
		}
	}
	if m == nil {
		if name == c.cfg.Skin && c.baseSkin != nil {
			m = c.baseSkin
		} else {
			return
		}
	}
	slog.Info("skin", "name", name)
	c.skin, c.curSkin = m, name
	if c.curAnim != "" && !c.skin.Has(c.curAnim) {
		c.curAnim = ""
	}
	if !quiet || c.sink.HasRender() {
		c.sink.Render(map[string]any{"t": "skin", "skin": name, "scale": c.cfg.Scale})
		c.sendMenu()
		if !c.dragging && !c.walking && !c.sleeping {
			c.curAnim = ""
			c.restState()
		}
	}
	c.publish()
}

// SetSkin pins a skin (manual mode). Returns false if it does not exist.
func (c *Core) SetSkin(name string) bool {
	done := make(chan bool, 1)
	c.do(func() { done <- c.pinSkin(name) })
	select {
	case ok := <-done:
		return ok
	case <-time.After(2 * time.Second):
		return false
	}
}

// AutoSkin returns to schedule-driven skin selection.
func (c *Core) AutoSkin() { c.do(c.unpinSkin) }

func (c *Core) pinSkin(name string) bool {
	if c.wd == nil || c.wd.lib.Skins[name] == nil {
		return false
	}
	if err := c.store.Update(func(s *state.State) { s.SkinMode, s.Skin = state.ModeManual, name }); err != nil {
		slog.Warn("saving state failed", "err", err)
	}
	c.evalSkin(false)
	c.sendMenu()
	return true
}

func (c *Core) unpinSkin() {
	if err := c.store.Update(func(s *state.State) { s.SkinMode, s.Skin = state.ModeAuto, "" }); err != nil {
		slog.Warn("saving state failed", "err", err)
	}
	c.rescan(true)
	c.evalSkin(false)
	c.sendMenu()
}

// skinMenu builds the "皮肤" submenu.
func (c *Core) skinMenu() (proto.MenuItem, bool) {
	if c.wd == nil || len(c.wd.lib.Skins) < 2 {
		return proto.MenuItem{}, false
	}
	st := c.store.Get()
	auto := "自动（按计划）"
	if st.SkinMode == state.ModeAuto {
		auto = "● " + auto
	}
	kids := []proto.MenuItem{{ID: "skin:auto", Label: auto}}
	for _, n := range c.wd.lib.Order {
		l := c.wd.lib.Skins[n].Label
		if n == c.curSkin {
			l = "● " + l
		}
		kids = append(kids, proto.MenuItem{ID: "skin:" + n, Label: l})
	}
	return proto.MenuItem{Label: "皮肤", Children: kids}, true
}

// handleMenuClick consumes core-owned menu ids; returns true if consumed.
func (c *Core) handleMenuClick(ev proto.Event) bool {
	id := str(ev.Data["id"])
	switch {
	case id == "skin:auto":
		c.unpinSkin()
	case strings.HasPrefix(id, "skin:"):
		c.pinSkin(strings.TrimPrefix(id, "skin:"))
	case strings.HasPrefix(id, "channel:"):
		on := true
		if b, ok := ev.Data["checked"].(bool); ok {
			on = b
		}
		c.setChannel(strings.TrimPrefix(id, "channel:"), on)
	default:
		return false
	}
	return true
}
