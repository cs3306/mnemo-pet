package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistAndReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Get().SkinMode != ModeAuto {
		t.Fatal("default mode must be auto")
	}
	if err := s.Update(func(st *State) {
		st.SkinMode, st.Skin = ModeManual, "night"
		st.Channels = map[string]bool{"report.cpu": true}
	}); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	g := s2.Get()
	if g.SkinMode != ModeManual || g.Skin != "night" || !g.Channels["report.cpu"] {
		t.Fatalf("%+v", g)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	s := Memory()
	s.Update(func(st *State) { st.Channels = map[string]bool{"a": true} })
	g := s.Get()
	g.Channels["a"] = false
	if !s.Get().Channels["a"] {
		t.Fatal("Get must not expose internal map")
	}
}

func TestCorruptFileIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte("{nope"), 0o644)
	if _, err := Open(p); err == nil {
		t.Fatal("expected error")
	}
}
