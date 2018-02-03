package main

import (
	"fmt"
	"math"
)

const (
	radToDeg = 180 / math.Pi
	degToRad = math.Pi / 180
)

type webmerc struct {
	E, N float64
}

func (wm webmerc) String() string {
	return fmt.Sprintf("webmerc(E %v, N %v)", wm.E, wm.N)
}

type wgs84 struct {
	xlon float64
	ylat float64
}

func (loc wgs84) String() string {
	return fmt.Sprintf("wgs84(xlon %v, ylat %v)", loc.xlon, loc.ylat)
}

func (loc wgs84) Webmerc() (r webmerc) {
	// see page 41 of http://www.ogp.org.uk/pubs/373-07-2.pdf
	const a = 6378137.0
	λ := loc.xlon * degToRad
	φ := loc.ylat * degToRad
	r.E = a * λ
	r.N = a * math.Log(math.Tan(math.Pi/4+φ/2))
	return
}

type tile struct {
	x, y, z int
}

func (t tile) String() string {
	return fmt.Sprintf("tile(x %d, y %d, z %d)", t.x, t.y, t.z)
}

func (t tile) Wgs84() (r wgs84) {
	n := math.Pow(2, float64(t.z))
	r.xlon = float64(t.x)/n*360.0 - 180.0
	r.ylat = radToDeg * math.Atan(math.Sinh(math.Pi*(1-2*float64(t.y)/n)))
	return
}

// Webmerc returns the topleft webmercator coordinate (epsg3857) for the tile
func (t tile) Webmerc() webmerc {
	return t.Wgs84().Webmerc()
}

func (t tile) Next() tile {
	return tile{x: t.x + 1, y: t.y + 1, z: t.z}
}

func (t tile) WebmercSize() webmerc {
	s := t.Webmerc()
	e := t.Next().Webmerc()
	return webmerc{E: e.E - s.E, N: e.N - s.N}
}

func (wm webmerc) Tile(zoom int) tile {
	return wm.Wgs84().Tile(zoom)
}

func (wm webmerc) Add(o webmerc) webmerc {
	return webmerc{
		E: wm.E + o.E,
		N: wm.N + o.N,
	}
}

func (wm webmerc) Wgs84() wgs84 {
	xlon := wm.E / (6378137.0 * degToRad)

	// y := 6378137.0 * math.Log(math.Tan((math.Pi * 0.25) + (0.5 * lat * degToRad)))
	// calculate the inverse of y
	ylat := wm.N / 6378137.0
	ylat = math.Pow(math.E, ylat)
	ylat = math.Atan(ylat)
	ylat -= math.Pi * 0.25
	ylat /= (0.5 * degToRad)

	return wgs84{xlon: xlon, ylat: ylat}
}

// Tile returns the tile contains the wgs84 location for the given zoom level.
func (loc wgs84) Tile(zoom int) (t tile) {
	latRad := loc.ylat * degToRad
	n := math.Pow(2.0, float64(zoom))
	return tile{
		x: int((loc.xlon + 180.0) / 360.0 * n),
		y: int(math.Floor((1.0 - math.Log(math.Tan(latRad)+(1.0/math.Cos(latRad)))/math.Pi) / 2.0 * n)),
		z: zoom,
	}
}
