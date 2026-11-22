package wardrobe

import (
	"math"
	"time"
)

// SunStatus tells whether the sun rises and sets on a given day.
type SunStatus int

const (
	SunNormal     SunStatus = iota
	SunPolarDay             // sun never sets
	SunPolarNight           // sun never rises
)

const jd2000 = 2451545.0

// SunTimes returns sunrise and sunset as minutes after local midnight on the
// calendar day of t (in t's location), using the standard "sunrise equation"
// (accurate to a few minutes, plenty for choosing a skin).
//
// For polar day, rise=0 and set=1439; for polar night, rise=1439 and set=0, so
// that "sunset → sunrise" covers the whole day in winter at high latitudes.
func SunTimes(t time.Time, lat, lon float64) (rise, set int, st SunStatus) {
	y, m, d := t.Date()
	// Julian date of the calendar day at 12:00 UTC.
	noonUTC := time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
	jDate := float64(noonUTC.Unix())/86400.0 + 2440587.5
	n := math.Round(jDate - jd2000) // whole days since J2000.0 (noon UTC based)

	jStar := n - lon/360.0 // mean solar time at this longitude (east positive)
	M := math.Mod(357.5291+0.98560028*jStar, 360)
	Mr := rad(M)
	C := 1.9148*math.Sin(Mr) + 0.0200*math.Sin(2*Mr) + 0.0003*math.Sin(3*Mr)
	lambda := math.Mod(M+C+180+102.9372, 360)
	lr := rad(lambda)
	jTransit := jd2000 + jStar + 0.0053*math.Sin(Mr) - 0.0069*math.Sin(2*lr)
	sinDec := math.Sin(lr) * math.Sin(rad(23.4397))
	dec := math.Asin(sinDec)
	latR := rad(lat)
	cosH := (math.Sin(rad(-0.833)) - math.Sin(latR)*sinDec) / (math.Cos(latR) * math.Cos(dec))
	switch {
	case cosH > 1:
		return 1439, 0, SunPolarNight
	case cosH < -1:
		return 0, 1439, SunPolarDay
	}
	h := deg(math.Acos(cosH))
	jRise := jTransit - h/360.0
	jSet := jTransit + h/360.0
	return minuteOfDay(jRise, t.Location(), y, m, d), minuteOfDay(jSet, t.Location(), y, m, d), SunNormal
}

func rad(x float64) float64 { return x * math.Pi / 180 }
func deg(x float64) float64 { return x * 180 / math.Pi }

// minuteOfDay converts a Julian date to minutes after local midnight of the
// given calendar day (clamped into 0..1439).
func minuteOfDay(j float64, loc *time.Location, y int, m time.Month, d int) int {
	unix := (j - 2440587.5) * 86400.0
	tt := time.Unix(int64(math.Round(unix)), 0).In(loc)
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	mins := int(math.Round(tt.Sub(midnight).Minutes()))
	if mins < 0 {
		mins = 0
	}
	if mins > 1439 {
		mins = 1439
	}
	return mins
}
