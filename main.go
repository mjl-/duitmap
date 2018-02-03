package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"9fans.net/go/draw"
	"github.com/mjl-/duit"
)

type tileLink struct {
	tile    tile
	tileURL string
}

type tileImage struct {
	tileLink tileLink
	image    *draw.Image
	buf      []byte
}

var (
	tileimages chan tileImage
	dui        *duit.DUI
)

type tileMap struct {
	ZoomMin   int
	ZoomMax   int
	Zoom      int
	TileURL   string // with {x}, {y}, {z} placeholders for the right tile
	TileURL2x string // used for hiDPI screens
	Center    webmerc

	tileImages map[tileLink]*draw.Image
	tileFiles  map[tileLink][]byte
	tileBusy   map[tileLink]struct{}
	lastDraw   struct {
		orig image.Point
		size image.Point
	}

	prevM  draw.Mouse
	prevB1 draw.Mouse
}

func (ui *tileMap) zoom() int {
	z := ui.Zoom
	if z < ui.ZoomMin {
		z = ui.ZoomMin
	}
	if z > ui.ZoomMax {
		z = ui.ZoomMax
	}
	return z
}

func (ui *tileMap) Layout(dui *duit.DUI, self *duit.Kid, sizeAvail image.Point, force bool) {
	if dui.DebugLayout > 0 {
		log.Printf("tilemap Layout\n")
	}

	self.R = rect(sizeAvail)
}

func (ui *tileMap) tileConfig(hiDPI bool) (string, image.Point) {
	if hiDPI {
		return ui.TileURL2x, image.Pt(512, 512)
	}
	return ui.TileURL, image.Pt(256, 256)
}

func (ui *tileMap) Draw(dui *duit.DUI, self *duit.Kid, img *draw.Image, orig image.Point, m draw.Mouse, force bool) {
	if dui.DebugDraw > 0 {
		log.Printf("tilemap Draw\n")
	}

	hiDPI := dui.Scale(100) > 100
	tileURL, tileSize := ui.tileConfig(hiDPI)

	if ui.tileImages == nil {
		ui.tileImages = map[tileLink]*draw.Image{}
		ui.tileFiles = map[tileLink][]byte{}
		ui.tileBusy = map[tileLink]struct{}{}
	}
	if ui.lastDraw.orig != orig || ui.lastDraw.size != self.R.Size() {
		ui.lastDraw.orig = orig
		ui.lastDraw.size = self.R.Size()
		img.Draw(self.R.Add(orig), dui.Display.White, nil, image.ZP)
	}

	zoom := ui.zoom()
	tc := ui.Center.Tile(zoom)
	tcWM := tc.Webmerc()
	wmSize := tc.WebmercSize()
	dE := tcWM.E - ui.Center.E
	dN := tcWM.N - ui.Center.N
	offset := image.Pt(int(float64(tileSize.X)*dE/wmSize.E), int(float64(tileSize.Y)*dN/wmSize.N))
	t0 := tile{
		x: tc.x - (self.R.Dx()/2+offset.X+tileSize.X-1)/tileSize.X,
		y: tc.y - (self.R.Dy()/2+offset.Y+tileSize.Y-1)/tileSize.Y,
		z: zoom,
	}
	p0 := offset.Add(image.Pt((t0.x-tc.x)*tileSize.X, (t0.y-tc.y)*tileSize.Y)).Add(self.R.Size().Div(2))
	ntx := (-offset.X + self.R.Dx() + tileSize.X - 1) / tileSize.X
	nty := (-offset.Y + self.R.Dy() + tileSize.Y - 1) / tileSize.Y
	// log.Printf("tileMap.Draw, orig, %s, size %v, t0 %s, tc %s, offset %s, p0 %s, ntx %d nty %d\n", orig, ui.size, t0, tc, offset, p0, ntx, nty)
	for tx := 0; tx < ntx; tx++ {
		for ty := 0; ty < nty; ty++ {
			t := tile{x: t0.x + tx, y: t0.y + ty, z: zoom}
			url := tileURL
			url = strings.Replace(url, "{x}", fmt.Sprintf("%d", t.x), -1)
			url = strings.Replace(url, "{y}", fmt.Sprintf("%d", t.y), -1)
			url = strings.Replace(url, "{z}", fmt.Sprintf("%d", t.z), -1)
			tl := tileLink{tile: t, tileURL: url}

			// log.Printf("draw, want tile %s\n", t)
			if timg, ok := ui.tileImages[tl]; ok {
				p := p0.Add(image.Pt(tx*tileSize.X, ty*tileSize.Y))
				sp := image.ZP
				if p.X < 0 {
					p.X, sp.X = 0, -p.X
				}
				if p.Y < 0 {
					p.Y, sp.Y = 0, -p.Y
				}
				r := rect(tileSize).Add(p).Add(orig).Intersect(self.R.Add(orig))
				img.Draw(r, timg, nil, sp)
				continue
			}
			if _, ok := ui.tileBusy[tl]; ok {
				continue
			}
			if tbuf, ok := ui.tileFiles[tl]; ok {
				ui.tileBusy[tl] = struct{}{}
				go func() {
					timg, err := duit.ReadImage(dui.Display, bytes.NewReader(tbuf))
					if err != nil {
						log.Printf("loading image from existing buffer: %s\n", err)
						return
					}
					tileimages <- tileImage{tileLink: tl, image: timg, buf: tbuf}
				}()
				continue
			}
			ui.tileBusy[tl] = struct{}{}
			go func() {
				// log.Printf("fetching %s\n", url)
				resp, err := http.Get(url)
				if err != nil {
					log.Printf("http get: %s\n", err)
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != 200 {
					log.Printf("http response not 200 but %v\n", resp.StatusCode)
					return
				}
				tbuf, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.Printf("reading tile: %s\n", err)
					return
				}
				timg, err := duit.ReadImage(dui.Display, bytes.NewReader(tbuf))
				if err != nil {
					log.Printf("loading image: %s\n", err)
					return
				}
				tileimages <- tileImage{tileLink: tl, image: timg, buf: tbuf}
			}()
		}
	}
}

func rect(size image.Point) image.Rectangle {
	return image.Rectangle{image.ZP, size}
}

func (ui *tileMap) Mouse(dui *duit.DUI, self *duit.Kid, m draw.Mouse, origM draw.Mouse, orig image.Point) (r duit.Result) {
	move := func(delta image.Point) webmerc {
		zoom := ui.zoom()
		t0 := ui.Center.Tile(zoom)
		deltaWebmerc := t0.WebmercSize()
		hiDPI := dui.Scale(100) > 100
		_, tileSize := ui.tileConfig(hiDPI)
		deltaWebmerc.E *= -float64(delta.X) / float64(tileSize.X)
		deltaWebmerc.N *= -float64(delta.Y) / float64(tileSize.Y)
		return ui.Center.Add(deltaWebmerc)
	}

	r.Hit = ui
	if m.Buttons == duit.Button1 && ui.prevM.Buttons == 0 {
		if m.Msec-ui.prevB1.Msec < 350 {
			// double click, zoom in
			delta := self.R.Size().Div(2).Sub(m.Point)
			ui.Center = move(delta)
			ui.Zoom++
			ui.Zoom = ui.zoom()
			ui.Center = move(image.ZP.Sub(delta))
			r.Consumed = true
			self.Draw = duit.Dirty
		}
		ui.prevB1 = m
	} else if m.Buttons == duit.Button1 {
		delta := m.Point.Sub(ui.prevM.Point)
		ui.Center = move(delta)
		r.Consumed = true
		self.Draw = duit.Dirty
	}
	ui.prevM = m
	return
}

func (ui *tileMap) Key(dui *duit.DUI, self *duit.Kid, k rune, m draw.Mouse, orig image.Point) (r duit.Result) {

	move := func(dx, dy int) {
		t0 := ui.Center.Tile(ui.zoom())
		deltaWebmerc := t0.WebmercSize()
		hiDPI := dui.Scale(100) > 100
		_, tileSize := ui.tileConfig(hiDPI)
		deltaWebmerc.E *= float64(dx) / float64(tileSize.X)
		deltaWebmerc.N *= float64(dy) / float64(tileSize.Y)
		ui.Center = ui.Center.Add(deltaWebmerc)
	}

	r.Hit = ui
	switch k {
	default:
		return
	case '+':
		ui.Zoom++
		ui.Zoom = ui.zoom()
	case '-':
		ui.Zoom--
		ui.Zoom = ui.zoom()
	case draw.KeyLeft:
		move(dui.Scale(-50), 0)
	case draw.KeyRight:
		move(dui.Scale(50), 0)
	case draw.KeyUp:
		move(0, dui.Scale(-50))
	case draw.KeyDown:
		move(0, dui.Scale(50))
	}
	r.Consumed = true
	self.Draw = duit.Dirty
	return
}

func (ui *tileMap) FirstFocus(dui *duit.DUI, self *duit.Kid) (warp *image.Point) {
	p := image.ZP
	return &p
}

func (ui *tileMap) Focus(dui *duit.DUI, self *duit.Kid, o duit.UI) (warp *image.Point) {
	if ui != o {
		return nil
	}
	return ui.FirstFocus(dui, self)
}

func (ui *tileMap) Mark(self *duit.Kid, o duit.UI, forLayout bool) (marked bool) {
	return self.Mark(o, forLayout)
}

func (ui *tileMap) Print(self *duit.Kid, indent int) {
	duit.PrintUI("tileMap", self, indent)
}

func check(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s\n", msg, err)
	}
}

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		log.Println("usage: duitmap")
		flag.PrintDefaults()
	}
	flag.Parse()
	if len(flag.Args()) != 0 {
		flag.Usage()
		os.Exit(2)
	}

	var err error
	dui, err = duit.NewDUI("map", nil)
	check(err, "new dui")

	tileimages = make(chan tileImage)

	tilemap := &tileMap{
		ZoomMin: 1,
		ZoomMax: 17,
		Zoom:    8,
		Center:  wgs84{xlon: 5.5, ylat: 52.2}.Webmerc(),
	}

	setTileset := func(tileset string) {
		const url = "https://tilecache.irias.nl/tiles/{layer}/{z}/{x}/{y}.png"
		tilemap.TileURL = strings.Replace(url, "{layer}", tileset, -1)
		tilemap.TileURL2x = strings.Replace(url, "{layer}", tileset+"@2x", -1)
	}

	var tilesets *duit.Buttongroup
	tilesets = &duit.Buttongroup{
		Texts: []string{
			"osm-bright",
			"osm-liberty",
			"klokantech-basic",
			"positron",
			"dark-matter",
		},
		Changed: func(index int) (e duit.Event) {
			setTileset(tilesets.Texts[index])
			dui.MarkDraw(tilemap)
			return
		},
	}
	setTileset(tilesets.Texts[0])

	zoomOut := &duit.Button{
		Text: "-",
		Click: func() (e duit.Event) {
			tilemap.Zoom--
			e.Consumed = true
			dui.MarkDraw(nil)
			return
		},
	}
	zoomIn := &duit.Button{
		Text: "+",
		Click: func() (e duit.Event) {
			tilemap.Zoom++
			e.Consumed = true
			dui.MarkDraw(nil)
			return
		},
	}
	var search *duit.Field
	search = &duit.Field{
		Placeholder: "search address...",
		Keys: func(k rune, m draw.Mouse) (e duit.Event) {
			if k == '\n' && search.Text != "" {
				t := strings.Split(search.Text, ",")
				var loc wgs84
				var err error
				if len(t) == 2 {
					loc.xlon, err = strconv.ParseFloat(t[0], 64)
					if err == nil {
						loc.ylat, err = strconv.ParseFloat(t[1], 64)
					}
					if err == nil {
						tilemap.Center = loc.Webmerc()
						dui.MarkDraw(nil)
						e.Consumed = true
						return
					}
				}
				u := "https://nominatim.openstreetmap.org/search.php?format=json&q=" + url.QueryEscape(search.Text)
				go func() {
					resp, err := http.Get(u)
					if err != nil {
						log.Printf("address search in http request: %s\n", err)
						return
					}
					if resp.StatusCode != 200 {
						log.Printf("address search api: status not 200 but %d (%s)\n", resp.StatusCode, resp.Status)
						return
					}
					defer resp.Body.Close()
					var hits []struct {
						Licence     string
						Lon         string
						Lat         string
						DisplayName string `json:"display_name"`
					}
					err = json.NewDecoder(resp.Body).Decode(&hits)
					if err != nil {
						log.Printf("decoding address response from api: %s\n", err)
						return
					}
					if len(hits) == 0 {
						log.Printf("no hits for address\n")
						return
					}
					loc.xlon, err = strconv.ParseFloat(hits[0].Lon, 64)
					if err == nil {
						loc.ylat, err = strconv.ParseFloat(hits[0].Lat, 64)
					}
					if err != nil {
						log.Printf("parsing location from address search api hit result: %s\n", err)
						return
					}
					dui.Call <- func() {
						tilemap.Center = loc.Webmerc()
						tilemap.Zoom = 16
						dui.MarkDraw(nil)
					}
				}()

				e.Consumed = true
			}
			return
		},
	}

	var place *duit.Place
	place = &duit.Place{
		Place: func(self *duit.Kid, sizeAvail image.Point) {
			pad := image.Pt(dui.Scale(10), dui.Scale(10))
			kk := place.Kids

			tilemap.Layout(dui, kk[0], sizeAvail, true)

			// tilesets
			kk[1].UI.Layout(dui, kk[1], sizeAvail, true)
			kk[1].R = kk[1].R.Add(pad)

			// box with search
			kk[2].UI.Layout(dui, kk[2], sizeAvail, true)
			kk[2].R = kk[2].R.Add(image.Pt(kk[1].R.Min.X, kk[1].R.Max.Y+pad.Y))

			// zoomOut
			kk[3].UI.Layout(dui, kk[3], sizeAvail, true)
			kk[3].R = kk[3].R.Add(image.Pt(pad.X, sizeAvail.Y-pad.Y).Sub(image.Pt(0, kk[3].R.Dy())))

			// zoomIn
			kk[4].UI.Layout(dui, kk[4], sizeAvail, true)
			kk[4].R = kk[4].R.Add(image.Pt(kk[3].R.Max.X, kk[3].R.Min.Y))

			self.R = rect(sizeAvail)
		},
		Kids: duit.NewKids(
			tilemap,
			tilesets,
			&duit.Box{
				Width: dui.Scale(150),
				Kids:  duit.NewKids(search),
			},
			zoomOut,
			zoomIn,
		),
	}
	dui.Top.UI = place
	dui.Render()

	for {
		select {
		case e := <-dui.Inputs:
			dui.Input(e)

		case <-dui.Done:
			return

		case ti := <-tileimages:
			log.Printf("have tileimage %v\n", ti.tileLink)
			delete(tilemap.tileBusy, ti.tileLink)
			tilemap.tileFiles[ti.tileLink] = ti.buf
			tilemap.tileImages[ti.tileLink] = ti.image
			dui.MarkDraw(nil)
		}
	}
}
