// Package api serves the small HTTP control surface.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mnemo-pet/internal/core"
	"mnemo-pet/internal/hub"
	"mnemo-pet/internal/pluginhost"
	"mnemo-pet/internal/proto"
)

type Server struct {
	Token   string
	Core    *core.Core
	Hub     *hub.Hub
	Plugins *pluginhost.Host
	Version string
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" {
			got := r.URL.Query().Get("token")
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				got = strings.TrimPrefix(h, "Bearer ")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/api/state", s.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"version":  s.Version,
			"pet":      s.Core.Snapshot(),
			"clients":  s.Hub.PluginList(),
			"managed":  s.Plugins.Statuses(),
			"time_utc": time.Now().UTC().Format(time.RFC3339),
		})
	}))
	mux.HandleFunc("/api/skins", s.auth(func(w http.ResponseWriter, r *http.Request) {
		sn := s.Core.Snapshot()
		writeJSON(w, 200, map[string]any{"mode": sn.SkinMode, "current": sn.Skin, "skins": sn.Skins})
	}))
	mux.HandleFunc("/api/skin", s.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Skin string `json:"skin"`
			Auto bool   `json:"auto"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		switch {
		case req.Auto:
			s.Core.AutoSkin()
		case req.Skin != "":
			if !s.Core.SetSkin(req.Skin) {
				http.Error(w, "unknown skin", http.StatusNotFound)
				return
			}
		default:
			http.Error(w, `need {"skin":name} or {"auto":true}`, http.StatusBadRequest)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	}))
	mux.HandleFunc("/api/channels", s.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"channels": s.Core.Channels()})
	}))
	mux.HandleFunc("/api/channel", s.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID      string `json:"id"`
			Enabled *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || req.ID == "" || req.Enabled == nil {
			http.Error(w, `need {"id":..., "enabled":true|false}`, http.StatusBadRequest)
			return
		}
		if !s.Core.SetChannel(req.ID, *req.Enabled) {
			http.Error(w, "unknown channel", http.StatusNotFound)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	}))
	mux.HandleFunc("/api/act", s.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var a proto.Act
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&a); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.Core.ActHTTP(a)
		writeJSON(w, 202, map[string]string{"status": "accepted"})
	}))
	mux.HandleFunc("/api/event", s.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var ev proto.Event
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&ev); err != nil || ev.Name == "" {
			http.Error(w, "need {\"name\":...}", http.StatusBadRequest)
			return
		}
		s.Core.Emit(ev)
		writeJSON(w, 202, map[string]string{"status": "accepted"})
	}))
	// Server-sent events: everything the renderer is told. Handy for debugging.
	mux.HandleFunc("/api/stream", s.auth(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch, stop := s.Hub.Tap()
		defer stop()
		fmt.Fprint(w, ": connected\n\n")
		fl.Flush()
		for {
			select {
			case b := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}))
	return mux
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(sc)
	}()
	slog.Info("http listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
