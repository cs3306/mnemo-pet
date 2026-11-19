// Package wardrobe decides which skin the pet should wear: by time-of-day
// ranges (including sunrise/sunset), calendar dates, holidays, weekdays and
// months. It is pure logic with no I/O, so it is easy to test.
package wardrobe

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// When is a set of conditions. All present condition kinds must hold (AND);
// within a list, any element may hold (OR). At least one kind must be present.
type When struct {
	From     string   `json:"from,omitempty"`     // "HH:MM" | "sunrise" | "sunset" [+/-duration, e.g. "sunset+30m"]
	To       string   `json:"to,omitempty"`       // same syntax; may be earlier than From (wraps midnight)
	Dates    []string `json:"dates,omitempty"`    // "MM-DD", "YYYY-MM-DD", or ranges "A..B"
	Holidays []string `json:"holidays,omitempty"` // built-in names, see HolidayNames
	Weekdays []int    `json:"weekdays,omitempty"` // 1=Mon … 7=Sun
	Months   []int    `json:"months,omitempty"`   // 1..12
}

// Entry associates a skin with a When.
type Entry struct {
	Name     string `json:"name,omitempty"`
	Skin     string `json:"skin"`
	Priority int    `json:"priority,omitempty"` // higher wins; 0 = default for the source
	Enabled  *bool  `json:"enabled,omitempty"`
	When     When   `json:"when"`
}

// File is the on-disk wardrobe.json.
type File struct {
	Enabled *bool `json:"enabled,omitempty"` // false = never switch automatically
	// UseSkinSchedules: also honour the "when" found in each skin's pet.json (default true).
	UseSkinSchedules *bool   `json:"use_skin_schedules,omitempty"`
	Entries          []Entry `json:"entries"`
}

// Env is everything a condition may depend on.
type Env struct {
	Now    time.Time
	Lat    float64
	Lon    float64
	HasLoc bool
}

// ErrNeedsLocation is returned when a sunrise/sunset condition cannot be evaluated.
var ErrNeedsLocation = fmt.Errorf("sunrise/sunset need \"location\" in config.json")

var sunTok = regexp.MustCompile(`^(sunrise|sunset)\s*([+-].+)?$`)

// LoadFile reads and validates a wardrobe file. A missing file is not an error.
func LoadFile(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	for i, e := range f.Entries {
		if e.Skin == "" {
			return f, fmt.Errorf("%s: entry %d has no skin", path, i)
		}
		if err := e.When.Validate(); err != nil {
			return f, fmt.Errorf("%s: entry %q: %w", path, label(e), err)
		}
	}
	return f, nil
}

func label(e Entry) string {
	if e.Name != "" {
		return e.Name
	}
	return e.Skin
}

// Validate checks syntax without evaluating anything.
func (w When) Validate() error {
	if w.empty() {
		return fmt.Errorf("\"when\" has no conditions")
	}
	if (w.From == "") != (w.To == "") {
		return fmt.Errorf("\"from\" and \"to\" must be given together")
	}
	if w.From != "" {
		if _, _, err := parseClock(w.From); err != nil {
			return err
		}
		if _, _, err := parseClock(w.To); err != nil {
			return err
		}
	}
	for _, d := range w.Dates {
		if _, err := parseDateSpec(d); err != nil {
			return err
		}
	}
	for _, h := range w.Holidays {
		if _, err := holidayMatch(h, time.Now()); err != nil {
			return err
		}
	}
	for _, d := range w.Weekdays {
		if d < 1 || d > 7 {
			return fmt.Errorf("weekday %d out of 1..7", d)
		}
	}
	for _, m := range w.Months {
		if m < 1 || m > 12 {
			return fmt.Errorf("month %d out of 1..12", m)
		}
	}
	return nil
}

func (w When) empty() bool {
	return w.From == "" && w.To == "" && len(w.Dates) == 0 && len(w.Holidays) == 0 &&
		len(w.Weekdays) == 0 && len(w.Months) == 0
}

// Match evaluates the conditions.
func (w When) Match(env Env) (bool, error) {
	if w.empty() {
		return false, nil
	}
	now := env.Now
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if len(w.Months) > 0 && !containsInt(w.Months, int(now.Month())) {
		return false, nil
	}
	if len(w.Weekdays) > 0 {
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7
		}
		if !containsInt(w.Weekdays, wd) {
			return false, nil
		}
	}
	if len(w.Dates) > 0 {
		hit := false
		for _, spec := range w.Dates {
			ds, err := parseDateSpec(spec)
			if err != nil {
				return false, err
			}
			if ds.contains(date) {
				hit = true
				break
			}
		}
		if !hit {
			return false, nil
		}
	}
	if len(w.Holidays) > 0 {
		hit := false
		for _, h := range w.Holidays {
			ok, err := holidayMatch(h, date)
			if err != nil {
				return false, err
			}
			if ok {
				hit = true
				break
			}
		}
		if !hit {
			return false, nil
		}
	}
	if w.From != "" {
		from, err := resolveClock(w.From, env)
		if err != nil {
			return false, err
		}
		to, err := resolveClock(w.To, env)
		if err != nil {
			return false, err
		}
		cur := now.Hour()*60 + now.Minute()
		if from <= to {
			if !(cur >= from && cur < to) {
				return false, nil
			}
		} else if !(cur >= from || cur < to) { // wraps midnight
			return false, nil
		}
	}
	return true, nil
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// ---- clock tokens -------------------------------------------------------------

// parseClock validates a token: returns (minutes, isSun, err); for sun tokens
// minutes is the offset in minutes.
func parseClock(tok string) (int, bool, error) {
	tok = strings.ToLower(strings.TrimSpace(tok))
	if m := sunTok.FindStringSubmatch(tok); m != nil {
		off := 0
		if m[2] != "" {
			d, err := time.ParseDuration(strings.ReplaceAll(m[2], " ", ""))
			if err != nil {
				return 0, true, fmt.Errorf("bad offset in %q: %v", tok, err)
			}
			off = int(d.Minutes())
		}
		return off, true, nil
	}
	parts := strings.Split(tok, ":")
	if len(parts) != 2 {
		return 0, false, fmt.Errorf("bad time %q (want HH:MM, sunrise or sunset)", tok)
	}
	h, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 24 || mi < 0 || mi > 59 || (h == 24 && mi != 0) {
		return 0, false, fmt.Errorf("bad time %q", tok)
	}
	return h*60 + mi, false, nil
}

func resolveClock(tok string, env Env) (int, error) {
	v, isSun, err := parseClock(tok)
	if err != nil {
		return 0, err
	}
	if !isSun {
		if v == 1440 {
			v = 0
		}
		return v, nil
	}
	if !env.HasLoc {
		return 0, ErrNeedsLocation
	}
	rise, set, _ := SunTimes(env.Now, env.Lat, env.Lon)
	base := rise
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(tok)), "sunset") {
		base = set
	}
	return ((base+v)%1440 + 1440) % 1440, nil
}

// ---- date specs ---------------------------------------------------------------

type dateSpec struct {
	recurring bool
	from, to  int // YYYYMMDD for full dates, MMDD for recurring ones
}

func parseDateSpec(s string) (dateSpec, error) {
	a, b, hasRange := strings.Cut(strings.TrimSpace(s), "..")
	if !hasRange {
		b = a
	}
	pa, err := parseOneDate(a)
	if err != nil {
		return dateSpec{}, err
	}
	pb, err := parseOneDate(b)
	if err != nil {
		return dateSpec{}, err
	}
	if pa.year == 0 != (pb.year == 0) {
		return dateSpec{}, fmt.Errorf("date range %q mixes MM-DD and YYYY-MM-DD", s)
	}
	if pa.year == 0 {
		return dateSpec{recurring: true, from: pa.month*100 + pa.day, to: pb.month*100 + pb.day}, nil
	}
	return dateSpec{from: pa.year*10000 + pa.month*100 + pa.day, to: pb.year*10000 + pb.month*100 + pb.day}, nil
}

type oneDate struct{ year, month, day int }

func parseOneDate(s string) (oneDate, error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	var nums []int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return oneDate{}, fmt.Errorf("bad date %q (want MM-DD or YYYY-MM-DD)", s)
		}
		nums = append(nums, n)
	}
	var od oneDate
	switch len(nums) {
	case 2:
		od = oneDate{0, nums[0], nums[1]}
	case 3:
		od = oneDate{nums[0], nums[1], nums[2]}
	default:
		return oneDate{}, fmt.Errorf("bad date %q (want MM-DD or YYYY-MM-DD)", s)
	}
	if od.month < 1 || od.month > 12 || od.day < 1 || od.day > 31 {
		return oneDate{}, fmt.Errorf("bad date %q", s)
	}
	return od, nil
}

func (d dateSpec) contains(date time.Time) bool {
	if !d.recurring {
		cur := date.Year()*10000 + int(date.Month())*100 + date.Day()
		return cur >= d.from && cur <= d.to
	}
	cur := int(date.Month())*100 + date.Day()
	if d.from <= d.to {
		return cur >= d.from && cur <= d.to
	}
	return cur >= d.from || cur <= d.to // wraps the new year
}

// ---- selection ------------------------------------------------------------------

// Pick returns the winning entry: highest priority first, then file order.
// Entries that cannot be evaluated (e.g. sun conditions without a location) are
// skipped; their errors are returned so the caller can log them once.
func Pick(entries []Entry, env Env) (Entry, bool, []error) {
	idx := make([]int, 0, len(entries))
	for i, e := range entries {
		if e.Enabled == nil || *e.Enabled {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return entries[idx[a]].Priority > entries[idx[b]].Priority })
	var errs []error
	for _, i := range idx {
		e := entries[i]
		ok, err := e.When.Match(env)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", label(e), err))
			continue
		}
		if ok {
			return e, true, errs
		}
	}
	return Entry{}, false, errs
}
