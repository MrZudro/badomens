// Package theme define la paleta de la TUI v2 (RF-1/§4.2): temas dark, light,
// high-contrast y colorblind, degradación a color plano y un test que verifica
// el contraste WCAG AA (≥ 4.5:1) de los pares texto/fondo declarados.
package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Name identifica un tema.
type Name string

// Temas disponibles.
const (
	Dark         Name = "dark"
	Light        Name = "light"
	HighContrast Name = "high-contrast"
	Colorblind   Name = "colorblind"
)

// Palette es la paleta de un tema.
type Palette struct {
	Name       Name
	Background string
	Foreground string
	Accent     string // botón primario / foco
	AccentText string // texto sobre Accent
	Muted      string // texto secundario
	Danger     string // acciones destructivas
	Warn       string // avisos
	Good       string // éxito / en línea
	Bad        string // error / ocupado
	Border     string
	// Contrasts declara pares (texto, fondo) que deben cumplir WCAG AA.
	Contrasts [][2]string
}

// For devuelve la paleta de un tema (dark si el nombre no existe).
func For(name Name) Palette {
	switch name {
	case Light:
		p := paletteLight()
		return p
	case HighContrast:
		return paletteHighContrast()
	case Colorblind:
		return paletteColorblind()
	}
	return paletteDark()
}

// All devuelve los temas soportados en orden.
func All() []Name { return []Name{Dark, Light, HighContrast, Colorblind} }

// Parse interpreta el nombre de un tema; vacío → dark.
func Parse(s string) Name {
	switch Name(strings.ToLower(strings.TrimSpace(s))) {
	case Light, HighContrast, Colorblind:
		return Name(strings.ToLower(strings.TrimSpace(s)))
	}
	return Dark
}

func paletteDark() Palette {
	return Palette{
		Name:       Dark,
		Background: "#1e1e2e",
		Foreground: "#cdd6f4",
		Accent:     "#89b4fa",
		AccentText: "#11111b",
		Muted:      "#a6adc8",
		Danger:     "#f38ba8",
		Warn:       "#f9e2af",
		Good:       "#a6e3a1",
		Bad:        "#f38ba8",
		Border:     "#45475a",
		Contrasts: [][2]string{
			{"#cdd6f4", "#1e1e2e"}, // texto sobre fondo
			{"#a6adc8", "#1e1e2e"}, // muted sobre fondo
			{"#11111b", "#89b4fa"}, // texto del acento
			{"#a6e3a1", "#1e1e2e"}, // éxito
			{"#f38ba8", "#1e1e2e"}, // error
		},
	}
}

func paletteLight() Palette {
	return Palette{
		Name:       Light,
		Background: "#eff1f5",
		Foreground: "#4c4f69",
		Accent:     "#1e66f5",
		AccentText: "#ffffff",
		Muted:      "#5c5f77",
		Danger:     "#d20f39",
		Warn:       "#c89634",
		Good:       "#0a6b23",
		Bad:        "#d20f39",
		Border:     "#ccd0da",
		Contrasts: [][2]string{
			{"#4c4f69", "#eff1f5"},
			{"#5c5f77", "#eff1f5"},
			{"#ffffff", "#1e66f5"},
			{"#0a6b23", "#eff1f5"},
			{"#d20f39", "#eff1f5"},
		},
	}
}

func paletteHighContrast() Palette {
	return Palette{
		Name:       HighContrast,
		Background: "#000000",
		Foreground: "#ffffff",
		Accent:     "#ffff00",
		AccentText: "#000000",
		Muted:      "#e0e0e0",
		Danger:     "#ff6b6b",
		Warn:       "#ffd166",
		Good:       "#90ee90",
		Bad:        "#ff6b6b",
		Border:     "#cccccc",
		Contrasts: [][2]string{
			{"#ffffff", "#000000"},
			{"#e0e0e0", "#000000"},
			{"#000000", "#ffff00"},
			{"#90ee90", "#000000"},
			{"#ff6b6b", "#000000"},
		},
	}
}

func paletteColorblind() Palette {
	// Paleta distinguible por luminosidad (no solo por tono rojo/verde).
	return Palette{
		Name:       Colorblind,
		Background: "#1c1c1e",
		Foreground: "#f2f2f7",
		Accent:     "#0a84ff",
		AccentText: "#000000",
		Muted:      "#aeaeb2",
		Danger:     "#ff453a",
		Warn:       "#ffd60a",
		Good:       "#30d158",
		Bad:        "#ff453a",
		Border:     "#3a3a3c",
		Contrasts: [][2]string{
			{"#f2f2f7", "#1c1c1e"},
			{"#aeaeb2", "#1c1c1e"},
			{"#000000", "#0a84ff"},
			{"#30d158", "#1c1c1e"},
			{"#ff453a", "#1c1c1e"},
		},
	}
}

// RelLum calcula la luminancia relativa WCAG de un color "#rrggbb".
func RelLum(hex string) float64 {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return 0
	}
	rgb, err := strconv.ParseUint(hex, 16, 24)
	if err != nil {
		return 0
	}
	return lum(float64(byte(rgb>>16))/255, float64(byte(rgb>>8))/255, float64(byte(rgb))/255)
}

func lum(r, g, b float64) float64 {
	lin := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r, g, b = lin(r), lin(g), lin(b)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// ContrastRatio devuelve la razón de contraste WCAG entre dos colores.
func ContrastRatio(a, b string) float64 {
	la, lb := RelLum(a), RelLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Icon devuelve el icono de estado con su texto (el estado nunca se comunica
// solo con color). Si ascii es true, usa [OK]/[!!] etc.
func Icon(state string, ascii bool) string {
	type icon struct{ uni, asc string }
	m := map[string]icon{
		"running":  {"● En línea", "[OK] En línea"},
		"stopped":  {"○ Apagado", "[..] Apagado"},
		"starting": {"◐ Cargando…", "[..] Cargando…"},
		"problems": {"⚠ Con problemas", "[!!] Con problemas"},
		"error":    {"✖ Error", "[XX] Error"},
		"idle":     {"☾ Dormido por inactividad", "[-] Dormido por inactividad"},
	}
	ic, ok := m[state]
	if !ok {
		return state
	}
	if ascii {
		return ic.asc
	}
	return ic.uni
}

// Color pinta un nombre de color en ANSI 256 (para tests y depuración).
func Color(name string) string { return fmt.Sprintf("%%{color:%s}", name) }
