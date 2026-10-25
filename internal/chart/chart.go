// Package chart renders a small PNG bar chart for Telegram. It uses only the
// normalized snapshot, so the image can never contain values absent from the
// official source.
package chart

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/format"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// ErrNoData means there are no published votes to draw yet.
var ErrNoData = errors.New("sem votos publicados para gerar gráfico")

const (
	canvasWidth = 1000
	margin      = 48
)

var (
	bgColor     = color.RGBA{255, 255, 255, 255}
	titleColor  = color.RGBA{15, 23, 42, 255}
	mutedColor  = color.RGBA{100, 116, 139, 255}
	barBgColor  = color.RGBA{226, 232, 240, 255}
	footerColor = color.RGBA{148, 163, 184, 255}
)

// Render returns a PNG with the top candidates. It returns ErrNoData when the
// source has not published any candidate votes.
func Render(s *domain.Snapshot) ([]byte, error) {
	if s == nil {
		return nil, ErrNoData
	}
	cands := make([]domain.Candidate, 0, 5)
	for _, c := range s.Candidates {
		if c.Votes.OrZero() > 0 || c.PercentNano > 0 {
			cands = append(cands, c)
		}
		if len(cands) == 5 {
			break
		}
	}
	if len(cands) == 0 {
		return nil, ErrNoData
	}

	rowHeight := 92
	height := 238 + len(cands)*rowHeight + 78
	img := image.NewRGBA(image.Rect(0, 0, canvasWidth, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(bgColor), image.Point{}, draw.Src)

	title := fmt.Sprintf("%s — %s", s.Election.ContestName, s.Election.ScopeName)
	drawText(img, margin, 62, 34, titleColor, true, fitText(title, 34, true, canvasWidth-2*margin))
	subtitle := fmt.Sprintf("%s · %s", electionYear(s), s.Election.RoundLabel())
	if !s.TotalizedAt.IsZero() {
		subtitle += " · atualizado " + format.DateTime(s.TotalizedAt)
	}
	drawText(img, margin, 100, 20, mutedColor, false, subtitle)
	progress := progressText(s)
	drawText(img, margin, 134, 20, mutedColor, false, progress)

	y := 186
	for _, c := range cands {
		label := fmt.Sprintf("%d. %s", c.Position, c.DisplayName())
		if c.PartyAcronym != "" {
			label += " (" + c.PartyAcronym + ")"
		}
		drawText(img, margin, y, 24, titleColor, true, fitText(label, 24, true, 640))
		pct := c.PercentText
		if pct == "" {
			pct = format.PercentNano(c.PercentNano)
		}
		value := fmt.Sprintf("%s%% · %s votos", pct, format.Votes(c.Votes.OrZero()))
		drawTextRight(img, canvasWidth-margin, y, 20, mutedColor, false, value)

		barY := y + 18
		barW := canvasWidth - 2*margin
		draw.Draw(img, image.Rect(margin, barY, margin+barW, barY+18), image.NewUniform(barBgColor), image.Point{}, draw.Src)
		fill := int64(barW) * clampPercent(c.PercentNano) / 100_000_000_000
		if fill > 0 {
			draw.Draw(img, image.Rect(margin, barY, margin+int(fill), barY+18), image.NewUniform(CandidateColor(c)), image.Point{}, draw.Src)
		}
		y += rowHeight
	}

	footer := "Dados oficiais do TSE · Urna Watch"
	drawText(img, margin, height-36, 16, footerColor, false, footer)
	drawTextRight(img, canvasWidth-margin, height-36, 16, footerColor, false, progressShort(s))

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func clampPercent(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > 100_000_000_000 {
		return 100_000_000_000
	}
	return v
}

func progressText(s *domain.Snapshot) string {
	if s.IsFinal {
		return "Totalização final informada pelo TSE"
	}
	if s.SectionsCounted.Known && s.SectionsTotal.Known && s.SectionsTotal.Value > 0 {
		return fmt.Sprintf("Apuração parcial: %s%% das seções (%s de %s)",
			format.PercentNano(s.SectionsPercentNano),
			format.Votes(s.SectionsCounted.Value),
			format.Votes(s.SectionsTotal.Value))
	}
	return "Apuração em andamento"
}

func progressShort(s *domain.Snapshot) string {
	if s.IsFinal {
		return "totalização final"
	}
	return format.PercentNano(s.SectionsPercentNano) + "% das seções"
}

func electionYear(s *domain.Snapshot) string {
	if !s.TotalizedAt.IsZero() {
		return fmt.Sprintf("%d", s.TotalizedAt.Year())
	}
	if !s.GeneratedAt.IsZero() {
		return fmt.Sprintf("%d", s.GeneratedAt.Year())
	}
	cycle := strings.TrimPrefix(s.Election.Cycle, "ele")
	if len(cycle) == 4 {
		return cycle
	}
	return "eleição"
}

func drawText(img draw.Image, x, baseline, size int, c color.Color, bold bool, text string) {
	if text == "" {
		return
	}
	face, err := newFace(size, bold)
	if err != nil {
		return
	}
	defer face.Close()
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, baseline)}
	d.DrawString(text)
}

func drawTextRight(img draw.Image, right, baseline, size int, c color.Color, bold bool, text string) {
	if text == "" {
		return
	}
	face, err := newFace(size, bold)
	if err != nil {
		return
	}
	defer face.Close()
	w := font.MeasureString(face, text)
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(right-int(w>>6), baseline)}
	d.DrawString(text)
}

func fitText(text string, size int, bold bool, maxWidth int) string {
	face, err := newFace(size, bold)
	if err != nil {
		return text
	}
	defer face.Close()
	if font.MeasureString(face, text) <= fixed.I(maxWidth) {
		return text
	}
	runes := []rune(text)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if font.MeasureString(face, candidate) <= fixed.I(maxWidth) {
			return candidate
		}
	}
	return "…"
}

func newFace(size int, bold bool) (font.Face, error) {
	data := goregular.TTF
	if bold {
		data = gobold.TTF
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72,
		Hinting: font.HintingFull,
	})
}
