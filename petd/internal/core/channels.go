package core

import (
	"log/slog"
	"sort"

	"mnemo-pet/internal/proto"
	"mnemo-pet/internal/state"
)

const otherGroup = "其它"

// ChannelInfo is one speech channel with its current on/off state.
type ChannelInfo struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Group   string `json:"group"`
	Default bool   `json:"default"`
	Enabled bool   `json:"enabled"`
	Plugin  string `json:"plugin,omitempty"`
}

// chOn: user override, else the declared default, else on.
func (c *Core) chOn(id string) bool {
	if v, ok := c.store.Get().Channels[id]; ok {
		return v
	}
	if ch, ok := c.channels[id]; ok {
		return ch.Default
	}
	return true
}

func (c *Core) registerChannel(ch proto.Channel) {
	if ch.Group == "" {
		ch.Group = otherGroup
	}
	if ch.Label == "" {
		ch.Label = ch.ID
	}
	if _, ok := c.channels[ch.ID]; !ok {
		c.chOrder = append(c.chOrder, ch.ID)
	}
	c.channels[ch.ID] = ch
}

// PluginChannel registers or removes a plugin-declared channel.
func (c *Core) PluginChannel(plugin string, add bool, ch proto.Channel) {
	c.do(func() {
		if add {
			if ch.ID == "" {
				return
			}
			ch.Plugin = plugin
			c.registerChannel(ch)
		} else {
			c.dropChannel(ch.ID, plugin)
		}
		c.sendMenu()
	})
}

func (c *Core) dropChannel(id, plugin string) {
	if cur, ok := c.channels[id]; ok && cur.Plugin == plugin {
		delete(c.channels, id)
		for i, x := range c.chOrder {
			if x == id {
				c.chOrder = append(c.chOrder[:i], c.chOrder[i+1:]...)
				break
			}
		}
	}
}

func (c *Core) dropPluginChannels(plugin string) {
	for _, id := range append([]string(nil), c.chOrder...) {
		c.dropChannel(id, plugin)
	}
}

func (c *Core) setChannel(id string, on bool) {
	if err := c.store.Update(func(s *state.State) {
		if s.Channels == nil {
			s.Channels = map[string]bool{}
		}
		s.Channels[id] = on
	}); err != nil {
		slog.Warn("saving state failed", "err", err)
	}
	c.sendMenu()
}

// SetChannel is the goroutine-safe setter used by the HTTP API. False = unknown id.
func (c *Core) SetChannel(id string, on bool) bool {
	done := make(chan bool, 1)
	c.do(func() {
		if _, ok := c.channels[id]; !ok {
			done <- false
			return
		}
		c.setChannel(id, on)
		done <- true
	})
	return <-done
}

// channelList returns channels grouped for display: group order of first
// appearance, with "其它" last.
func (c *Core) channelList() []ChannelInfo {
	var groups []string
	byGroup := map[string][]ChannelInfo{}
	for _, id := range c.chOrder {
		ch := c.channels[id]
		if _, ok := byGroup[ch.Group]; !ok {
			groups = append(groups, ch.Group)
		}
		byGroup[ch.Group] = append(byGroup[ch.Group], ChannelInfo{
			ID: ch.ID, Label: ch.Label, Group: ch.Group, Default: ch.Default,
			Enabled: c.chOn(id), Plugin: ch.Plugin,
		})
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[j] == otherGroup && groups[i] != otherGroup })
	var out []ChannelInfo
	for _, g := range groups {
		out = append(out, byGroup[g]...)
	}
	return out
}

func (c *Core) channelMenu() (proto.MenuItem, bool) {
	list := c.channelList()
	if len(list) == 0 {
		return proto.MenuItem{}, false
	}
	var kids []proto.MenuItem
	last := "\x00"
	for _, ch := range list {
		if ch.Group != last {
			kids = append(kids, proto.MenuItem{Type: "header", Label: ch.Group})
			last = ch.Group
		}
		kids = append(kids, proto.MenuItem{ID: "channel:" + ch.ID, Type: "switch", Label: ch.Label, Checked: ch.Enabled})
	}
	return proto.MenuItem{Label: "说话内容", Children: kids}, true
}

// Channels lists channels with state (goroutine-safe).
func (c *Core) Channels() []ChannelInfo {
	c.snapMu.RLock()
	defer c.snapMu.RUnlock()
	return append([]ChannelInfo(nil), c.snap.Channels...)
}
