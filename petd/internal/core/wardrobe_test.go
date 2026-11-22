package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mnemo-pet/internal/proto"
	"mnemo-pet/internal/state"
)

func writeSkin(t *testing.T, root, name string, extra map[string]any) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := map[string]any{
		"name": name, "size": []int{8, 8},
		"anims": map[string]any{
			"idle":  map[string]any{"frames": []string{"idle.png"}, "fps": 1, "loop": true},
			"blink": map[string]any{"frames": []string{"idle.png"}, "fps": 1},
		},
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(dir, "pet.json"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "idle.png"), []byte("x"), 0o644)
}

func wardrobeCore(t *testing.T, wardrobeJSON string) (*Core, *fakeSink, *clock, string) {
	t.Helper()
	root := t.TempDir()
	writeSkin(t, root, "default", nil)
	writeSkin(t, root, "night", map[string]any{"label": "夜晚", "when": map[string]any{"from": "22:00", "to": "07:00"}})
	writeSkin(t, root, "christmas", map[string]any{"priority": 30, "when": map[string]any{"holidays": []string{"christmas"}}})
	wp := filepath.Join(t.TempDir(), "wardrobe.json")
	if wardrobeJSON != "" {
		os.WriteFile(wp, []byte(wardrobeJSON), 0o644)
	}
	c, s, clk := newTestCore(t, `[]`)
	c.cfg.Skin = "default"
	c.curSkin = "default"
	c.configure(Options{Store: state.Memory(), SkinDirs: []string{root}, WardrobePath: wp})
	return c, s, clk, wp
}

func skinMsgs(s *fakeSink) (out []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.render {
		if m["t"] == "skin" {
			out = append(out, m["skin"].(string))
		}
	}
	return
}

func TestAutoSkinFollowsSchedule(t *testing.T) {
	c, s, clk, _ := wardrobeCore(t, "")
	c.handle(proto.Event{Name: "connected"})
	if c.curSkin != "default" {
		t.Fatalf("noon → default, got %s", c.curSkin)
	}
	clk.t = time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	c.tick()
	if c.curSkin != "night" {
		t.Fatalf("23:00 → night (from the skin's own pet.json), got %s", c.curSkin)
	}
	if got := skinMsgs(s); len(got) != 1 || got[0] != "night" {
		t.Fatalf("expected exactly one skin message, got %v", got)
	}
	clk.t = time.Date(2026, 12, 25, 23, 0, 0, 0, time.UTC) // Christmas night: priority 30 > night's 10
	c.rescan(true)
	c.evalSkin(false)
	if c.curSkin != "christmas" {
		t.Fatalf("holiday should win over night, got %s", c.curSkin)
	}
	clk.t = time.Date(2026, 12, 28, 12, 0, 0, 0, time.UTC)
	c.evalSkin(false)
	if c.curSkin != "default" {
		t.Fatalf("back to base skin, got %s", c.curSkin)
	}
}

func TestManualPinOverridesAndAutoReturns(t *testing.T) {
	c, _, clk, _ := wardrobeCore(t, "")
	c.handle(proto.Event{Name: "connected"})
	c.handle(proto.Event{Name: "menu.click", Data: map[string]any{"id": "skin:night"}})
	if c.curSkin != "night" || c.store.Get().SkinMode != state.ModeManual {
		t.Fatalf("pin failed: %s %+v", c.curSkin, c.store.Get())
	}
	clk.t = time.Date(2026, 12, 25, 12, 0, 0, 0, time.UTC) // schedule wants christmas
	c.rescan(true)
	c.tick()
	if c.curSkin != "night" {
		t.Fatalf("pinned skin must not change, got %s", c.curSkin)
	}
	c.handle(proto.Event{Name: "menu.click", Data: map[string]any{"id": "skin:auto"}})
	if c.curSkin != "christmas" || c.store.Get().SkinMode != state.ModeAuto {
		t.Fatalf("auto should resume: %s %+v", c.curSkin, c.store.Get())
	}
}

func TestMenuClickForSkinIsNotForwardedToPlugins(t *testing.T) {
	c, s, _, _ := wardrobeCore(t, "")
	c.handle(proto.Event{Name: "menu.click", Data: map[string]any{"id": "skin:night"}})
	if len(s.events) != 0 {
		t.Fatalf("plugins must not see core menu clicks: %v", s.events)
	}
	c.handle(proto.Event{Name: "menu.click", Data: map[string]any{"id": "other"}})
	if len(s.events) != 1 {
		t.Fatal("plugin menu clicks must still be forwarded")
	}
}

func TestUnknownPinnedSkinFallsBackToAuto(t *testing.T) {
	c, _, _, _ := wardrobeCore(t, "")
	if c.pinSkin("nope") {
		t.Fatal("unknown skin must be refused")
	}
	if c.store.Get().SkinMode != state.ModeAuto {
		t.Fatal("state must be untouched")
	}
}

func TestWardrobeFileEntriesAndHotReload(t *testing.T) {
	c, _, clk, wp := wardrobeCore(t, `{"entries":[{"skin":"christmas","priority":99,"when":{"months":[1]}}]}`)
	if c.curSkin != "christmas" {
		t.Fatalf("file entry should win in January, got %s", c.curSkin)
	}
	os.WriteFile(wp, []byte(`{"enabled":false,"entries":[]}`), 0o644)
	future := time.Now().Add(time.Hour)
	os.Chtimes(wp, future, future)
	clk.t = clk.t.Add(2 * time.Minute) // past the rescan interval
	c.tick()
	if c.curSkin != "night" && c.curSkin != "default" {
		t.Fatalf("edited file should be picked up, got %s", c.curSkin)
	}
	if c.curSkin == "christmas" {
		t.Fatal("hot reload did not happen")
	}
}

func TestSunScheduleNeedsLocation(t *testing.T) {
	c, _, clk, _ := wardrobeCore(t, `{"use_skin_schedules":false,"entries":[{"skin":"night","when":{"from":"sunset","to":"sunrise"}}]}`)
	clk.t = time.Date(2026, 6, 21, 23, 30, 0, 0, time.UTC)
	c.evalSkin(false)
	if c.curSkin != "default" {
		t.Fatalf("without location sun entries are skipped, got %s", c.curSkin)
	}
}

func TestSkinMenuShape(t *testing.T) {
	c, s, _, _ := wardrobeCore(t, "")
	c.handle(proto.Event{Name: "connected"})
	items := s.last("menu")["items"].([]proto.MenuItem)
	var sk *proto.MenuItem
	for i := range items {
		if items[i].Label == "皮肤" {
			sk = &items[i]
		}
	}
	if sk == nil || len(sk.Children) != 4 { // auto + 3 skins
		t.Fatalf("skin submenu wrong: %+v", sk)
	}
}
