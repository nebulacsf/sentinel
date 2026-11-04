package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/nebulacsf/sentinel/internal/sshx"
)

type theme struct {
	text     lipgloss.Style
	muted    lipgloss.Style
	dim      lipgloss.Style
	accent   lipgloss.Style
	green    lipgloss.Style
	red      lipgloss.Style
	amber    lipgloss.Style
	border   lipgloss.Style
	barEmpty lipgloss.Style
	selBg    color.Color
}

func newTheme(dark bool) theme {
	pick := func(light, darkColor string) color.Color {
		if dark {
			return lipgloss.Color(darkColor)
		}
		return lipgloss.Color(light)
	}

	return theme{
		text:     lipgloss.NewStyle().Foreground(pick("#24292F", "#C9D1D9")),
		muted:    lipgloss.NewStyle().Foreground(pick("#57606A", "#8B949E")),
		dim:      lipgloss.NewStyle().Foreground(pick("#6E7781", "#6E7681")),
		accent:   lipgloss.NewStyle().Foreground(pick("#6639BA", "#A371F7")),
		green:    lipgloss.NewStyle().Foreground(pick("#1A7F37", "#3FB950")),
		red:      lipgloss.NewStyle().Foreground(pick("#CF222E", "#F85149")),
		amber:    lipgloss.NewStyle().Foreground(pick("#9A6700", "#D29922")),
		border:   lipgloss.NewStyle().Foreground(pick("#D0D7DE", "#30363D")),
		barEmpty: lipgloss.NewStyle().Foreground(pick("#D0D7DE", "#30363D")),
		selBg:    pick("#DDE3EA", "#1F2937"),
	}
}

func (t theme) status(status sshx.Status) lipgloss.Style {
	switch status {
	case sshx.StatusOnline:
		return t.green
	case sshx.StatusOffline:
		return t.red
	case sshx.StatusError:
		return t.amber
	default:
		return t.dim
	}
}

func (t theme) selected(style lipgloss.Style, selected bool) lipgloss.Style {
	if selected {
		return style.Background(t.selBg)
	}
	return style
}

func (t theme) bar(pct float64) lipgloss.Style {
	switch {
	case pct >= 90:
		return t.red
	case pct >= 75:
		return t.amber
	default:
		return t.green
	}
}
