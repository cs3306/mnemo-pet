// Package core is the pet's brain: it owns the state machine, the animation
// arbitration, the rules engine and the menu. Everything runs on one goroutine
// (see Run) so there is no locking inside the logic itself.
package core

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"mnemo-pet/internal/config"
	"mnemo-pet/internal/proto"
	"mnemo-pet/internal/rules"
	"mnemo-pet/internal/skin"
	"mnemo-pet/internal/state"
)

// Sink is how the core talks to the outside world (implemented by hub.Hub).
type Sink interface {
	Render(msg any)
	Plugins(ev proto.Event)
	HasRender() bool
	HasBrain() bool
}

// Default priorities.
const (
	PrioIdle      = 10 // autonomous behaviour (blink, wander, sit)
	PrioSleep     = 20
	PrioRule      = 40
	PrioPlugin    = 50
	PrioHTTP      = 60
	PrioUserReply = 65 // answers to something the user just typed
	PrioDrag      = 100
	safetyLockMs  = 20000

	bubbleMinShow = 1500 * time.Millisecond // a bubble is only protected this long
)

type Snapshot struct {
	State         string `json:"state"`
	Anim          string `json:"anim"`
	LockAnim      string `json:"lock_anim,omitempty"`
	LockPrio      int    `json:"lock_prio,omitempty"`
	RenderUp      bool   `json:"render_connected"`
	Skin          string `json:"skin"`
	Anims         int    `json:"anim_count"`
	Rules         int    `json:"rule_count"`
	Menu          int    `json:"menu_items"`
	EventsHandled uint64 `json:"events_handled"`

	SkinMode string        `json:"skin_mode"`
	Skins    []SkinInfo    `json:"skins,omitempty"`
	Channels []ChannelInfo `json:"channels,omitempty"`
}

// SkinInfo is one installed skin as shown in the API.
type SkinInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type Core struct {
	cfg   *config.Config
	sink  Sink
	skin  *skin.Manifest
	rules *rules.Engine
	jobs  chan func()
	now   func() time.Time
	rnd   *rand.Rand

	// Owned by the Run goroutine.
	curAnim      string
	lockPrio     int
	lockUntil    time.Time
	lockAnim     string
	bubPrio      int
	bubSince     time.Time
	sleeping     bool
	dragging     bool
	walking      bool
	nextBehavior time.Time
	lastMinute   int
	menu         []proto.MenuItem
	handled      uint64

	store    *state.Store
	wd       *wardrobeState
	baseSkin *skin.Manifest // the skin config.skin resolved to at startup
	curSkin  string
	channels map[string]proto.Channel
	chOrder  []string

	snapMu sync.RWMutex
	snap   Snapshot
}

func New(cfg *config.Config, sink Sink, sk *skin.Manifest, r *rules.Engine) *Core {
	c := &Core{
		cfg: cfg, sink: sink, skin: sk, rules: r,
		jobs: make(chan func(), 512),
		now:  time.Now,
		rnd:  rand.New(rand.NewSource(time.Now().UnixNano())),

		lastMinute: -1,

		store: state.Memory(), baseSkin: sk, curSkin: cfg.Skin,
		channels: map[string]proto.Channel{},
	}
	for _, ch := range r.Channels() {
		c.registerChannel(ch)
	}
	c.scheduleNext(c.now())
	c.publish()
	return c
}

func (c *Core) do(fn func()) {
	select {
	case c.jobs <- fn:
	default:
		slog.Warn("core queue full, dropping job")
	}
}

// ---- public, goroutine-safe API -------------------------------------------

func (c *Core) Emit(ev proto.Event)         { c.do(func() { c.handle(ev) }) }
func (c *Core) Act(a proto.Act, src string) { c.do(func() { c.applyAct(a, src, PrioPlugin) }) }
func (c *Core) ActHTTP(a proto.Act)         { c.do(func() { c.applyAct(a, "http", PrioHTTP) }) }

func (c *Core) SetMenu(plugin string, add bool, it proto.MenuItem) {
	c.do(func() { c.setMenu(plugin, add, it) })
}

func (c *Core) DropPluginMenu(plugin string) {
	c.do(func() {
		c.dropPluginChannels(plugin)
		kept := c.menu[:0]
		for _, m := range c.menu {
			if m.Plugin != plugin {
				kept = append(kept, m)
			}
		}
		c.menu = kept
		c.sendMenu()
	})
}

func (c *Core) RenderUp()   { c.Emit(proto.Event{Name: "connected"}) }
func (c *Core) RenderDown() { c.do(func() { c.curAnim = ""; c.publish() }) }

func (c *Core) Snapshot() Snapshot {
	c.snapMu.RLock()
	defer c.snapMu.RUnlock()
	return c.snap
}

// Run processes jobs and timers until ctx is cancelled.
func (c *Core) Run(ctx context.Context) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case fn := <-c.jobs:
			fn()
			c.publish()
		case <-tick.C:
			c.tick()
			c.publish()
		case <-ctx.Done():
			return
		}
	}
}

func (c *Core) publish() {
	st := "idle"
	switch {
	case c.dragging:
		st = "dragging"
	case c.sleeping:
		st = "sleeping"
	case c.walking:
		st = "walking"
	}
	s := Snapshot{
		State: st, Anim: c.curAnim, RenderUp: c.sink.HasRender(), Skin: c.curSkin,
		Anims: len(c.skin.Anims), Rules: c.rules.Count(), Menu: len(c.menu), EventsHandled: c.handled,
	}
	if c.locked() {
		s.LockAnim, s.LockPrio = c.lockAnim, c.lockPrio
	}
	s.SkinMode = c.store.Get().SkinMode
	if c.wd != nil {
		for _, n := range c.wd.lib.Order {
			s.Skins = append(s.Skins, SkinInfo{Name: n, Label: c.wd.lib.Skins[n].Label})
		}
	}
	s.Channels = c.channelList()
	c.snapMu.Lock()
	c.snap = s
	c.snapMu.Unlock()
}

// ---- event handling -----------------------------------------------------------

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}

func (c *Core) handle(ev proto.Event) {
	now := c.now()
	c.handled++
	switch ev.Name {
	case "menu.click":
		if c.handleMenuClick(ev) {
			return
		}
	case "connected":
		c.bubSince = time.Time{}
		c.rescan(false)
		c.evalSkin(false)
		c.sendConfig()
		c.sendMenu()
		c.curAnim = ""
		c.restState()
	case "drag_start":
		c.dragging, c.walking = true, false
		c.clearLock()
		c.playAnim("drag", PrioDrag, 0, nil)
	case "drag_end":
		c.dragging = false
		c.sleeping = false
		c.clearLock()
		c.scheduleNext(now)
		c.restState()
	case "arrived":
		if c.walking {
			c.walking = false
			c.clearLock()
			c.scheduleNext(now)
			c.restState()
		}
	case "anim_end":
		name := str(ev.Data["name"])
		if name != "" && name == c.lockAnim {
			c.clearLock()
		}
		if name == c.curAnim && !c.dragging && !c.walking {
			c.restState()
		}
	case "idle":
		if int(num(ev.Data["sec"])) >= c.cfg.Behavior.SleepAfter && !c.sleeping && !c.dragging && c.skin.Has("sleep") {
			c.sleeping, c.walking = true, false
			c.clearLock()
			c.playAnim("sleep", PrioSleep, 0, nil)
		}
	case "active":
		if c.sleeping {
			c.sleeping = false
			c.clearLock()
			c.scheduleNext(now)
			c.restState()
		}
	case "user_text":
		if !c.sink.HasBrain() && len(c.cfg.NoBrainReplies) > 0 {
			t := c.cfg.NoBrainReplies[c.rnd.Intn(len(c.cfg.NoBrainReplies))]
			c.applyAct(proto.Act{Say: t, Anim: "apologize", Prio: PrioUserReply}, "core", PrioUserReply)
		}
	}

	for _, a := range c.rules.Eval(ev, now) {
		c.applyAct(a, "rule", PrioRule)
	}
	c.sink.Plugins(ev)
}

// ---- acting ----------------------------------------------------------------------

func (c *Core) locked() bool { return c.now().Before(c.lockUntil) }

func (c *Core) clearLock() {
	c.lockUntil, c.lockPrio, c.lockAnim = time.Time{}, 0, ""
}

// blocked reports whether something with this priority may not act right now.
func (c *Core) blocked(prio int) bool {
	if c.dragging && prio < PrioDrag {
		return true
	}
	if c.sleeping && prio < PrioSleep {
		return true
	}
	return c.locked() && prio < c.lockPrio
}

func (c *Core) applyAct(a proto.Act, src string, defPrio int) {
	if a.Channel != "" {
		if _, ok := c.channels[a.Channel]; !ok {
			c.registerChannel(proto.Channel{ID: a.Channel, Default: true})
			c.sendMenu()
		}
		if !c.chOn(a.Channel) {
			return // the user switched this kind of speech off
		}
	}
	prio := a.Prio
	if prio == 0 {
		prio = defPrio
	}
	now := c.now()
	blocked := c.blocked(prio)

	if a.Say == "" && len(a.SayAny) > 0 {
		a.Say = a.SayAny[c.rnd.Intn(len(a.SayAny))]
	}
	if a.Move != nil && !blocked {
		c.doMove(a.Move, a.Anim, prio, a.Hold)
	} else if a.Anim != "" && !blocked {
		c.playAnim(a.Anim, prio, a.Hold, a.Loop)
	}
	// A bubble is only protected for a short minimum display time; after that a
	// newer message always replaces it, so replies never get stuck behind it.
	if a.Say != "" && !(now.Sub(c.bubSince) < bubbleMinShow && prio < c.bubPrio) {
		ttl := a.TTL
		if ttl <= 0 {
			ttl = c.cfg.BubbleTTL
			if l := len([]rune(a.Say)) * 120; l > ttl {
				ttl = l
			}
			if ttl > 20000 {
				ttl = 20000
			}
		}
		c.bubPrio, c.bubSince = prio, now
		c.sink.Render(map[string]any{"t": "say", "text": a.Say, "ttl": ttl})
	}
	if a.Sound != "" {
		c.sink.Render(map[string]any{"t": "sound", "name": a.Sound})
	}
	_ = src
}

func (c *Core) sendAnim(name string, loop bool) {
	c.curAnim = name
	c.sink.Render(map[string]any{"t": "anim", "name": name, "loop": loop})
}

// playAnim starts an animation and installs the matching lock.
func (c *Core) playAnim(name string, prio, hold int, loop *bool) {
	if !c.skin.Has(name) {
		slog.Debug("skin has no such animation", "anim", name)
		return
	}
	l := c.skin.Anims[name].Loop
	if loop != nil {
		l = *loop
	}
	c.sendAnim(name, l)
	now := c.now()
	switch {
	case hold > 0:
		c.lockPrio, c.lockUntil, c.lockAnim = prio, now.Add(time.Duration(hold)*time.Millisecond), name
	case !l:
		// Released by the renderer's anim_end event; the timeout is a safety net.
		c.lockPrio, c.lockUntil, c.lockAnim = prio, now.Add(safetyLockMs*time.Millisecond), name
	default:
		c.clearLock()
	}
}

func (c *Core) doMove(m *proto.Move, anim string, prio, hold int) {
	if anim == "" {
		anim = "walk"
	}
	if !c.skin.Has(anim) {
		anim = "idle"
	}
	speed := m.Speed
	if speed <= 0 {
		speed = c.cfg.Behavior.WalkSpeed
	}
	c.walking = true
	c.sink.Render(map[string]any{"t": "move", "x": m.X, "to": m.To, "speed": speed, "anim": anim})
	c.curAnim = anim
	c.lockPrio, c.lockAnim = prio, anim
	c.lockUntil = c.now().Add(60 * time.Second)
}

// restState puts the pet back into its resting animation.
func (c *Core) restState() {
	switch {
	case c.sleeping && c.skin.Has("sleep"):
		c.sendAnim("sleep", true)
	case c.skin.Has("idle"):
		c.sendAnim("idle", true)
	}
}

// ---- timers -------------------------------------------------------------------------

func (c *Core) scheduleNext(now time.Time) {
	b := c.cfg.Behavior
	gap := b.MinGap
	if b.MaxGap > b.MinGap {
		gap += c.rnd.Intn(b.MaxGap - b.MinGap + 1)
	}
	c.nextBehavior = now.Add(time.Duration(gap) * time.Second)
}

func (c *Core) tick() {
	now := c.now()

	if !c.lockUntil.IsZero() && !now.Before(c.lockUntil) { // lock expired
		wasWalking := c.walking
		c.clearLock()
		if wasWalking {
			c.walking = false // renderer never reported arrival; give up
		}
		if !c.dragging {
			c.restState()
		}
	}

	if m := now.Minute(); m != c.lastMinute {
		first := c.lastMinute == -1
		c.lastMinute = m
		c.rescan(false)
		c.evalSkin(false)
		if !first {
			c.handle(proto.Event{Name: "time", Data: map[string]any{
				"hour": now.Hour(), "minute": m, "hm": now.Format("15:04"),
				"weekday": int(now.Weekday()),
			}})
		}
	}

	if !c.sink.HasRender() || c.sleeping || c.dragging || c.walking || c.locked() || c.curAnim != "idle" {
		return
	}
	if now.Before(c.nextBehavior) {
		return
	}
	c.scheduleNext(now)
	c.randomBehavior()
}

func (c *Core) randomBehavior() {
	type opt struct {
		name string
		w    int
	}
	var opts []opt
	total := 0
	for name, w := range c.cfg.Behavior.Weights {
		if w <= 0 {
			continue
		}
		switch name {
		case "wander":
			if !c.cfg.Behavior.Wander || !c.skin.Has("walk") {
				continue
			}
		default:
			if !c.skin.Has(name) {
				continue
			}
		}
		opts = append(opts, opt{name, w})
		total += w
	}
	if total == 0 {
		return
	}
	// Deterministic order is irrelevant; the draw is weighted random.
	r := c.rnd.Intn(total)
	for _, o := range opts {
		if r < o.w {
			c.runBehavior(o.name)
			return
		}
		r -= o.w
	}
}

func (c *Core) runBehavior(name string) {
	switch name {
	case "wander":
		c.doMove(&proto.Move{To: "random"}, "walk", PrioIdle, 0)
	case "sit":
		c.playAnim("sit", PrioIdle, 6000+c.rnd.Intn(9000), nil)
	default:
		c.playAnim(name, PrioIdle, 0, nil)
	}
}

// ---- messages to the renderer ------------------------------------------------------

func (c *Core) sendConfig() {
	c.sink.Render(map[string]any{
		"t":            "config",
		"skin":         c.curSkin,
		"scale":        c.cfg.Scale,
		"gravity":      c.cfg.Gravity,
		"floor_margin": c.cfg.FloorMargin,
		"idle_marks":   c.cfg.IdleMarks,
		"sensors":      c.cfg.Sensors,
		"sounds":       c.cfg.Sounds,
		"bubble_ttl":   c.cfg.BubbleTTL,
		"walk_speed":   c.cfg.Behavior.WalkSpeed,
	})
}

func (c *Core) setMenu(plugin string, add bool, it proto.MenuItem) {
	for i, m := range c.menu {
		if m.Plugin == plugin && m.ID == it.ID {
			if add {
				c.menu[i].Label = it.Label
			} else {
				c.menu = append(c.menu[:i], c.menu[i+1:]...)
			}
			c.sendMenu()
			return
		}
	}
	if add {
		c.menu = append(c.menu, it)
		c.sendMenu()
	}
}

func (c *Core) sendMenu() {
	items := make([]proto.MenuItem, 0, len(c.menu)+1)
	if c.sink.HasBrain() {
		items = append(items, proto.MenuItem{ID: "chat", Label: "聊天…", Local: "chat"})
	}
	if m, ok := c.skinMenu(); ok {
		items = append(items, m)
	}
	items = append(items, c.channelMenus()...)
	items = append(items, c.menu...)
	c.publish()
	c.sink.Render(map[string]any{"t": "menu", "items": items})
}

// PluginChanged must be called when a plugin connects or disconnects so the
// "chat" menu entry follows the presence of a brain plugin.
func (c *Core) PluginChanged() { c.do(c.sendMenu) }
