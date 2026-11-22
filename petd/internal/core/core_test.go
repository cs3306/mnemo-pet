package core

import (
	"sync"
	"testing"
	"time"

	"mnemo-pet/internal/config"
	"mnemo-pet/internal/proto"
	"mnemo-pet/internal/rules"
	"mnemo-pet/internal/skin"
)

type fakeSink struct {
	mu     sync.Mutex
	render []map[string]any
	events []proto.Event
	brain  bool
}

func (f *fakeSink) Render(m any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.render = append(f.render, m.(map[string]any))
}
func (f *fakeSink) Plugins(ev proto.Event) {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
}
func (f *fakeSink) HasRender() bool { return true }
func (f *fakeSink) HasBrain() bool  { return f.brain }

func (f *fakeSink) anims() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.render {
		if m["t"] == "anim" {
			out = append(out, m["name"].(string))
		}
	}
	return out
}

func (f *fakeSink) last(t string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.render) - 1; i >= 0; i-- {
		if f.render[i]["t"] == t {
			return f.render[i]
		}
	}
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestCore(t *testing.T, rulesJSON string) (*Core, *fakeSink, *clock) {
	t.Helper()
	cfg := config.Default()
	cfg.Behavior.Wander = false
	sink := &fakeSink{}
	eng, err := rules.Parse([]byte(rulesJSON))
	if err != nil {
		t.Fatal(err)
	}
	c := New(cfg, sink, skin.Fallback(), eng)
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	c.now = clk.now
	return c, sink, clk
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestConnectSendsConfigMenuAndIdle(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.handle(proto.Event{Name: "connected"})
	if s.last("config") == nil || s.last("menu") == nil {
		t.Fatal("config/menu not sent")
	}
	if got := s.anims(); !eq(got, []string{"idle"}) {
		t.Fatalf("anims %v", got)
	}
}

func TestPriorityArbitration(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.applyAct(proto.Act{Anim: "happy", Prio: 50}, "t", 50) // non-loop: locks at 50
	c.applyAct(proto.Act{Anim: "blink", Prio: 10}, "t", 10) // lower priority: dropped
	if got := s.anims(); !eq(got, []string{"happy"}) {
		t.Fatalf("low prio should be dropped: %v", got)
	}
	c.applyAct(proto.Act{Anim: "wave", Prio: 60}, "t", 60) // higher: wins
	if got := s.anims(); !eq(got, []string{"happy", "wave"}) {
		t.Fatalf("high prio should win: %v", got)
	}
}

func TestAnimEndReleasesLockAndReturnsToIdle(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.applyAct(proto.Act{Anim: "wave", Prio: 50}, "t", 50)
	c.handle(proto.Event{Name: "anim_end", Data: map[string]any{"name": "wave"}})
	if got := s.anims(); !eq(got, []string{"wave", "idle"}) {
		t.Fatalf("%v", got)
	}
	c.applyAct(proto.Act{Anim: "blink", Prio: 10}, "t", 10)
	if got := s.anims(); !eq(got, []string{"wave", "idle", "blink"}) {
		t.Fatalf("lock should be released: %v", got)
	}
}

func TestHoldExpiresOnTick(t *testing.T) {
	c, s, clk := newTestCore(t, `[]`)
	c.applyAct(proto.Act{Anim: "think", Hold: 3000, Prio: 50}, "t", 50)
	clk.t = clk.t.Add(2 * time.Second)
	c.tick()
	if got := s.anims(); !eq(got, []string{"think"}) {
		t.Fatalf("still held: %v", got)
	}
	clk.t = clk.t.Add(2 * time.Second)
	c.tick()
	if got := s.anims(); !eq(got, []string{"think", "idle"}) {
		t.Fatalf("hold should expire: %v", got)
	}
}

func TestSleepWakeAndBlocking(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.handle(proto.Event{Name: "idle", Data: map[string]any{"sec": float64(700)}})
	c.applyAct(proto.Act{Anim: "blink", Prio: PrioIdle}, "t", PrioIdle) // blocked while asleep
	if got := s.anims(); !eq(got, []string{"sleep"}) {
		t.Fatalf("%v", got)
	}
	c.applyAct(proto.Act{Anim: "notify", Prio: 70}, "t", 70) // plugin may wake it visually
	c.handle(proto.Event{Name: "anim_end", Data: map[string]any{"name": "notify"}})
	if got := s.anims(); !eq(got, []string{"sleep", "notify", "sleep"}) {
		t.Fatalf("should fall back asleep: %v", got)
	}
	c.handle(proto.Event{Name: "active", Data: map[string]any{"idle_sec": float64(700)}})
	if got := s.anims(); got[len(got)-1] != "idle" {
		t.Fatalf("should wake to idle: %v", got)
	}
}

func TestDragOverridesEverything(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.handle(proto.Event{Name: "drag_start"})
	c.applyAct(proto.Act{Anim: "happy", Prio: 90}, "t", 90)
	if got := s.anims(); !eq(got, []string{"drag"}) {
		t.Fatalf("%v", got)
	}
	c.handle(proto.Event{Name: "drag_end"})
	if got := s.anims(); got[len(got)-1] != "idle" {
		t.Fatalf("%v", got)
	}
}

func TestMoveAndArrived(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.applyAct(proto.Act{Move: &proto.Move{To: "left"}, Prio: 50}, "t", 50)
	if m := s.last("move"); m == nil || m["to"] != "left" || m["anim"] != "walk" {
		t.Fatalf("move msg: %v", m)
	}
	if !c.walking {
		t.Fatal("should be walking")
	}
	c.handle(proto.Event{Name: "arrived"})
	if c.walking || s.anims()[len(s.anims())-1] != "idle" {
		t.Fatalf("arrival should return to idle: %v", s.anims())
	}
}

func TestRulesFireAndBubbleTTL(t *testing.T) {
	c, s, _ := newTestCore(t, `[{"on":"click","do":{"anim":"react","say":"hi {x}"}}]`)
	c.handle(proto.Event{Name: "click", Data: map[string]any{"x": 5}})
	if got := s.anims(); !eq(got, []string{"react"}) {
		t.Fatalf("%v", got)
	}
	m := s.last("say")
	if m == nil || m["text"] != "hi 5" || m["ttl"].(int) < 6000 {
		t.Fatalf("say: %v", m)
	}
	if len(s.events) != 1 || s.events[0].Name != "click" {
		t.Fatal("plugins should receive the event")
	}
}

func TestNoBrainFallbackReply(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.handle(proto.Event{Name: "user_text", Data: map[string]any{"text": "hello"}})
	if s.last("say") == nil {
		t.Fatal("expected a fallback bubble")
	}
	s.brain = true
	n := len(s.render)
	c.handle(proto.Event{Name: "user_text", Data: map[string]any{"text": "hello"}})
	if len(s.render) != n {
		t.Fatal("brain present: core must stay quiet")
	}
}

func TestMenuLifecycle(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.setMenu("p", true, proto.MenuItem{ID: "a", Label: "A", Plugin: "p"})
	c.setMenu("p", true, proto.MenuItem{ID: "a", Label: "A2", Plugin: "p"}) // replace
	items := s.last("menu")["items"].([]proto.MenuItem)
	if len(items) != 1 || items[0].Label != "A2" {
		t.Fatalf("%+v", items)
	}
	c.setMenu("p", false, proto.MenuItem{ID: "a", Plugin: "p"})
	if len(s.last("menu")["items"].([]proto.MenuItem)) != 0 {
		t.Fatal("not removed")
	}
}

func TestRandomBehaviorRespectsSkinAndLocks(t *testing.T) {
	c, s, clk := newTestCore(t, `[]`)
	c.curAnim = "idle"
	c.nextBehavior = clk.t
	for i := 0; i < 30; i++ {
		clk.t = clk.t.Add(time.Minute)
		c.lastMinute = clk.t.Minute()
		c.nextBehavior = clk.t
		c.curAnim = "idle"
		c.clearLock()
		c.tick()
	}
	for _, a := range s.anims() {
		if a != "blink" && a != "sit" {
			t.Fatalf("wander disabled, unexpected anim %q", a)
		}
	}
	if len(s.anims()) == 0 {
		t.Fatal("expected some autonomous behaviour")
	}
}
