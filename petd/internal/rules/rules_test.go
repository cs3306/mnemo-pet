package rules

import (
	"testing"
	"time"

	"mnemo-pet/internal/proto"
)

func TestMatchOperators(t *testing.T) {
	e, err := Parse([]byte(`[
	 {"name":"a","on":"focus","match":{"app~":"FIRE"},"do":{"say":"fx {app}"}},
	 {"name":"b","on":"active","match":{"idle_sec>=":600},"do":{"say":"back"}},
	 {"name":"c","on":"time","match":{"hm":"12:00"},"do":[{"say":"noon"},{"anim":"wave"}]},
	 {"name":"d","on":"x","match":{"k!=":"v"},"do":{"say":"ne"}},
	 {"name":"off","on":"click","enabled":false,"do":{"say":"never"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cases := []struct {
		ev   proto.Event
		want []string
	}{
		{proto.Event{Name: "focus", Data: map[string]any{"app": "firefox"}}, []string{"fx firefox"}},
		{proto.Event{Name: "focus", Data: map[string]any{"app": "gedit"}}, nil},
		{proto.Event{Name: "active", Data: map[string]any{"idle_sec": float64(120)}}, nil},
		{proto.Event{Name: "active", Data: map[string]any{"idle_sec": float64(600)}}, []string{"back"}},
		{proto.Event{Name: "time", Data: map[string]any{"hm": "12:00"}}, []string{"noon", ""}},
		{proto.Event{Name: "x", Data: map[string]any{"k": "other"}}, []string{"ne"}},
		{proto.Event{Name: "x", Data: map[string]any{"k": "v"}}, nil},
		{proto.Event{Name: "click"}, nil},
	}
	for i, c := range cases {
		acts := e.Eval(c.ev, now)
		if len(acts) != len(c.want) {
			t.Fatalf("case %d: got %d acts, want %d (%+v)", i, len(acts), len(c.want), acts)
		}
		for j, a := range acts {
			if a.Say != c.want[j] {
				t.Errorf("case %d act %d: say=%q want %q", i, j, a.Say, c.want[j])
			}
		}
	}
}

func TestCooldownAndProb(t *testing.T) {
	e, err := Parse([]byte(`[
	 {"name":"cd","on":"click","cooldown":10,"do":{"say":"hi"}},
	 {"name":"never","on":"p","prob":0,"do":{"say":"x"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	if n := len(e.Eval(proto.Event{Name: "click"}, t0)); n != 1 {
		t.Fatalf("first click: %d", n)
	}
	if n := len(e.Eval(proto.Event{Name: "click"}, t0.Add(5*time.Second))); n != 0 {
		t.Fatalf("inside cooldown: %d", n)
	}
	if n := len(e.Eval(proto.Event{Name: "click"}, t0.Add(11*time.Second))); n != 1 {
		t.Fatalf("after cooldown: %d", n)
	}
	for i := 0; i < 50; i++ {
		if len(e.Eval(proto.Event{Name: "p"}, t0)) != 0 {
			t.Fatal("prob 0 fired")
		}
	}
}

func TestSayAnyPicksOne(t *testing.T) {
	e, _ := Parse([]byte(`[{"on":"c","do":{"say_any":["a","b"]}}]`))
	got := e.Eval(proto.Event{Name: "c"}, time.Now())
	if len(got) != 1 || (got[0].Say != "a" && got[0].Say != "b") {
		t.Fatalf("%+v", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{`[{"do":{}}]`, `[{"on":"x"}]`, `{"not":"a list"}`} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("expected error for %s", s)
		}
	}
}

func TestDefaultRulesParse(t *testing.T) {
	// Guard against shipping a broken default rules file.
	if _, err := Parse(defaultRulesForTest(t)); err != nil {
		t.Fatal(err)
	}
}
