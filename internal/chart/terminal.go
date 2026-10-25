package chart

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/vector"
)

// RasterStates renders the Brazil UF mesh into a logical pixel grid. Each cell
// holds a UF code or "" for the ocean/outside. The TUI uses two logical rows
// per terminal row (upper/lower half blocks).
func RasterStates(width, height int) ([][]string, error) {
	if width < 10 {
		width = 10
	}
	if width > 240 {
		width = 240
	}
	if height < 10 {
		height = 10
	}
	if height > 240 {
		height = 240
	}
	shapes, err := loadBrazilMap()
	if err != nil {
		return nil, err
	}
	project := newProjector(shapes, width, height)
	best := make([]uint8, width*height)
	grid := make([][]string, height)
	for y := range grid {
		grid[y] = make([]string, width)
	}
	for _, shape := range shapes {
		mask := image.NewAlpha(image.Rect(0, 0, width, height))
		for _, poly := range shape.Polygons {
			if len(poly) == 0 || len(poly[0]) < 3 {
				continue
			}
			r := vector.NewRasterizer(width, height)
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
			r.DrawOp = draw.Over
			r.Draw(mask, mask.Bounds(), image.NewUniform(color.Alpha{A: 255}), image.Point{})
		}
		uf := shape.UF
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				a := mask.AlphaAt(x, y).A
				if a == 0 {
					continue
				}
				idx := y*width + x
				if a > best[idx] {
					best[idx] = a
					grid[y][x] = uf
				}
			}
		}
	}
	return grid, nil
}
