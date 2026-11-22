package wardrobe

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Moving Chinese festival dates. Sources: published calendar tables (Chinese
// New Year 2026-2035, Mid-Autumn 2026-2035, Dragon Boat 2026-2032). Years that
// are not in a table simply never match; add explicit dates to a schedule
// entry for those.
var (
	lunarNewYear = map[int]string{
		2026: "02-17", 2027: "02-06", 2028: "01-26", 2029: "02-13", 2030: "02-03",
		2031: "01-23", 2032: "02-11", 2033: "01-31", 2034: "02-19", 2035: "02-08",
	}
	midAutumn = map[int]string{
		2026: "09-25", 2027: "09-15", 2028: "10-03", 2029: "09-22", 2030: "09-12",
		2031: "10-01", 2032: "09-19", 2033: "09-08", 2034: "09-27", 2035: "09-16",
	}
	dragonBoat = map[int]string{
		2026: "06-19", 2027: "06-09", 2028: "05-28", 2029: "06-16", 2030: "06-05",
		2031: "06-24", 2032: "06-12",
	}
)

// fixedHolidays are "MM-DD" or "MM-DD..MM-DD" (the end may wrap into the next year).
var fixedHolidays = map[string]string{
	"new_year":        "12-31..01-02",
	"valentine":       "02-13..02-14",
	"labour_day":      "05-01",
	"children_day":    "06-01",
	"canada_day":      "07-01",
	"halloween":       "10-25..10-31",
	"christmas":       "12-24..12-26",
	"national_day_cn": "10-01..10-07",
}

// HolidayNames lists every built-in holiday name.
func HolidayNames() []string {
	set := map[string]bool{
		"easter": true, "thanksgiving_ca": true, "thanksgiving_us": true,
		"lunar_new_year": true, "mid_autumn": true, "dragon_boat": true,
	}
	for k := range fixedHolidays {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type dayRange struct{ start, end time.Time } // inclusive calendar days, local midnight

func day(y int, m time.Month, d int, loc *time.Location) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func mdOf(s string) (time.Month, int, error) {
	var m, d int
	if _, err := fmt.Sscanf(s, "%d-%d", &m, &d); err != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, 0, fmt.Errorf("bad MM-DD %q", s)
	}
	return time.Month(m), d, nil
}

// holidayRanges returns the occurrence(s) of a holiday that start in year y.
func holidayRanges(name string, y int, loc *time.Location) ([]dayRange, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if spec, ok := fixedHolidays[name]; ok {
		a, b, _ := strings.Cut(spec, "..")
		if b == "" {
			b = a
		}
		am, ad, err := mdOf(a)
		if err != nil {
			return nil, err
		}
		bm, bd, err := mdOf(b)
		if err != nil {
			return nil, err
		}
		ey := y
		if bm < am || (bm == am && bd < ad) {
			ey = y + 1
		}
		return []dayRange{{day(y, am, ad, loc), day(ey, bm, bd, loc)}}, nil
	}
	switch name {
	case "easter":
		e := easter(y, loc)
		return []dayRange{{e.AddDate(0, 0, -2), e.AddDate(0, 0, 1)}}, nil // Good Friday .. Easter Monday
	case "thanksgiving_ca": // second Monday of October
		return one(nthWeekday(y, time.October, time.Monday, 2, loc)), nil
	case "thanksgiving_us": // fourth Thursday of November
		return one(nthWeekday(y, time.November, time.Thursday, 4, loc)), nil
	case "lunar_new_year": // New Year's Eve .. day 6
		if s, ok := lunarNewYear[y]; ok {
			m, d, _ := mdOf(s)
			c := day(y, m, d, loc)
			return []dayRange{{c.AddDate(0, 0, -1), c.AddDate(0, 0, 5)}}, nil
		}
		return nil, nil
	case "mid_autumn":
		if s, ok := midAutumn[y]; ok {
			m, d, _ := mdOf(s)
			return one(day(y, m, d, loc)), nil
		}
		return nil, nil
	case "dragon_boat":
		if s, ok := dragonBoat[y]; ok {
			m, d, _ := mdOf(s)
			return one(day(y, m, d, loc)), nil
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown holiday %q (known: %s)", name, strings.Join(HolidayNames(), ", "))
}

func one(t time.Time) []dayRange { return []dayRange{{t, t}} }

func nthWeekday(y int, m time.Month, wd time.Weekday, n int, loc *time.Location) time.Time {
	first := day(y, m, 1, loc)
	off := (int(wd) - int(first.Weekday()) + 7) % 7
	return first.AddDate(0, 0, off+7*(n-1))
}

// easter computes Western Easter Sunday (anonymous Gregorian algorithm).
func easter(y int, loc *time.Location) time.Time {
	a := y % 19
	b := y / 100
	c := y % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	dd := (h+l-7*m+114)%31 + 1
	return day(y, time.Month(month), dd, loc)
}

// holidayMatch reports whether date (local midnight) is inside holiday `name`.
func holidayMatch(name string, date time.Time) (bool, error) {
	for _, y := range []int{date.Year() - 1, date.Year()} { // ranges can wrap the new year
		rs, err := holidayRanges(name, y, date.Location())
		if err != nil {
			return false, err
		}
		for _, r := range rs {
			if !date.Before(r.start) && !date.After(r.end) {
				return true, nil
			}
		}
	}
	return false, nil
}
