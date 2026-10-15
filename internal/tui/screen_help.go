package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MrZudro/badomens/internal/tui/components"
	"github.com/MrZudro/badomens/internal/tui/layout"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// helpGap separa las celdas del grid de Ayuda: más espaciado que un listado
// pegado, sin desperdiciar el ancho útil (SPEC 2 §13.2).
const helpGap = 3

// helpMaxColumns acota las columnas del grid: con más columnas la hoja se lee
// como una tabla densa y las columnas dejan de seguirse con la vista.
const helpMaxColumns = 3

// helpMaxWidth es el ancho máximo del cuadro de Ayuda: aprovecha las terminales
// anchas para repartir los atajos en columnas sin ocupar toda la pantalla.
const helpMaxWidth = 84

// helpCascade fija el orden en cascada piramidal de los comandos globales
// (SPEC 2 §13.2): primero los comandos cortos (F1, F2, Tab), después Shift+Tab,
// luego los Ctrl cortos (Ctrl+P, Ctrl+Q) y, al final, el resto ordenado de menor
// a mayor longitud de tecla. Las claves son las canónicas de bubbletea.
var helpCascade = []string{"f1", "f2", "tab", "shift+tab", "ctrl+p", "ctrl+q"}

// helpEntry es una fila de la hoja: la tecla tal como se muestra y lo que hace.
type helpEntry struct {
	key   string
	label string
}

// cell es la celda plana «[Tecla] Etiqueta»: el mismo formato que la barra
// inferior (SPEC 2 §1.2/P-04).
func (e helpEntry) cell() string { return "[" + e.key + "] " + e.label }

// helpScreen es la hoja de atajos (§5.14). Separa dos bloques para que no se
// confundan los botones de la pantalla activa con los atajos globales, y pinta
// cada bloque como un grid de columnas alineadas y ordenado (SPEC 2 §13).
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

	inner := helpContentWidth(w)
	screenEntries, screenKeys := h.screenEntries()
	screenGrid := helpGrid(screenEntries, inner, accent)
	globalGrid := helpGrid(helpGlobals(h.keys, screenKeys), inner, accent)

	// El Modal añade borde (2 líneas), título (1) y dos líneas de cierre: el
	// cuerpo se recorta para que el marco no pierda su borde inferior en alturas
	// pequeñas (§5.14).
	maxBody := hgt - 5
	if maxBody < 6 {
		maxBody = 6
	}
	// Fijas: títulos de los bloques, un blanco entre ellos y la pista de cierre.
	fixed := 5
	if len(screenGrid) == 0 {
		fixed = 3
	}
	// Los globales mandan: la barra inferior ya muestra los atajos de la pantalla
	// activa, así que en terminales bajas nunca desaparece antes la cascada
	// piramidal (con los comandos retirados de la barra; §13.2/§13.3).
	globals := truncateRows(globalGrid, maxBody-fixed, muted)
	screen := truncateRows(screenGrid, maxBody-fixed-len(globals), muted)
	if len(screen) == 0 && len(screenGrid) > 0 {
		// No queda sitio para el bloque «Pantalla»: sus teclas vuelven al bloque
		// global (sin deduplicar) para que ninguna quede sin documentar.
		globals = truncateRows(helpGrid(helpGlobals(h.keys, map[string]bool{}), inner, accent), maxBody-3, muted)
	}

	lines := make([]string, 0, maxBody)
	if len(screen) > 0 {
		lines = append(lines, bold.Render("Pantalla"))
		lines = append(lines, screen...)
		lines = append(lines, "")
	} else if len(screenGrid) == 0 {
		// Sin atajos propios se dice explícitamente, para no dar a entender que
		// los globales son botones de la pantalla.
		lines = append(lines, bold.Render("Pantalla"),
			"  "+muted.Render("(esta pantalla no tiene atajos propios)"))
	}
	lines = append(lines, bold.Render("Globales (cualquier pantalla)"))
	lines = append(lines, globals...)
	lines = append(lines, "",
		"  "+muted.Render("Enter abre o confirma · Esc vuelve o cancela sin guardar · F1 o Esc cierra"))

	return components.Modal{
		Title:    "Ayuda",
		Body:     strings.Join(lines, "\n"),
		MaxWidth: helpMaxWidth,
	}.Render(w, h.pal, h.ascii)
}

// screenEntries convierte las acciones de la pantalla activa en filas de la hoja
// y anota sus teclas para no repetirlas en el bloque de globales. Las globales
// (prefijo «global.») van al bloque de abajo.
func (h *helpScreen) screenEntries() ([]helpEntry, map[string]bool) {
	used := make(map[string]bool, len(h.actions))
	out := make([]helpEntry, 0, len(h.actions))
	for _, a := range h.actions {
		if strings.HasPrefix(a.ID, "global.") {
			continue
		}
		label := a.Label()
		if a.Disabled {
			label += " (no disponible)"
		}
		// Las teclas se muestran con la única traducción del repo
		// (components.DisplayKey, la misma que usa la barra inferior): formato único
		// y ninguna tabla paralela de atajos aquí (P-04).
		key := components.DisplayKey(a.Key)
		if strings.TrimSpace(key) == "" {
			key = "—"
		}
		used[normalizeKey(a.Key)] = true
		out = append(out, helpEntry{key: key, label: label})
	}
	return out, used
}

// helpGlobals devuelve los comandos globales de la hoja ya ordenados: los que
// funcionan en cualquier pantalla, incluidos los retirados de la barra inferior
// (Ctrl+P, F2 y Ctrl+Q siguen activos y se documentan aquí, SPEC 2 §1.2/§13.2).
// Una tecla que la pantalla activa ya usa no se repite en este bloque.
func helpGlobals(km KeyMap, used map[string]bool) []helpEntry {
	bindings := []key.Binding{
		km.Help, km.Health, km.NextPane, km.PrevPane,
		km.Palette, km.Quit, km.Back, km.Enter, km.New,
	}
	byCanon := make(map[string]helpEntry, len(bindings))
	for _, b := range bindings {
		if !b.Enabled() || helpBindingUsed(b, used) {
			continue
		}
		canons := b.Keys()
		if len(canons) == 0 {
			continue
		}
		help := b.Help()
		if help.Key == "" || strings.TrimSpace(help.Desc) == "" {
			continue
		}
		// La tecla visible sale de la traducción única de la barra (P-04): la hoja y
		// la barra nunca muestran la misma tecla con dos formatos distintos.
		byCanon[normalizeKey(canons[0])] = helpEntry{
			key:   components.DisplayKey(canons[0]),
			label: help.Desc,
		}
	}
	// Cascada piramidal: primero los comandos cortos en el orden de la spec.
	out := make([]helpEntry, 0, len(bindings))
	for _, canon := range helpCascade {
		if e, ok := byCanon[canon]; ok {
			out = append(out, e)
			delete(byCanon, canon)
		}
	}
	// Resto: de menor a mayor longitud de tecla y, a igual longitud, por la forma
	// visible, para que la hoja no cambie entre ejecuciones.
	rest := make([]helpEntry, 0, len(byCanon))
	for _, e := range byCanon {
		rest = append(rest, e)
	}
	sort.Slice(rest, func(i, j int) bool {
		li, lj := len([]rune(rest[i].key)), len([]rune(rest[j].key))
		if li != lj {
			return li < lj
		}
		return rest[i].key < rest[j].key
	})
	return append(out, rest...)
}

// helpBindingUsed indica si la pantalla activa ya usa alguna tecla de este atajo
// global: en ese caso no se repite en el bloque de globales.
func helpBindingUsed(b key.Binding, used map[string]bool) bool {
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

// helpMore avisa de que el bloque se recortó por altura: la paleta (Ctrl+P)
// siempre tiene la lista completa de acciones.
const helpMore = "… más atajos en la paleta (Ctrl+P)"

// truncateRows recorta un bloque del grid a `max` líneas dejando el aviso de que
// hay más atajos: nunca se oculta información en silencio.
func truncateRows(rows []string, max int, muted lipgloss.Style) []string {
	if max <= 0 {
		return nil
	}
	if len(rows) <= max {
		return rows
	}
	if max == 1 {
		return []string{"  " + muted.Render(helpMore)}
	}
	return append(rows[:max-1], "  "+muted.Render(helpMore))
}

// helpGrid reparte las filas en columnas alineadas que quepan en `inner` celdas.
// El ancho de celda lo fija la fila más larga, de modo que las columnas no bailan
// entre filas, y el número de columnas se ajusta al ancho del terminal sin
// romper la alineación (SPEC 2 §13.2).
func helpGrid(entries []helpEntry, inner int, keyStyle lipgloss.Style) []string {
	if len(entries) == 0 || inner < 8 {
		return nil
	}
	cellW := 0
	for _, e := range entries {
		if w := layout.Width(e.cell()); w > cellW {
			cellW = w
		}
	}
	if cellW > inner {
		cellW = inner
	}
	// En anchos medios se acota el ancho de celda a la mitad del ancho útil: así
	// el grid usa al menos dos columnas en vez de una sola columna kilométrica.
	// Las etiquetas que no caben se abrevian con «…» (el detalle completo vive
	// en la paleta) y la alineación se mantiene porque TODAS las celdas de una
	// misma cuadrícula se rellenan al mismo ancho.
	if inner >= 40 {
		if half := (inner - helpGap) / 2; cellW > half {
			cellW = half
		}
	}
	cols := (inner + helpGap) / (cellW + helpGap)
	if cols < 1 {
		cols = 1
	}
	if cols > helpMaxColumns {
		cols = helpMaxColumns
	}
	gap := strings.Repeat(" ", helpGap)
	rows := (len(entries) + cols - 1) / cols
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		cells := make([]string, 0, cols)
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(entries) {
				break
			}
			// Todas las celdas se rellenan al mismo ancho: las columnas quedan
			// alineadas entre filas.
			cells = append(cells, padPlain(helpCell(entries[i], keyStyle), cellW))
		}
		out = append(out, strings.TrimRight(strings.Join(cells, gap), " "))
	}
	return out
}

// helpCell pinta una celda: la tecla entre corchetes en el color de acento y la
// etiqueta en texto normal (mismo formato que la barra inferior).
func helpCell(e helpEntry, keyStyle lipgloss.Style) string {
	return keyStyle.Render("["+e.key+"]") + " " + e.label
}

// helpContentWidth es el ancho útil dentro del cuadro de Ayuda: el Modal añade
// borde y relleno, y el cuadro se acota a helpMaxWidth.
func helpContentWidth(w int) int {
	boxW := w - 4
	if boxW > helpMaxWidth {
		boxW = helpMaxWidth
	}
	if boxW < 20 {
		boxW = 20
	}
	inner := boxW - 2 // el relleno del modal ocupa una celda a cada lado
	if inner < 8 {
		inner = 8
	}
	return inner
}

func (h *helpScreen) Actions() []components.Action { return nil }
func (h *helpScreen) Title() string                { return "Ayuda" }
func (h *helpScreen) ID() string                   { return "help" }

// Status no aporta mensaje propio.
func (h *helpScreen) Status() string { return "" }
