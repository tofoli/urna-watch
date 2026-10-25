package chart

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"

	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/format"
	"golang.org/x/image/vector"
)

//go:embed brazil.geojson
var brazilGeoJSON []byte

const (
	dashboardWidth  = 1300
	dashboardHeight = 800
	mapPanelX       = 540
	mapPanelY       = 96
	mapPanelW       = 720
	mapPanelH       = 616
	mapInnerX       = 566
	mapInnerY       = 152
	mapInnerW       = 668
	mapInnerH       = 520
)

var (
	darkBG      = color.RGBA{8, 15, 30, 255}
	darkPanel   = color.RGBA{15, 23, 42, 255}
	darkBorder  = color.RGBA{30, 41, 59, 255}
	lightText   = color.RGBA{248, 250, 252, 255}
	softText    = color.RGBA{148, 163, 184, 255}
	unknownArea = color.RGBA{71, 85, 105, 255}
	mapBorder   = color.RGBA{8, 15, 30, 255}
	ibgeToUF    = map[string]string{
		"11": "RO", "12": "AC", "13": "AM", "14": "RR", "15": "PA", "16": "AP", "17": "TO",
		"21": "MA", "22": "PI", "23": "CE", "24": "RN", "25": "PB", "26": "PE", "27": "AL",
		"28": "SE", "29": "BA", "31": "MG", "32": "ES", "33": "RJ", "35": "SP", "41": "PR",
		"42": "SC", "43": "RS", "50": "MS", "51": "MT", "52": "GO", "53": "DF",
	}
)

type geoCollection struct {
	Features []struct {
		Properties struct {
			Code string `json:"codarea"`
		} `json:"properties"`
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

type geoState struct {
	UF       string
	Polygons [][][]geoPoint
	Bounds   geoBounds
}

type geoPoint struct{ Lon, Lat float64 }

type geoBounds struct {
	MinLon, MinLat, MaxLon, MaxLat float64
}

// RenderMap draws a TV-style dashboard: candidate bars on the left and a Brazil
// map coloured by the winning candidate in each state on the right. It returns
// ErrNoData when no state has published votes.
func RenderMap(main *domain.Snapshot, states map[string]*domain.Snapshot) ([]byte, error) {
	shapes, err := loadBrazilMap()
	if err != nil {
		return nil, err
	}
	if main == nil {
		return nil, ErrNoData
	}
	colorByNumber := make(map[string]color.RGBA, len(main.Candidates))
	for _, c := range main.Candidates {
		colorByNumber[c.Number] = CandidateColor(c)
	}
	winners := make(map[string]string, len(states))
	for uf, snap := range states {
		if snap == nil {
			continue
		}
		for _, c := range snap.Candidates {
			if c.Votes.OrZero() > 0 {
				winners[strings.ToUpper(uf)] = c.Number
				break
			}
		}
	}
	if len(winners) == 0 {
		return nil, ErrNoData
	}

	img := image.NewRGBA(image.Rect(0, 0, dashboardWidth, dashboardHeight))
	draw.Draw(img, img.Bounds(), image.NewUniform(darkBG), image.Point{}, draw.Src)

	drawText(img, 60, 88, 42, lightText, true, fitText(main.Election.ContestName, 42, true, 440))
	subtitle := electionYear(main) + " · " + main.Election.RoundLabel() + " · maioria por estado"
	drawText(img, 60, 128, 21, softText, false, fitText(subtitle, 21, false, 440))

	candidateRows := main.Candidates
	if len(candidateRows) > 5 {
		candidateRows = candidateRows[:5]
	}
	y := 196
	for _, c := range candidateRows {
		col := colorByNumber[c.Number]
		draw.Draw(img, image.Rect(60, y-22, 86, y+4), image.NewUniform(col), image.Point{}, draw.Src)
		name := c.DisplayName()
		if c.PartyAcronym != "" {
			name += " (" + c.PartyAcronym + ")"
		}
		drawText(img, 100, y, 25, lightText, true, fitText(name, 25, true, 350))
		pct := c.PercentText
		if pct == "" {
			pct = format.PercentNano(c.PercentNano)
		}
		drawText(img, 100, y+34, 20, softText, false, fmt.Sprintf("%s%% · %s votos", pct, format.Votes(c.Votes.OrZero())))
		barW := 400
		fill := int64(barW) * clampPercent(c.PercentNano) / 100_000_000_000
		draw.Draw(img, image.Rect(100, y+50, 100+barW, y+62), image.NewUniform(darkBorder), image.Point{}, draw.Src)
		if fill > 0 {
			draw.Draw(img, image.Rect(100, y+50, 100+int(fill), y+62), image.NewUniform(col), image.Point{}, draw.Src)
		}
		y += 96
	}

	progressY := y + 26
	drawText(img, 60, progressY, 18, softText, false, "Apuração")
	barW := 460
	fill := int64(barW) * clampPercent(main.SectionsPercentNano) / 100_000_000_000
	draw.Draw(img, image.Rect(60, progressY+12, 60+barW, progressY+24), image.NewUniform(darkBorder), image.Point{}, draw.Src)
	if fill > 0 {
		draw.Draw(img, image.Rect(60, progressY+12, 60+int(fill), progressY+24), image.NewUniform(color.RGBA{45, 212, 191, 255}), image.Point{}, draw.Src)
	}
	drawText(img, 60, progressY+52, 18, lightText, false, fmt.Sprintf("%s%% das seções · %s de %s", format.PercentNano(main.SectionsPercentNano), format.Votes(main.SectionsCounted.OrZero()), format.Votes(main.SectionsTotal.OrZero())))
	if !main.TotalizedAt.IsZero() {
		drawText(img, 60, progressY+82, 16, softText, false, "Fonte: TSE · atualizado "+format.DateTime(main.TotalizedAt))
	}

	draw.Draw(img, image.Rect(mapPanelX, mapPanelY, mapPanelX+mapPanelW, mapPanelY+mapPanelH), image.NewUniform(darkPanel), image.Point{}, draw.Src)
	drawRect(img, image.Rect(mapPanelX, mapPanelY, mapPanelX+mapPanelW, mapPanelY+mapPanelH), darkBorder)
	drawText(img, mapPanelX+24, mapPanelY+34, 22, lightText, true, "Maioria por estado")
	drawText(img, mapPanelX+24, mapPanelY+60, 15, softText, false, "Cada estado usa a cor do candidato com mais votos")

	mapImg := renderBrazil(shapes, winners, colorByNumber)
	draw.Draw(img, image.Rect(mapInnerX, mapInnerY, mapInnerX+mapInnerW, mapInnerY+mapInnerH), mapImg, image.Point{}, draw.Over)

	drawText(img, mapPanelX+24, mapPanelY+mapPanelH-22, 14, softText, false, "Cinza = sem votos publicados na UF")

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderBrazil(shapes []geoState, winners map[string]string, colors map[string]color.RGBA) image.Image {
	project := newProjector(shapes, mapInnerW, mapInnerH)
	img := image.NewRGBA(image.Rect(0, 0, mapInnerW, mapInnerH))
	for _, s := range shapes {
		uf := strings.ToUpper(s.UF)
		col := unknownArea
		if num, ok := winners[uf]; ok {
			if c, found := colors[num]; found {
				col = c
			}
		}
		for _, poly := range s.Polygons {
			if len(poly) == 0 || len(poly[0]) < 3 {
				continue
			}
			r := vector.NewRasterizer(mapInnerW, mapInnerH)
			for _, ring := range poly {
				if len(ring) < 3 {
					continue
				}
				x, y := project(ring[0])
				r.MoveTo(x, y)
				for _, p := range ring[1:] {
					x, y = project(p)
					r.LineTo(x, y)
				}
				r.ClosePath()
			}
			r.Draw(img, img.Bounds(), image.NewUniform(col), image.Point{})
		}
	}
	// Borders.
	for _, s := range shapes {
		for _, poly := range s.Polygons {
			for _, ring := range poly {
				for i := 0; i+1 < len(ring); i++ {
					x1, y1 := project(ring[i])
					x2, y2 := project(ring[i+1])
					drawLine(img, int(x1), int(y1), int(x2), int(y2), mapBorder)
				}
			}
		}
	}
	return img
}

func newProjector(shapes []geoState, width, height int) func(geoPoint) (float32, float32) {
	bounds := geoBounds{MinLon: math.MaxFloat64, MinLat: math.MaxFloat64, MaxLon: -math.MaxFloat64, MaxLat: -math.MaxFloat64}
	for _, s := range shapes {
		bounds.MinLon = math.Min(bounds.MinLon, s.Bounds.MinLon)
		bounds.MinLat = math.Min(bounds.MinLat, s.Bounds.MinLat)
		bounds.MaxLon = math.Max(bounds.MaxLon, s.Bounds.MaxLon)
		bounds.MaxLat = math.Max(bounds.MaxLat, s.Bounds.MaxLat)
	}
	lonSpan := bounds.MaxLon - bounds.MinLon
	latSpan := bounds.MaxLat - bounds.MinLat
	scale := math.Min(float64(width)/lonSpan, float64(height)/latSpan)
	offX := (float64(width) - lonSpan*scale) / 2
	offY := (float64(height) - latSpan*scale) / 2
	return func(p geoPoint) (float32, float32) {
		x := float32(offX + (p.Lon-bounds.MinLon)*scale)
		y := float32(offY + (bounds.MaxLat-p.Lat)*scale)
		return x, y
	}
}

func loadBrazilMap() ([]geoState, error) {
	var collection geoCollection
	if err := json.Unmarshal(brazilGeoJSON, &collection); err != nil {
		return nil, fmt.Errorf("mapa do Brasil inválido: %w", err)
	}
	var out []geoState
	for _, f := range collection.Features {
		uf, ok := ibgeToUF[f.Properties.Code]
		if !ok {
			continue
		}
		state := geoState{UF: uf}
		var rawPolys [][][][]float64
		switch f.Geometry.Type {
		case "Polygon":
			var poly [][][]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &poly); err != nil {
				return nil, err
			}
			rawPolys = [][][][]float64{poly}
		case "MultiPolygon":
			if err := json.Unmarshal(f.Geometry.Coordinates, &rawPolys); err != nil {
				return nil, err
			}
		default:
			continue
		}
		for _, poly := range rawPolys {
			var rings [][]geoPoint
			for _, ring := range poly {
				var pts []geoPoint
				for _, p := range ring {
					if len(p) < 2 {
						continue
					}
					pt := geoPoint{Lon: p[0], Lat: p[1]}
					pts = append(pts, pt)
					state.Bounds.MinLon = math.Min(state.Bounds.MinLon, pt.Lon)
					state.Bounds.MinLat = math.Min(state.Bounds.MinLat, pt.Lat)
					state.Bounds.MaxLon = math.Max(state.Bounds.MaxLon, pt.Lon)
					state.Bounds.MaxLat = math.Max(state.Bounds.MaxLat, pt.Lat)
				}
				if len(pts) >= 3 {
					rings = append(rings, pts)
				}
			}
			if len(rings) > 0 {
				state.Polygons = append(state.Polygons, rings)
			}
		}
		if len(state.Polygons) > 0 {
			out = append(out, state)
		}
	}
	if len(out) != 27 {
		return nil, fmt.Errorf("mapa do Brasil incompleto: %d UFs", len(out))
	}
	return out, nil
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if image.Pt(x0, y0).In(img.Bounds()) {
			img.Set(x0, y0, c)
			if x0+1 < img.Bounds().Max.X {
				img.Set(x0+1, y0, c)
			}
			if y0+1 < img.Bounds().Max.Y {
				img.Set(x0, y0+1, c)
			}
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func drawRect(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+2), image.NewUniform(c), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(r.Min.X, r.Max.Y-2, r.Max.X, r.Max.Y), image.NewUniform(c), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+2, r.Max.Y), image.NewUniform(c), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(r.Max.X-2, r.Min.Y, r.Max.X, r.Max.Y), image.NewUniform(c), image.Point{}, draw.Src)
}
