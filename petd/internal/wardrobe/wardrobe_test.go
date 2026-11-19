package wardrobe

import (
	"errors"
	"math"
	"testing"
	"time"
)

func at(y int, m time.Month, d, h, mi int, loc *time.Location) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, loc)
}

func within(t *testing.T, what string, got, want, tol int) {
	t.Helper()
	if math.Abs(float64(got-want)) > float64(tol) {
		t.Errorf("%s: got %02d:%02d want %02d:%02d (±%d min)", what, got/60, got%60, want/60, want%60, tol)
	}
}

func TestSunTimesKnownPlaces(t *testing.T) {
	bst := time.FixedZone("BST", 3600)
	r, s, st := SunTimes(at(2024, 6, 21, 12, 0, bst), 51.5074, -0.1278) // London, summer solstice
	if st != SunNormal {
		t.Fatal("London must have a normal day")
	}
	within(t, "London sunrise", r, 4*60+43, 4)
	within(t, "London sunset", s, 21*60+21, 4)

	est := time.FixedZone("EST", -5*3600)
	r, s, _ = SunTimes(at(2024, 12, 21, 12, 0, est), 40.7128, -74.0060) // New York, winter solstice
	within(t, "NYC sunrise", r, 7*60+17, 4)
	within(t, "NYC sunset", s, 16*60+32, 4)
}

func TestSunTimesPolar(t *testing.T) {
	utc1 := time.FixedZone("CET", 3600)
	if _, _, st := SunTimes(at(2024, 12, 21, 12, 0, utc1), 69.65, 18.96); st != SunPolarNight {
		t.Errorf("Tromsø in December should be polar night, got %v", st)
	}
	if _, _, st := SunTimes(at(2024, 6, 21, 12, 0, utc1), 69.65, 18.96); st != SunPolarDay {
		t.Errorf("Tromsø in June should be polar day, got %v", st)
	}
}

func TestTimeRanges(t *testing.T) {
	w := When{From: "22:00", To: "06:00"}
	for _, c := range []struct {
		h, m int
		want bool
	}{{22, 0, true}, {23, 59, true}, {0, 0, true}, {5, 59, true}, {6, 0, false}, {12, 0, false}, {21, 59, false}} {
		got, err := w.Match(Env{Now: at(2026, 3, 1, c.h, c.m, time.UTC)})
		if err != nil || got != c.want {
			t.Errorf("%02d:%02d got %v err %v want %v", c.h, c.m, got, err, c.want)
		}
	}
	day := When{From: "08:00", To: "17:30"}
	if ok, _ := day.Match(Env{Now: at(2026, 3, 1, 17, 29, time.UTC)}); !ok {
		t.Error("17:29 inside 08:00-17:30")
	}
	if ok, _ := day.Match(Env{Now: at(2026, 3, 1, 17, 30, time.UTC)}); ok {
		t.Error("17:30 is exclusive end")
	}
}

func TestSunTokensNeedLocationAndWork(t *testing.T) {
	night := When{From: "sunset", To: "sunrise"}
	noLoc := Env{Now: at(2024, 6, 21, 23, 0, time.UTC)}
	if _, err := night.Match(noLoc); !errors.Is(err, ErrNeedsLocation) {
		t.Fatalf("want ErrNeedsLocation, got %v", err)
	}
	bst := time.FixedZone("BST", 3600)
	env := func(h, m int) Env {
		return Env{Now: at(2024, 6, 21, h, m, bst), Lat: 51.5074, Lon: -0.1278, HasLoc: true}
	}
	for _, c := range []struct {
		h, m int
		want bool
	}{{23, 0, true}, {3, 0, true}, {12, 0, false}, {20, 0, false}, {22, 30, true}} {
		got, err := night.Match(env(c.h, c.m))
		if err != nil || got != c.want {
			t.Errorf("%02d:%02d night got %v err %v want %v", c.h, c.m, got, err, c.want)
		}
	}
	// offsets: "sunset-1h" starts an hour before sunset (~20:21 in London on the solstice)
	early := When{From: "sunset-1h", To: "sunrise"}
	if ok, _ := early.Match(env(20, 45)); !ok {
		t.Error("20:45 should be inside sunset-1h..sunrise")
	}
	if ok, _ := early.Match(env(19, 50)); ok {
		t.Error("19:50 is before sunset-1h")
	}
}

func TestDatesWeekdaysMonthsAreANDed(t *testing.T) {
	w := When{Dates: []string{"12-20..01-05"}, Weekdays: []int{6, 7}}
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at(2026, 12, 26, 12, 0, time.UTC), true},  // Saturday inside the wrapped range
		{at(2026, 12, 25, 12, 0, time.UTC), false}, // Friday
		{at(2027, 1, 3, 12, 0, time.UTC), true},    // Sunday, after the wrap
		{at(2027, 1, 9, 12, 0, time.UTC), false},   // Saturday outside the range
	}
	for i, c := range cases {
		if got, err := w.Match(Env{Now: c.t}); err != nil || got != c.want {
			t.Errorf("case %d: got %v err %v want %v", i, got, err, c.want)
		}
	}
	full := When{Dates: []string{"2026-12-24..2026-12-26", "2027-02-14"}}
	if ok, _ := full.Match(Env{Now: at(2027, 2, 14, 9, 0, time.UTC)}); !ok {
		t.Error("explicit full date should match")
	}
	if ok, _ := full.Match(Env{Now: at(2028, 2, 14, 9, 0, time.UTC)}); ok {
		t.Error("full dates must not recur")
	}
	if ok, _ := (When{Months: []int{12}}).Match(Env{Now: at(2026, 12, 1, 0, 0, time.UTC)}); !ok {
		t.Error("month")
	}
}

func TestHolidays(t *testing.T) {
	utc := time.UTC
	yes := func(name string, y int, m time.Month, d int) {
		t.Helper()
		if ok, err := (When{Holidays: []string{name}}).Match(Env{Now: at(y, m, d, 10, 0, utc)}); err != nil || !ok {
			t.Errorf("%s should include %d-%02d-%02d (err %v)", name, y, m, d, err)
		}
	}
	no := func(name string, y int, m time.Month, d int) {
		t.Helper()
		if ok, _ := (When{Holidays: []string{name}}).Match(Env{Now: at(y, m, d, 10, 0, utc)}); ok {
			t.Errorf("%s should not include %d-%02d-%02d", name, y, m, d)
		}
	}
	yes("christmas", 2026, 12, 24)
	yes("christmas", 2026, 12, 26)
	no("christmas", 2026, 12, 27)
	yes("new_year", 2026, 12, 31) // wraps into the next year
	yes("new_year", 2027, 1, 2)
	no("new_year", 2027, 1, 3)
	yes("halloween", 2026, 10, 31)
	no("halloween", 2026, 10, 24)
	yes("easter", 2026, 4, 5) // Easter Sunday 2026
	yes("easter", 2026, 4, 3) // Good Friday
	yes("easter", 2026, 4, 6) // Easter Monday
	no("easter", 2026, 4, 7)
	yes("easter", 2027, 3, 28)
	yes("thanksgiving_ca", 2026, 10, 12)
	no("thanksgiving_ca", 2026, 10, 5)
	yes("thanksgiving_us", 2026, 11, 26)
	yes("lunar_new_year", 2027, 2, 5) // eve
	yes("lunar_new_year", 2027, 2, 11)
	no("lunar_new_year", 2027, 2, 12)
	yes("lunar_new_year", 2028, 1, 26)
	no("lunar_new_year", 2040, 2, 1) // outside the table: never matches
	yes("mid_autumn", 2026, 9, 25)
	yes("mid_autumn", 2028, 10, 3)
	yes("dragon_boat", 2026, 6, 19)
	no("dragon_boat", 2040, 6, 19)

	if _, err := (When{Holidays: []string{"nonsense"}}).Match(Env{Now: time.Now()}); err == nil {
		t.Error("unknown holiday must be an error")
	}
}

func TestValidate(t *testing.T) {
	bad := []When{
		{},
		{From: "22:00"},
		{From: "25:00", To: "01:00"},
		{From: "sunset+xyz", To: "sunrise"},
		{Dates: []string{"13-40"}},
		{Dates: []string{"2026-01-01..01-05"}},
		{Weekdays: []int{0}},
		{Months: []int{13}},
		{Holidays: []string{"???"}},
	}
	for i, w := range bad {
		if w.Validate() == nil {
			t.Errorf("case %d should be invalid: %+v", i, w)
		}
	}
	good := When{From: "sunset+30m", To: "06:00", Dates: []string{"10-31", "2026-12-24..2026-12-26"}, Holidays: []string{"christmas"}, Weekdays: []int{1, 7}, Months: []int{12}}
	if err := good.Validate(); err != nil {
		t.Errorf("good: %v", err)
	}
}

func TestPickPriorityOrderAndSkips(t *testing.T) {
	off := false
	now := Env{Now: at(2026, 12, 25, 23, 0, time.UTC)}
	entries := []Entry{
		{Name: "night", Skin: "night", Priority: 10, When: When{From: "22:00", To: "06:00"}},
		{Name: "xmas", Skin: "christmas", Priority: 50, When: When{Holidays: []string{"christmas"}}},
		{Name: "disabled", Skin: "x", Priority: 99, Enabled: &off, When: When{Months: []int{12}}},
		{Name: "needs-sun", Skin: "sun", Priority: 80, When: When{From: "sunset", To: "sunrise"}},
	}
	e, ok, errs := Pick(entries, now)
	if !ok || e.Skin != "christmas" {
		t.Fatalf("holiday (50) must beat night (10): %+v", e)
	}
	if len(errs) != 1 {
		t.Fatalf("the sun entry should have reported one error, got %v", errs)
	}
	if _, ok, _ := Pick(entries[:1], Env{Now: at(2026, 6, 1, 12, 0, time.UTC)}); ok {
		t.Fatal("nothing should match at noon in June")
	}
	// equal priority: file order wins
	tie := []Entry{
		{Skin: "a", Priority: 5, When: When{Months: []int{6}}},
		{Skin: "b", Priority: 5, When: When{Months: []int{6}}},
	}
	if e, _, _ := Pick(tie, Env{Now: at(2026, 6, 1, 0, 0, time.UTC)}); e.Skin != "a" {
		t.Fatalf("tie must keep file order, got %s", e.Skin)
	}
}
