package skin

import (
	"os"
	"path/filepath"
	"testing"
)

func mk(t *testing.T, root, name, body string) {
	t.Helper()
	d := filepath.Join(root, name)
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "pet.json"), []byte(body), 0o644)
}

const base = `"size":[8,8],"anims":{"idle":{"frames":["a.png"],"fps":1,"loop":true}}`

func TestScanFirstDirWinsAndLabels(t *testing.T) {
	user, bundled := t.TempDir(), t.TempDir()
	mk(t, user, "night", `{"name":"night","label":"我的夜晚",`+base+`}`)
	mk(t, bundled, "night", `{"name":"night","label":"夜晚",`+base+`}`)
	mk(t, bundled, "default", `{"name":"default",`+base+`}`)
	lib := Scan([]string{user, bundled, filepath.Join(user, "missing")})
	if len(lib.Order) != 2 || lib.Order[0] != "default" || lib.Order[1] != "night" {
		t.Fatalf("order %v", lib.Order)
	}
	if lib.Skins["night"].Label != "我的夜晚" {
		t.Fatalf("user dir must win, got %q", lib.Skins["night"].Label)
	}
	if lib.Skins["default"].Label != "default" {
		t.Fatal("label defaults to folder name")
	}
}

func TestScanBadScheduleIsIgnoredBadSkinSkipped(t *testing.T) {
	d := t.TempDir()
	mk(t, d, "ok", `{"name":"ok","when":{"holidays":["nonsense"]},`+base+`}`)
	mk(t, d, "broken", `{not json`)
	os.MkdirAll(filepath.Join(d, "empty-dir"), 0o755)
	lib := Scan([]string{d})
	if lib.Skins["ok"] == nil || lib.Skins["ok"].Manifest.When != nil {
		t.Fatal("skin stays selectable, its invalid schedule is dropped")
	}
	if lib.Skins["broken"] != nil || lib.Skins["empty-dir"] != nil {
		t.Fatal("broken/empty dirs must be skipped")
	}
	if len(lib.Warnings) != 2 {
		t.Fatalf("want a warning for the schedule and the broken skin, got %v", lib.Warnings)
	}
}
