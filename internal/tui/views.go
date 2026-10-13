package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/manuelserver/badomens/internal/version"
)

// View renderiza la pantalla completa según el modo actual.
func (m Model) View() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	header := m.headerView(w)
	body := m.bodyView(w, bodyHeight(h))
	status := m.statusLine(w)
	keys := m.keysView(w)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, status, keys)
}

func (m Model) headerView(w int) string {
	title := styleHeader.Render("badomens") + " · panel multi-instancia"
	info := fmt.Sprintf("v%s · %d/%d corriendo · %d/%d instancias",
		version.Version, m.running, m.maxParallel, len(m.rows), m.maxInstances)
	pad := w - lipgloss.Width(title) - lipgloss.Width(info) - 2
	if pad < 1 {
		pad = 1
	}
	return title + strings.Repeat(" ", pad) + styleDim.Render(info)
}

func (m Model) bodyView(w, h int) string {
	switch m.mode {
	case modeDetail:
		return m.detailView(w, h)
	case modeLogs:
		return m.logsView.View()
	case modeConsole:
		return m.consoleView(w, h)
	case modeBackups:
		return m.backupsView(w, h)
	case modeHelp:
		return m.helpView(w)
	default:
		return m.listView(w, h)
	}
}

// statusLine muestra el último error (rojo) o estado (tenue).
func (m Model) statusLine(w int) string {
	if m.errText != "" {
		return styleErr.Render(truncate(m.errText, w))
	}
	if m.status != "" {
		return styleStatus.Render(truncate(m.status, w))
	}
	return " "
}

// Columnas de la tabla: 16+13+7+8+5+7+12+8 = 76 más 7 separadores = 83 ≤ 84.
const listCols = "%-16s %-13s %-7s %-8s %-5s %-7s %-12s %s"

func (m Model) listView(w, h int) string {
	if len(m.rows) == 0 {
		msg := "  Sin instancias registradas aún.\n" +
			"  Crea la primera con:  badomens setup\n" +
			"  El resto de comandos del CLI funcionan igual (badomens --help)."
		return styleMuted.Render(msg) + "\n"
	}
	header := fmt.Sprintf(listCols, "SLUG", "NOMBRE", "MOTOR", "VERSIÓN", "RAM", "PUERTO", "ESTADO", "SALUD")
	body := strings.Builder{}
	body.WriteString(styleTitle.Render(header))
	body.WriteString("\n")

	visible := h - 2
	if visible < 1 {
		visible = 1
	}
	top := 0
	if m.cursor >= visible {
		top = m.cursor - visible + 1
	}
	if top > 0 {
		body.WriteString(styleMuted.Render("  ↑ más instancias arriba") + "\n")
		visible--
	}
	end := top + visible
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := top; i < end; i++ {
		sel := i == m.cursor
		line := m.rowLine(m.rows[i], sel, w)
		body.WriteString(line)
		if i < end-1 {
			body.WriteString("\n")
		}
	}
	if end < len(m.rows) {
		body.WriteString("\n" + styleMuted.Render("  ↓ más instancias abajo"))
	}
	return body.String()
}

func (m Model) rowLine(r Row, selected bool, width int) string {
	if !selected {
		return fmt.Sprintf(listCols,
			truncate(r.Slug, 16), truncate(r.Name, 13), cell(r.Engine, 7),
			cell(r.Version, 8), cell(r.Memory, 5), cell(fmt.Sprintf("%d", r.Port), 7),
			styleStateText(stateStyle(r.State), stateLabel(r.State), 12),
			styleHealthText(healthStyle(m.health[r.Slug]), healthLabel(m.health[r.Slug]), 8))
	}
	// Fila seleccionada: banda continua de borde a borde. Se construye primero
	// el texto plano (sin ANSI por celda para que el fondo no se reinicie a
	// mitad) y luego se aplica UN solo estilo a toda la línea.
	plain := fmt.Sprintf(listCols,
		truncate(r.Slug, 16), truncate(r.Name, 13), cell(r.Engine, 7),
		cell(r.Version, 8), cell(r.Memory, 5), cell(fmt.Sprintf("%d", r.Port), 7),
		cell(stateLabel(r.State), 12), cell(healthLabel(m.health[r.Slug]), 8))
	return styleSelected.Render(padTo(plain, width))
}

// styleStateText aplica el color semántico a la celda de estado rellena.
func styleStateText(s lipgloss.Style, label string, width int) string {
	return s.Render(cell(label, width))
}

// styleHealthText aplica el color de salud a la celda rellena.
func styleHealthText(s lipgloss.Style, label string, width int) string {
	return s.Render(cell(label, width))
}

func (m Model) detailView(w, h int) string {
	if m.detail == nil || m.detail.Spec == nil {
		return styleMuted.Render("  cargando detalle…")
	}
	s := m.detail.Spec
	var b strings.Builder
	b.WriteString(styleTitle.Render(fmt.Sprintf("Instancia %s · %s", s.Slug, s.Name)) + "\n")
	b.WriteString(fmt.Sprintf("  estado      %s\n", stateStyle(m.detail.State).Render(stateLabel(m.detail.State))))
	if m.detail.HostIP != "" {
		b.WriteString(fmt.Sprintf("  dirección   %s:%d\n", m.detail.HostIP, s.Port))
	}
	b.WriteString(fmt.Sprintf("  motor       %s %s\n", s.Engine, s.Version))
	if p := pinsLabel(s.Pins); p != "" {
		b.WriteString(fmt.Sprintf("  pines       %s\n", p))
	}
	b.WriteString(fmt.Sprintf("  recursos    %s RAM · %d CPU · puerto %d\n", s.Memory, s.CPU, s.Port))
	b.WriteString(fmt.Sprintf("  juego       %s · %s · %d jugadores máx · online=%v\n",
		s.Difficulty, s.Gamemode, s.MaxPlayers, s.OnlineMode))
	if m.detail.Metrics != nil {
		mtr := m.detail.Metrics
		b.WriteString(fmt.Sprintf("  métricas    CPU %.1f%% · RAM %.0f/%.0f MB · mundo %.1f MB\n",
			mtr.CPUPercent, mtr.MemUsedMB, mtr.MemLimitMB, mtr.WorldMB))
	}
	b.WriteString(fmt.Sprintf("  backups     %d (b abrir) · whitelist=%v · ops %d\n",
		len(m.detail.Backups), s.WhitelistEnabled, len(s.Ops)))
	if s.Motd != "" {
		b.WriteString(fmt.Sprintf("  motd        %s\n", truncate(s.Motd, w-14)))
	}
	return b.String()
}

func (m Model) backupsView(w, h int) string {
	if m.backupsSlug == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Backups de "+m.backupsSlug) + "\n")
	if len(m.backups) == 0 {
		b.WriteString(styleMuted.Render("  (sin backups todavía)"))
		return b.String()
	}
	for i, bk := range m.backups {
		if i >= h-2 {
			break
		}
		b.WriteString(fmt.Sprintf("  %-25s %10s  %s",
			truncate(bk.Name, 25), humanBytes(bk.Size), shortSha(bk.Sha256)))
		if i < len(m.backups)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) consoleView(w, h int) string {
	slug := ""
	if m.detail != nil && m.detail.Spec != nil {
		slug = m.detail.Spec.Slug
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Consola de "+slug) + "\n")
	b.WriteString(styleMuted.Render("  envía comandos al servidor (sin barra: say hola, list, op <jugador>, stop…)\n"))
	b.WriteString("\n")
	b.WriteString(m.input.View())
	return b.String()
}

func (m Model) helpView(w int) string {
	help := [][2]string{
		{"↑/↓ j/k", "seleccionar instancia"},
		{"enter", "abrir detalle"},
		{"s", "start · arrancar (respeta el máximo de paralelas)"},
		{"p", "stop · detener rápido"},
		{"c", "close · apagado con cuidado (guarda el mundo)"},
		{"r", "restart · recrear el contenedor"},
		{"l", "logs · últimas líneas (scroll ↑/↓)"},
		{"b", "backups · listar/crear/podar"},
		{"n", "crear backup (en detalle)"},
		{"e", "consola · enviar comando al servidor (en detalle)"},
		{"d", "salud · doctor tps de las instancias corriendo"},
		{"?", "esta ayuda"},
		{"esc", "volver a la lista"},
		{"q / ctrl+c", "salir"},
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Teclas") + "\n")
	for _, row := range help {
		b.WriteString(fmt.Sprintf("  %-16s %s\n", styleKey.Render(row[0]), row[1]))
	}
	return b.String()
}

// keysView resume las teclas de la pantalla actual.
func (m Model) keysView(w int) string {
	keys := map[mode]string{
		modeList:    "↑/↓ j/k sel · enter detalle · s/p/c/r ops · l logs · b bkps · d salud · ? · q",
		modeDetail:  "esc volver · s/p/c/r ops · e consola · l logs · b bkps · n backup · ? · q",
		modeLogs:    "↑/↓ scroll · r recargar · esc atrás · q salir",
		modeConsole: "enter enviar · esc cancelar",
		modeBackups: "c crear · p podar(5) · esc atrás · q salir",
		modeHelp:    "esc volver",
	}
	hint, _ := keys[m.mode]
	return styleDim.Render(truncate(hint, w))
}

// ---------------------------------------------------------------------------
// utilidades de texto

func cell(s string, width int) string {
	return fmt.Sprintf("%-*s", width, truncate(s, width))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n || n <= 0 {
		if n > 0 && len(r) > n {
			return string(r[:n])
		}
		return s
	}
	return string(r[:n-1]) + "…"
}

func shortSha(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// padTo rellena una cadena (que puede contener ANSI) con espacios hasta el
// ancho visible pedido.
func padTo(s string, width int) string {
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// ansiRe elimina secuencias de escape de terminal: CSI (ESC [ …), OSC (ESC ] …)
// , ESC ( … ) y ESC simple de 1 char.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1b\([0-9A-Za-z]|\x1b[XM]`)

// stripANSI quita secuencias de escape y caracteres de control (C0) del texto
// tal cual llega de docker, dejando solo texto legible.
func stripANSI(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// wrapLogs limpia y encaja el texto de logs a un ancho de columnas: trunca cada
// línea larga con "…" para que el viewport no desborde ni parta palabras.
func wrapLogs(text string, width int) string {
	if width <= 0 {
		width = 80
	}
	lines := strings.Split(stripANSI(text), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		r := []rune(ln)
		if len(r) <= width {
			out = append(out, ln)
			continue
		}
		if width <= 1 {
			out = append(out, "…")
			continue
		}
		out = append(out, string(r[:width-1])+"…")
	}
	return strings.Join(out, "\n")
}

// pinsLabel formatea los pines del motor como "CLAVE=valor" ordenado.
func pinsLabel(pins map[string]string) string {
	if len(pins) == 0 {
		return ""
	}
	keys := make([]string, 0, len(pins))
	for k := range pins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+pins[k])
	}
	return strings.Join(parts, ", ")
}
