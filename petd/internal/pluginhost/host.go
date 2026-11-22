// Package pluginhost launches and supervises plugins that live in the plugins
// directory (<dir>/<name>/plugin.json). Plugins connect back to petd over the
// regular TCP protocol, so externally started plugins work the same way.
package pluginhost

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"mnemo-pet/internal/config"
)

type Manifest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     []string          `json:"command"`
	Autostart   *bool             `json:"autostart"`
	Env         map[string]string `json:"env"`
}

type Status struct {
	Name     string `json:"name"`
	Running  bool   `json:"running"`
	PID      int    `json:"pid,omitempty"`
	Restarts int    `json:"restarts"`
	Note     string `json:"note,omitempty"`
}

type Host struct {
	cfg  *config.Config
	port string

	mu     sync.Mutex
	status map[string]*Status
	wg     sync.WaitGroup
}

func New(cfg *config.Config) *Host {
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil || port == "" {
		port = "23817"
	}
	return &Host{cfg: cfg, port: port, status: map[string]*Status{}}
}

// Discover reads every plugin manifest in the plugins directory.
func (h *Host) Discover() []Manifest {
	entries, err := os.ReadDir(h.cfg.PluginsDir)
	if err != nil {
		slog.Warn("plugins dir not readable", "dir", h.cfg.PluginsDir, "err", err)
		return nil
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() || e.Name()[0] == '_' || e.Name()[0] == '.' {
			continue
		}
		p := filepath.Join(h.cfg.PluginsDir, e.Name(), "plugin.json")
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			slog.Error("bad plugin manifest", "path", p, "err", err)
			continue
		}
		if m.Name == "" {
			m.Name = e.Name()
		}
		if len(m.Command) == 0 {
			slog.Error("plugin manifest has no command", "path", p)
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Run starts all enabled plugins and blocks until ctx ends and they exit.
func (h *Host) Run(ctx context.Context) {
	for _, m := range h.Discover() {
		m := m
		st := &Status{Name: m.Name}
		h.mu.Lock()
		h.status[m.Name] = st
		h.mu.Unlock()
		switch {
		case h.cfg.PluginDisabled(m.Name):
			h.setNote(m.Name, "disabled in config")
			slog.Info("plugin disabled in config", "plugin", m.Name)
			continue
		case m.Autostart != nil && !*m.Autostart:
			h.setNote(m.Name, "autostart off")
			continue
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			h.supervise(ctx, m)
		}()
	}
	h.wg.Wait()
}

func (h *Host) Statuses() []Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Status, 0, len(h.status))
	for _, s := range h.status {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (h *Host) update(name string, f func(*Status)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.status[name]; s != nil {
		f(s)
	}
}

func (h *Host) setNote(name, note string) { h.update(name, func(s *Status) { s.Note = note }) }

func (h *Host) env(m Manifest) []string {
	dataDir := filepath.Join(h.cfg.DataDir, "plugins", m.Name)
	os.MkdirAll(dataDir, 0o755)
	env := os.Environ()
	add := func(k, v string) { env = append(env, k+"="+v) }
	add("PET_HOST", "127.0.0.1")
	add("PET_PORT", h.port)
	add("PET_TOKEN", h.cfg.Token)
	add("PET_PLUGIN", m.Name)
	add("PET_DATA_DIR", dataDir)
	for k, v := range m.Env {
		add(k, v)
	}
	for k, v := range h.cfg.Plugins.Env[m.Name] {
		add(k, v)
	}
	return env
}

func (h *Host) supervise(ctx context.Context, m Manifest) {
	dir := filepath.Join(h.cfg.PluginsDir, m.Name)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	backoff := time.Second
	for {
		cmd := exec.CommandContext(ctx, m.Command[0], m.Command[1:]...)
		cmd.Dir = dir
		cmd.Env = h.env(m)
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = 3 * time.Second
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()

		started := time.Now()
		if err := cmd.Start(); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("plugin failed to start", "plugin", m.Name, "err", err)
			h.setNote(m.Name, "start failed: "+err.Error())
		} else {
			h.update(m.Name, func(s *Status) { s.Running, s.PID, s.Note = true, cmd.Process.Pid, "" })
			slog.Info("plugin started", "plugin", m.Name, "pid", cmd.Process.Pid)
			go pipeLog(m.Name, stdout)
			go pipeLog(m.Name, stderr)
			err := cmd.Wait()
			h.update(m.Name, func(s *Status) { s.Running, s.PID = false, 0 })
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				slog.Info("plugin exited cleanly, not restarting", "plugin", m.Name)
				h.setNote(m.Name, "exited (0)")
				return
			}
			slog.Warn("plugin crashed", "plugin", m.Name, "err", err)
			h.setNote(m.Name, "crashed: "+err.Error())
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		h.update(m.Name, func(s *Status) { s.Restarts++ })
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

func pipeLog(name string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		slog.Info(sc.Text(), "plugin", name)
	}
}
