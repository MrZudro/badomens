package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// helpScreen es la hoja de atajos (§5.14). Separa dos bloques para que no se
// confundan los botones de la pantalla activa con la navegación de primer nivel:
// «Pantalla» (las mismas acciones que la barra inferior y la paleta) y «Globales
// (menú principal)» (Tab/Shift+Tab, F1, Ctrl+P, F2, Ctrl+N, Ctrl+Q).
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

	// Bloque «Pantalla»: acciones propias de la pantalla activa (tecla +
	// etiqueta). Las globales (prefijo «global.») van al bloque de abajo; sus
	// teclas se anotan para no repetirlas luego (dedup).
	var screen []string
	screenKeys := make(map[string]bool, len(h.actions))
	for _, a := range h.actions {
		if strings.HasPrefix(a.ID, "global.") {
			continue
		}
		screenKeys[normalizeKey(a.Key)] = true
		label := a.Key
		if len([]rune(label)) == 1 {
			label = strings.ToUpper(label)
		}
		if label == "" {
			label = "—"
		}
		line := "  " + accent.Render("["+label+"]") + "  " + a.Label()
		if a.Disabled {
			line += "  " + muted.Render("(no disponible)")
		}
		screen = append(screen, line)
	}
	if len(screen) == 0 {
		// Sin atajos propios se dice explícitamente, para no dar a entender que
		// las teclas del menú son botones de la pantalla.
		screen = append(screen, "  "+muted.Render("(esta pantalla no tiene atajos propios: usa los del menú principal)"))
	}

	// Bloque «Globales (menú principal)»: solo lo que funciona en cualquier
	// pantalla. Si la pantalla activa ya usa esa tecla (el asistente usa Tab para
	// avanzar), no se repite aquí (§5.14). Los esenciales van primero para que el
	// recorte por altura nunca pierda «Salir», la ayuda ni los comandos.
	var globals []string
	for _, binding := range []key.Binding{
		h.keys.Quit, h.keys.Help, h.keys.Palette, h.keys.Health,
		h.keys.New, h.keys.NextPane, h.keys.PrevPane,
	} {
		if !binding.Enabled() || helpUsesKey(binding, screenKeys) {
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
	fixed := len(screen) + 4 // título + acciones + blanco + Globales + pista
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
	lines = append(lines, "", bold.Render("Globales (menú principal)"))
	lines = append(lines, globals...)
	lines = append(lines, "  "+muted.Render("Esc o F1: cerrar ayuda"))

	return components.Modal{
		Title: "Ayuda",
		Body:  strings.Join(lines, "\n"),
	}.Render(w, h.pal, h.ascii)
}

// helpUsesKey indica si la pantalla activa ya usa alguna de las teclas del atajo
// global: en ese caso no se repite en el bloque de globales.
func helpUsesKey(b key.Binding, used map[string]bool) bool {
	for _, k := range b.Keys() {
		if used[normalizeKey(k)] {
			return true
		}
	}
	return false
}

// normalizeKey compara teclas sin depender de mayúsculas ni espacios sobrantes.
func normalizeKey(k string) string {
	return strings.ToLower(strings.TrimSpace(k))
}

func (h *helpScreen) Actions() []components.Action { return nil }
func (h *helpScreen) Title() string                { return "Ayuda" }
func (h *helpScreen) ID() string                   { return "help" }

// NavSection indica la sección de primer nivel activa.
func (h *helpScreen) NavSection() string { return "help" }

// Status no aporta mensaje propio.
func (h *helpScreen) Status() string { return "" }
