package core

import (
	"testing"
	"time"

	"mnemo-pet/internal/proto"
)

func say(c *Core, ch, text string) {
	c.applyAct(proto.Act{Say: text, Channel: ch, Prio: PrioPlugin}, "t", PrioPlugin)
}

func said(s *fakeSink) (out []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.render {
		if m["t"] == "say" {
			out = append(out, m["text"].(string))
		}
	}
	return
}

func TestChannelDefaultOffThenEnabled(t *testing.T) {
	c, s, clk := newTestCore(t, `[]`)
	c.PluginChannel("sysmon", true, proto.Channel{ID: "report.cpu", Label: "CPU", Group: "系统播报", Default: false})
	drain(c)
	say(c, "report.cpu", "cpu 10%")
	if len(said(s)) != 0 {
		t.Fatal("default-off channel must be silent")
	}
	c.handle(proto.Event{Name: "menu.click", Data: map[string]any{"id": "channel:report.cpu", "checked": true}})
	clk.t = clk.t.Add(time.Minute)
	say(c, "report.cpu", "cpu 11%")
	if got := said(s); len(got) != 1 {
		t.Fatalf("enabled channel should speak: %v", got)
	}
	c.handle(proto.Event{Name: "menu.click", Data: map[string]any{"id": "channel:report.cpu", "checked": false}})
	clk.t = clk.t.Add(time.Minute)
	say(c, "report.cpu", "cpu 12%")
	if len(said(s)) != 1 {
		t.Fatal("disabling must silence it again")
	}
}

func TestDisabledChannelDropsAnimAndSoundToo(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	c.registerChannel(proto.Channel{ID: "x", Default: false})
	c.applyAct(proto.Act{Anim: "wave", Sound: "pop", Say: "hi", Channel: "x"}, "t", PrioPlugin)
	if len(s.anims()) != 0 || len(said(s)) != 0 || s.last("sound") != nil {
		t.Fatal("a disabled channel must drop the whole act")
	}
}

func TestChannelStatePersistsAcrossPluginReconnect(t *testing.T) {
	c, _, _ := newTestCore(t, `[]`)
	c.PluginChannel("p", true, proto.Channel{ID: "a", Default: true})
	drain(c)
	c.setChannel("a", false)
	c.DropPluginMenu("p")
	drain(c)
	if len(c.channelList()) != 0 {
		t.Fatal("plugin gone → its channels leave the menu")
	}
	c.PluginChannel("p", true, proto.Channel{ID: "a", Default: true})
	drain(c)
	if c.chOn("a") {
		t.Fatal("user's choice must survive a reconnect")
	}
}

func TestUndeclaredChannelAutoRegistered(t *testing.T) {
	c, s, _ := newTestCore(t, `[]`)
	say(c, "mystery", "boo")
	if len(said(s)) != 1 {
		t.Fatal("undeclared channels default to on")
	}
	l := c.channelList()
	if len(l) != 1 || l[0].Group != otherGroup {
		t.Fatalf("%+v", l)
	}
}

func TestRuleChannelRegisteredAndGated(t *testing.T) {
	c, s, _ := newTestCore(t, `[{"name":"greet","on":"click","channel":"rule.greet","label":"点击问候","do":{"say":"hi"}}]`)
	if l := c.channelList(); len(l) != 1 || l[0].ID != "rule.greet" || l[0].Group != "互动" {
		t.Fatalf("%+v", l)
	}
	c.setChannel("rule.greet", false)
	c.handle(proto.Event{Name: "click"})
	if len(said(s)) != 0 {
		t.Fatal("rule on a disabled channel must stay quiet")
	}
}

func TestChannelMenuGroupsWithHeaders(t *testing.T) {
	c, _, _ := newTestCore(t, `[]`)
	c.registerChannel(proto.Channel{ID: "a", Label: "A", Group: "G1", Default: true})
	c.registerChannel(proto.Channel{ID: "b", Label: "B", Group: "G2"})
	ms := c.channelMenus()
	if len(ms) != 1 {
		t.Fatalf("%+v", ms)
	}
	m := ms[0]
	if len(m.Children) != 4 || m.Children[0].Type != "header" || m.Children[1].Type != "switch" || !m.Children[1].Checked || m.Children[3].Checked {
		t.Fatalf("%+v", m)
	}
}

func TestChannelMenuOneEntryPerMachine(t *testing.T) {
	c, _, _ := newTestCore(t, `[]`)
	c.registerChannel(proto.Channel{ID: "a", Label: "A", Group: "互动", Default: true})
	c.registerChannel(proto.Channel{ID: "b", Label: "B", Group: "homelab · 系统播报"})
	c.registerChannel(proto.Channel{ID: "c", Label: "C", Group: "homelab · 系统告警"})
	c.registerChannel(proto.Channel{ID: "d", Label: "D", Group: "S600 · 系统播报"})
	ms := c.channelMenus()
	if len(ms) != 3 || ms[0].Label != "提醒与问候" {
		t.Fatalf("%+v", ms)
	}
	for _, m := range ms {
		if m.Label == "homelab · 说话内容" && (len(m.Children) != 4 || m.Children[0].Label != "系统告警" && m.Children[0].Label != "系统播报") {
			t.Fatalf("%+v", m)
		}
	}
}

// drain runs queued jobs (PluginChannel etc. use c.do).
func drain(c *Core) {
	for {
		select {
		case f := <-c.jobs:
			f()
		default:
			return
		}
	}
}
