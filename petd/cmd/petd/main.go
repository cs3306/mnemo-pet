// petd is the core daemon of mnemo-pet.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"mnemo-pet/internal/api"
	"mnemo-pet/internal/config"
	"mnemo-pet/internal/core"
	"mnemo-pet/internal/hub"
	"mnemo-pet/internal/pluginhost"
	"mnemo-pet/internal/proto"
	"mnemo-pet/internal/rules"
	"mnemo-pet/internal/skin"
	"mnemo-pet/internal/state"
)

var version = "dev"

// bridge adapts hub callbacks to core calls.
type bridge struct{ c *core.Core }

func (b bridge) RenderUp()                            { b.c.RenderUp() }
func (b bridge) RenderDown()                          { b.c.RenderDown() }
func (b bridge) RenderEvent(ev proto.Event)           { b.c.Emit(ev) }
func (b bridge) PluginUp(string)                      { b.c.PluginChanged() }
func (b bridge) PluginDown(name string)               { b.c.DropPluginMenu(name); b.c.PluginChanged() }
func (b bridge) PluginEvent(_ string, ev proto.Event) { b.c.Emit(ev) }
func (b bridge) PluginAct(p string, a proto.Act)      { b.c.Act(a, "plugin:"+p) }
func (b bridge) PluginChannel(p string, add bool, ch proto.Channel) {
	b.c.PluginChannel(p, add, ch)
}
func (b bridge) PluginMenu(p string, add bool, it proto.MenuItem) {
	b.c.SetMenu(p, add, it)
}

func main() {
	dataDir := flag.String("data", envOr("PET_DATA_DIR", "./data"), "data directory (config.json, rules.json, plugin data)")
	showVersion := flag.Bool("version", false, "print version and exit")
	debug := flag.Bool("debug", os.Getenv("PET_DEBUG") != "", "debug logging")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	lvl := slog.LevelInfo
	if *debug {
		lvl = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))

	cfg, err := config.Load(*dataDir)
	if err != nil {
		fatal("config", err)
	}

	sk, dir, err := skin.Load([]string{filepath.Join(cfg.DataDir, "skins"), cfg.SkinsDir}, cfg.Skin)
	if err != nil {
		slog.Warn("skin manifest not found, using built-in animation list", "skin", cfg.Skin, "err", err)
		sk = skin.Fallback()
		sk.Name = cfg.Skin
	} else {
		slog.Info("skin loaded", "skin", sk.Name, "dir", dir, "anims", len(sk.Anims))
	}

	eng, err := rules.Load(cfg.RulesPath)
	if err != nil {
		fatal("rules", err)
	}
	slog.Info("rules loaded", "count", eng.Count(), "path", cfg.RulesPath)

	h := hub.New(cfg.Token, version)
	c := core.New(cfg, h, sk, eng)
	st, err := state.Open(cfg.StatePath)
	if err != nil {
		slog.Warn("state.json unreadable, starting fresh (file left untouched until next change)", "err", err)
		st = state.Memory()
	}
	c.Configure(core.Options{
		Store:        st,
		SkinDirs:     []string{filepath.Join(cfg.DataDir, "skins"), cfg.SkinsDir},
		WardrobePath: cfg.WardrobePath,
	})
	h.SetHandler(bridge{c})
	ph := pluginhost.New(cfg)
	apiSrv := &api.Server{Token: cfg.Token, Core: c, Hub: h, Plugins: ph, Version: version}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 3)
	go func() { c.Run(ctx) }()
	go func() { errc <- h.Serve(ctx, cfg.Listen) }()
	go func() { errc <- apiSrv.Serve(ctx, cfg.HTTPListen) }()
	done := make(chan struct{})
	go func() { ph.Run(ctx); close(done) }()

	slog.Info("petd started", "version", version, "tcp", cfg.Listen, "http", cfg.HTTPListen, "auth", cfg.Token != "")
	select {
	case err := <-errc:
		if err != nil {
			slog.Error("server failed", "err", err)
			stop()
		}
	case <-ctx.Done():
	}
	<-done
	slog.Info("bye")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func fatal(what string, err error) {
	slog.Error("startup failed", "what", what, "err", err)
	os.Exit(1)
}
