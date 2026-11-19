// Package hub runs the TCP server that renderers and plugins connect to.
// Wire format: one JSON object per line, in both directions.
package hub

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"

	"mnemo-pet/internal/proto"
)

// Handler receives everything the hub learns from its clients.
type Handler interface {
	RenderUp()
	RenderDown()
	RenderEvent(ev proto.Event)
	PluginUp(name string)
	PluginDown(name string)
	PluginEvent(plugin string, ev proto.Event)
	PluginAct(plugin string, a proto.Act)
	PluginMenu(plugin string, add bool, item proto.MenuItem)
	PluginChannel(plugin string, add bool, ch proto.Channel)
}

type Client struct {
	Role   string
	Name   string
	Caps   []string
	Subs   []string
	Remote string

	conn net.Conn
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func (c *Client) send(b []byte) bool {
	select {
	case c.out <- b:
		return true
	default:
		return false
	}
}

func (c *Client) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

func (c *Client) hasCap(name string) bool {
	for _, x := range c.Caps {
		if x == name {
			return true
		}
	}
	return false
}

type PluginInfo struct {
	Name      string   `json:"name"`
	Caps      []string `json:"caps,omitempty"`
	Subscribe []string `json:"subscribe,omitempty"`
	Remote    string   `json:"remote"`
}

type Hub struct {
	token   string
	version string
	h       Handler

	mu      sync.RWMutex
	render  *Client
	plugins map[string]*Client
	taps    map[chan []byte]struct{}
}

func New(token, version string) *Hub {
	return &Hub{token: token, version: version, plugins: map[string]*Client{}, taps: map[chan []byte]struct{}{}}
}

func (h *Hub) SetHandler(x Handler) { h.h = x }

// ---- core.Sink implementation -------------------------------------------

func (h *Hub) Render(msg any) {
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("marshal render msg", "err", err)
		return
	}
	h.mu.RLock()
	r := h.render
	for t := range h.taps {
		select {
		case t <- b:
		default:
		}
	}
	h.mu.RUnlock()
	if r != nil && !r.send(b) {
		slog.Warn("render client too slow, message dropped")
	}
}

func (h *Hub) Plugins(ev proto.Event) {
	b, _ := json.Marshal(struct {
		T string `json:"t"`
		proto.Event
	}{"event", ev})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, p := range h.plugins {
		for _, pat := range p.Subs {
			if proto.Match(pat, ev.Name) {
				if !p.send(b) {
					slog.Warn("plugin too slow, event dropped", "plugin", p.Name, "event", ev.Name)
				}
				break
			}
		}
	}
}

func (h *Hub) HasRender() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.render != nil
}

func (h *Hub) HasBrain() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, p := range h.plugins {
		if p.hasCap("brain") {
			return true
		}
	}
	return false
}

func (h *Hub) PluginList() []PluginInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]PluginInfo, 0, len(h.plugins))
	for _, p := range h.plugins {
		out = append(out, PluginInfo{Name: p.Name, Caps: p.Caps, Subscribe: p.Subs, Remote: p.Remote})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Tap returns a channel receiving a copy of every message sent to the renderer.
func (h *Hub) Tap() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.taps[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.taps, ch)
		h.mu.Unlock()
	}
}

// ---- server ---------------------------------------------------------------

func (h *Hub) Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	slog.Info("hub listening", "addr", ln.Addr().String())
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go h.handle(ctx, conn)
	}
}

func (h *Hub) writeLine(conn net.Conn, v any) {
	b, _ := json.Marshal(v)
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	conn.Write(append(b, '\n'))
}

func (h *Hub) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1<<20)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if !sc.Scan() {
		return
	}
	var hello proto.In
	if err := json.Unmarshal(sc.Bytes(), &hello); err != nil || hello.T != "hello" {
		h.writeLine(conn, map[string]string{"t": "error", "msg": "first message must be hello"})
		return
	}
	if h.token != "" && subtle.ConstantTimeCompare([]byte(hello.Token), []byte(h.token)) != 1 {
		h.writeLine(conn, map[string]string{"t": "error", "msg": "bad token"})
		slog.Warn("rejected connection: bad token", "remote", conn.RemoteAddr().String())
		return
	}
	if hello.Role != "render" && hello.Role != "plugin" {
		h.writeLine(conn, map[string]string{"t": "error", "msg": "role must be render or plugin"})
		return
	}
	if hello.Role == "plugin" && hello.Name == "" {
		h.writeLine(conn, map[string]string{"t": "error", "msg": "plugin needs a name"})
		return
	}
	conn.SetReadDeadline(time.Time{})

	c := &Client{
		Role: hello.Role, Name: hello.Name, Caps: hello.Caps, Subs: hello.Subscribe,
		Remote: conn.RemoteAddr().String(), conn: conn,
		out: make(chan []byte, 128), done: make(chan struct{}),
	}

	h.mu.Lock()
	if c.Role == "render" {
		if h.render != nil {
			h.render.close()
		}
		h.render = c
	} else {
		if _, dup := h.plugins[c.Name]; dup {
			h.mu.Unlock()
			h.writeLine(conn, map[string]string{"t": "error", "msg": "plugin name already connected"})
			return
		}
		h.plugins[c.Name] = c
	}
	h.mu.Unlock()

	go func() { // writer
		for {
			select {
			case b := <-c.out:
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := conn.Write(append(b, '\n')); err != nil {
					c.close()
					return
				}
			case <-c.done:
				return
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.close()
		case <-c.done:
		}
	}()

	wb, _ := json.Marshal(map[string]any{"t": "welcome", "version": h.version, "role": c.Role})
	c.send(wb)
	slog.Info("client connected", "role", c.Role, "name", c.Name, "remote", c.Remote)

	if c.Role == "render" {
		h.h.RenderUp()
	} else {
		h.h.PluginUp(c.Name)
	}

	for sc.Scan() {
		var m proto.In
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			eb, _ := json.Marshal(map[string]string{"t": "error", "msg": "bad json"})
			c.send(eb)
			continue
		}
		h.dispatch(c, m)
	}

	c.close()
	h.mu.Lock()
	if c.Role == "render" {
		if h.render == c {
			h.render = nil
		}
	} else if h.plugins[c.Name] == c {
		delete(h.plugins, c.Name)
	}
	h.mu.Unlock()
	slog.Info("client disconnected", "role", c.Role, "name", c.Name)
	if c.Role == "render" {
		h.mu.RLock()
		still := h.render != nil
		h.mu.RUnlock()
		if !still {
			h.h.RenderDown()
		}
	} else {
		h.h.PluginDown(c.Name)
	}
}

func (h *Hub) dispatch(c *Client, m proto.In) {
	switch m.T {
	case "ping":
		b, _ := json.Marshal(map[string]string{"t": "pong"})
		c.send(b)
	case "event":
		if m.Name == "" {
			return
		}
		if c.Role == "render" {
			h.h.RenderEvent(proto.Event{Name: m.Name, Data: m.Data})
			return
		}
		name := m.Name
		prefix := "plugin." + c.Name + "."
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			name = prefix + name // plugins may not impersonate core events
		}
		h.h.PluginEvent(c.Name, proto.Event{Name: name, Data: m.Data})
	case "act":
		if c.Role == "plugin" {
			h.h.PluginAct(c.Name, m.Act)
		}
	case "menu.add":
		if c.Role == "plugin" && m.ID != "" {
			h.h.PluginMenu(c.Name, true, proto.MenuItem{ID: m.ID, Label: m.Label, Plugin: c.Name})
		}
	case "channel.add":
		if c.Role == "plugin" && m.ID != "" {
			on := m.Enabled == nil || *m.Enabled
			h.h.PluginChannel(c.Name, true, proto.Channel{ID: m.ID, Label: m.Label, Group: m.Group, Default: on})
		}
	case "channel.remove":
		if c.Role == "plugin" && m.ID != "" {
			h.h.PluginChannel(c.Name, false, proto.Channel{ID: m.ID})
		}
	case "menu.remove":
		if c.Role == "plugin" && m.ID != "" {
			h.h.PluginMenu(c.Name, false, proto.MenuItem{ID: m.ID, Plugin: c.Name})
		}
	}
}
