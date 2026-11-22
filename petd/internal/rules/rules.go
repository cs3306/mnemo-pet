// Package rules is the declarative "event -> action" engine.
package rules

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"mnemo-pet/internal/proto"
)

type Rule struct {
	Name     string          `json:"name"`
	On       string          `json:"on"`
	Match    map[string]any  `json:"match"`
	Cooldown float64         `json:"cooldown"` // seconds
	Prob     *float64        `json:"prob"`
	Enabled  *bool           `json:"enabled"`
	Do       json.RawMessage `json:"do"`

	// Optional: makes the rule's acts a user-switchable speech channel.
	Channel string `json:"channel"`
	Label   string `json:"label"`
	Group   string `json:"group"`

	acts []proto.Act
}

type Engine struct {
	rules []*Rule
	last  map[string]time.Time
	rnd   *rand.Rand
}

func Parse(b []byte) (*Engine, error) {
	var rs []*Rule
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, err
	}
	for i, r := range rs {
		if r.Name == "" {
			r.Name = fmt.Sprintf("rule-%d", i)
		}
		if r.On == "" {
			return nil, fmt.Errorf("rule %q: missing \"on\"", r.Name)
		}
		if len(r.Do) == 0 {
			return nil, fmt.Errorf("rule %q: missing \"do\"", r.Name)
		}
		var many []proto.Act
		if err := json.Unmarshal(r.Do, &many); err != nil {
			var one proto.Act
			if err2 := json.Unmarshal(r.Do, &one); err2 != nil {
				return nil, fmt.Errorf("rule %q: bad \"do\": %w", r.Name, err2)
			}
			many = []proto.Act{one}
		}
		r.acts = many
	}
	return &Engine{rules: rs, last: map[string]time.Time{}, rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}, nil
}

func Load(path string) (*Engine, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	e, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return e, nil
}

// Channels lists the speech channels declared by rules.
func (e *Engine) Channels() []proto.Channel {
	var out []proto.Channel
	seen := map[string]bool{}
	for _, r := range e.rules {
		if r.Channel == "" || seen[r.Channel] {
			continue
		}
		seen[r.Channel] = true
		label := r.Label
		if label == "" {
			label = r.Name
		}
		group := r.Group
		if group == "" {
			group = "互动"
		}
		on := r.Enabled == nil || *r.Enabled
		out = append(out, proto.Channel{ID: r.Channel, Label: label, Group: group, Default: on})
	}
	return out
}

// Count returns the number of rules loaded.
func (e *Engine) Count() int { return len(e.rules) }

// Eval returns the acts triggered by an event.
func (e *Engine) Eval(ev proto.Event, now time.Time) []proto.Act {
	var out []proto.Act
	for _, r := range e.rules {
		if r.Enabled != nil && !*r.Enabled {
			continue
		}
		if !proto.Match(r.On, ev.Name) || !matchAll(r.Match, ev.Data) {
			continue
		}
		if r.Cooldown > 0 {
			if t, ok := e.last[r.Name]; ok && now.Sub(t) < time.Duration(r.Cooldown*float64(time.Second)) {
				continue
			}
		}
		if r.Prob != nil && e.rnd.Float64() >= *r.Prob {
			continue
		}
		e.last[r.Name] = now
		for _, a := range r.acts {
			a = expand(a, ev.Data)
			if a.Channel == "" {
				a.Channel = r.Channel
			}
			if len(a.SayAny) > 0 && a.Say == "" {
				a.Say = a.SayAny[e.rnd.Intn(len(a.SayAny))]
			}
			a.SayAny = nil
			out = append(out, a)
		}
	}
	return out
}

var ops = []string{">=", "<=", "!=", "~", ">", "<"}

func matchAll(m map[string]any, data map[string]any) bool {
	for k, want := range m {
		op, key := "=", k
		for _, o := range ops {
			if strings.HasSuffix(k, o) {
				op, key = o, strings.TrimSuffix(k, o)
				break
			}
		}
		got, ok := data[key]
		if !ok {
			return false
		}
		if !compare(op, got, want) {
			return false
		}
	}
	return true
}

func compare(op string, got, want any) bool {
	switch op {
	case "~":
		return strings.Contains(strings.ToLower(fmt.Sprint(got)), strings.ToLower(fmt.Sprint(want)))
	case "=":
		if g, ok1 := num(got); ok1 {
			if w, ok2 := num(want); ok2 {
				return g == w
			}
		}
		return fmt.Sprint(got) == fmt.Sprint(want)
	case "!=":
		return !compare("=", got, want)
	}
	g, ok1 := num(got)
	w, ok2 := num(want)
	if !ok1 || !ok2 {
		return false
	}
	switch op {
	case ">=":
		return g >= w
	case "<=":
		return g <= w
	case ">":
		return g > w
	case "<":
		return g < w
	}
	return false
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func expand(a proto.Act, data map[string]any) proto.Act {
	if len(data) == 0 {
		return a
	}
	pairs := make([]string, 0, len(data)*2)
	for k, v := range data {
		pairs = append(pairs, "{"+k+"}", fmt.Sprint(v))
	}
	r := strings.NewReplacer(pairs...)
	a.Say = r.Replace(a.Say)
	if len(a.SayAny) > 0 {
		cp := make([]string, len(a.SayAny))
		for i, s := range a.SayAny {
			cp[i] = r.Replace(s)
		}
		a.SayAny = cp
	}
	return a
}
