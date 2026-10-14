package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// helpScreen es la hoja de atajos: los contextuales de la pantalla activa
// (Pantalla) y los globales no imprimibles (Globales) (§5.14).
type helpScreen struct {
	keys    KeyMap
	actions []components.Action
	pal     theme.Palette
	ascii   bool
}

// newHelpScreen construye la ayuda con las acciones de la pantalla activa (las
// mismas que alimentan la barra y la paleta) y el mapa de atajos globales.
func newHelpScreen(keys KeyMap, actions []components.Action, pal theme.Palette, ascii bool) *helpScreen {
	return &helpScreen{keys: keys, actions: actions, pal: pal, ascii: ascii}
}

func (h *helpScreen) Init() tea.Cmd { return nil }

func (h *helpScreen) Update(tea.Msg) (Screen, tea.Cmd) { return h, nil }

func (h *helpScreen) View(w, hgt int) string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Accent))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(h.pal.Muted))
	bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(h.pal.Accent))

	// Acciones contextuales de la pantalla activa (tecla + etiqueta). Las
	// globales (prefijo «global.») van al bloque de abajo.
	var screen []string
	for _, a := range h.actions {
		if strings.HasPrefix(a.ID, "global.") {
			continue
		}
		key := a.Key
		if len([]rune(key)) == 1 {
			key = strings.ToUpper(key)
		}
		line := "  " + accent.Render("["+key+"]") + "  " + a.Label()
		if a.Disabled {
			line += "  " + muted.Render("(no disponible)")
		}
		screen = append(screen, line)
	}
	if len(screen) == 0 {
		screen = append(screen, "  "+muted.Render("(esta pantalla no tiene atajos propios)"))
	}

	// Orden por prioridad: los globales esenciales (ayuda, comandos, salud,
	// salir, crear) van primero para que el recorte por altura no los pierda
	// (§5.14); después, la navegación.
	ordered := []key.Binding{
		h.keys.Help, h.keys.Palette, h.keys.Health, h.keys.Quit, h.keys.New,
		h.keys.Up, h.keys.Down, h.keys.Enter, h.keys.Back, h.keys.NextPane, h.keys.Copy,
	}
	var globals []string
	for _, binding := range ordered {
		if !binding.Enabled() {
			continue
		}
		help := binding.Help()
		globals = append(globals, "  "+accent.Render(help.Key)+"  "+help.Desc)
	}

	// El Modal añade borde (2 líneas), título (1) y dos líneas de cierre: el
	// cuerpo se recorta para que el marco no pierda su borde inferior en alturas
	// pequeñas (§5.14). Se sacrifican atajos globales antes que la pista de cierre.
	maxBody := hgt - 5
	if maxBody < 6 {
		maxBody = 6
	}
	fixed := len(screen) + 4 // Pantalla + acciones + blanco + Globales + pista
	allowed := maxBody - fixed
	if allowed < 0 {
		allowed = 0
	}
	if len(globals) > allowed {
		if allowed > 0 {
			globals = append(globals[:allowed-1], "  "+muted.Render("… más atajos en la paleta (Ctrl+P)"))
		} else {
			globals = nil
		}
	}

	lines := []string{bold.Render("Pantalla")}
	lines = append(lines, screen...)
	lines = append(lines, "", bold.Render("Globales"))
	lines = append(lines, globals...)
	lines = append(lines, "  Esc  cerrar ayuda")

	return components.Modal{
		Title: "Ayuda",
		Body:  strings.Join(lines, "\n"),
	}.Render(w, h.pal, h.ascii)
}

func (h *helpScreen) Actions() []components.Action { return nil }
func (h *helpScreen) Title() string                { return "Ayuda" }
func (h *helpScreen) ID() string                   { return "help" }

// NavSection indica la sección de primer nivel activa.
func (h *helpScreen) NavSection() string { return "help" }

// Status no aporta mensaje propio.
func (h *helpScreen) Status() string { return "" }
