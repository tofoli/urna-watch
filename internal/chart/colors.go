package chart

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/gpu/urna-watch/internal/domain"
)

// Candidate colors are stable by candidate identity, not by current position.
// Lula/PT is always red and Bolsonaro/PL is always blue, matching the map
// legend even when the leading position changes.
var (
	lulaRed        = color.RGBA{239, 68, 68, 255}
	bolsonaroBlue  = color.RGBA{59, 130, 246, 255}
	fallbackColors = []color.RGBA{
		{34, 197, 94, 255},  // green
		{245, 158, 11, 255}, // amber
		{168, 85, 247, 255}, // purple
		{8, 145, 178, 255},  // cyan
		{219, 39, 119, 255}, // pink
	}
)

// CandidateColor returns the stable visualization color for a candidate.
func CandidateColor(c domain.Candidate) color.RGBA {
	if isLula(c) {
		return lulaRed
	}
	if isBolsonaro(c) {
		return bolsonaroBlue
	}
	idx := c.Position - 1
	if idx < 0 {
		idx = 0
	}
	return fallbackColors[idx%len(fallbackColors)]
}

// CandidateColorHex returns the stable color as #rrggbb for the TUI.
func CandidateColorHex(c domain.Candidate) string {
	col := CandidateColor(c)
	return fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B)
}

func isLula(c domain.Candidate) bool {
	return strings.TrimLeft(c.Number, "0") == "13" || strings.EqualFold(c.PartyAcronym, "PT")
}

func isBolsonaro(c domain.Candidate) bool {
	return strings.TrimLeft(c.Number, "0") == "22" || strings.EqualFold(c.PartyAcronym, "PL")
}
