package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"mnemo-pet/internal/proto"
)

type rec struct {
	mu     sync.Mutex
	events []proto.Event
	acts   []proto.Act
	menus  []proto.MenuItem
	up     []string
	down   []string
}

func (r *rec) RenderUp()                 { r.mu.Lock(); r.up = append(r.up, "render"); r.mu.Unlock() }
func (r *rec) RenderDown()               { r.mu.Lock(); r.down = append(r.down, "render"); r.mu.Unlock() }
func (r *rec) RenderEvent(e proto.Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *rec) PluginUp(n string)         { r.mu.Lock(); r.up = append(r.up, n); r.mu.Unlock() }
func (r *rec) PluginDown(n string)       { r.mu.Lock(); r.down = append(r.down, n); r.mu.Unlock() }
func (r *rec) PluginEvent(_ string, e proto.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}
func (r *rec) PluginAct(_ string, a proto.Act) {
	r.mu.Lock()
	r.acts = append(r.acts, a)
	r.mu.Unlock()
}
func (r *rec) PluginMenu(_ string, add bool, it proto.MenuItem) {
	r.mu.Lock()
	if add {
		r.menus = append(r.menus, it)
	}
	r.mu.Unlock()
}

func start(t *testing.T, token string) (*Hub, *rec, string) {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	h := New(token, "test")
	r := &rec{}
	h.SetHandler(r)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Serve(ctx, addr)
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h, r, addr
}

type cli struct {
	c  net.Conn
	sc *bufio.Scanner
}

func dial(t *testing.T, addr string, hello map[string]any) *cli {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	b, _ := json.Marshal(hello)
	c.Write(append(b, '\n'))
	return &cli{c, bufio.NewScanner(c)}
}

func (c *cli) send(v any) { b, _ := json.Marshal(v); c.c.Write(append(b, '\n')) }

func (c *cli) read(t *testing.T) map[string]any {
	t.Helper()
	c.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if !c.sc.Scan() {
		t.Fatalf("read failed: %v", c.sc.Err())
	}
	var m map[string]any
	json.Unmarshal(c.sc.Bytes(), &m)
	return m
}

func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestPluginRoutingAndPrefix(t *testing.T) {
	h, r, addr := start(t, "")
	p := dial(t, addr, map[string]any{"t": "hello", "role": "plugin", "name": "demo",
		"subscribe": []string{"click", "time.*"}, "caps": []string{"brain"}})
	if m := p.read(t); m["t"] != "welcome" {
		t.Fatalf("%v", m)
	}
	waitFor(t, func() bool { return h.HasBrain() })

	p.send(map[string]any{"t": "event", "name": "hello", "data": map[string]any{"a": 1}})
	p.send(map[string]any{"t": "act", "say": "yo", "prio": 70})
	p.send(map[string]any{"t": "menu.add", "id": "m", "label": "M"})
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.events) == 1 && len(r.acts) == 1 && len(r.menus) == 1
	})
	if r.events[0].Name != "plugin.demo.hello" {
		t.Fatalf("plugin events must be namespaced, got %q", r.events[0].Name)
	}
	if r.acts[0].Say != "yo" || r.acts[0].Prio != 70 {
		t.Fatalf("%+v", r.acts[0])
	}

	h.Plugins(proto.Event{Name: "click"})
	h.Plugins(proto.Event{Name: "focus"}) // not subscribed
	h.Plugins(proto.Event{Name: "time"})  // "time.*" must not match bare "time"
	h.Plugins(proto.Event{Name: "time.tick"})
	if m := p.read(t); m["name"] != "click" {
		t.Fatalf("%v", m)
	}
	if m := p.read(t); m["name"] != "time.tick" {
		t.Fatalf("%v", m)
	}
}

func TestRenderAndTokenAndDuplicate(t *testing.T) {
	h, r, addr := start(t, "sekret")

	bad := dial(t, addr, map[string]any{"t": "hello", "role": "render", "token": "nope"})
	if m := bad.read(t); m["t"] != "error" {
		t.Fatalf("bad token must be rejected: %v", m)
	}

	rd := dial(t, addr, map[string]any{"t": "hello", "role": "render", "token": "sekret"})
	if m := rd.read(t); m["t"] != "welcome" {
		t.Fatalf("%v", m)
	}
	waitFor(t, func() bool { return h.HasRender() })
	rd.send(map[string]any{"t": "event", "name": "click", "data": map[string]any{"x": 1}})
	waitFor(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.events) == 1 })
	if r.events[0].Name != "click" { // renderer events are not prefixed
		t.Fatalf("%+v", r.events[0])
	}
	h.Render(map[string]any{"t": "say", "text": "x"})
	if m := rd.read(t); m["t"] != "say" {
		t.Fatalf("%v", m)
	}

	p1 := dial(t, addr, map[string]any{"t": "hello", "role": "plugin", "name": "dup", "token": "sekret"})
	p1.read(t)
	p2 := dial(t, addr, map[string]any{"t": "hello", "role": "plugin", "name": "dup", "token": "sekret"})
	if m := p2.read(t); m["t"] != "error" {
		t.Fatalf("duplicate plugin name must be rejected: %v", m)
	}

	rd.c.Close()
	waitFor(t, func() bool { return !h.HasRender() })
}

func TestMatch(t *testing.T) {
	cases := []struct {
		p, n string
		want bool
	}{
		{"*", "x", true}, {"click", "click", true}, {"click", "click2", false},
		{"time.*", "time.tick", true}, {"time.*", "time", false}, {"plugin.*", "plugin.a.b", true},
	}
	for _, c := range cases {
		if proto.Match(c.p, c.n) != c.want {
			t.Errorf("Match(%q,%q)", c.p, c.n)
		}
	}
}

func (r *rec) PluginChannel(string, bool, proto.Channel) {}
