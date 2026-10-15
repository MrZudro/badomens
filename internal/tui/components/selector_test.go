package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/MrZudro/badomens/internal/tui/layout"
)

func sampleItems() []SelectItem {
	return []SelectItem{
		{Value: "1.21.4", Label: "1.21.4", Detail: "release", Recommended: true},
		{Value: "1.21.1", Label: "1.21.1"},
		{Value: "1.20.6", Label: "1.20.6"},
		{Value: "1.20.1", Label: "1.20.1"},
		{Value: "26.1-snap", Label: "26.1-snap", Disabled: true, DisabledWhy: "sin artefacto"},
	}
}

// P-01/P-03: la lista vertical se navega con ↑/↓ y expone la selección por VALOR.
func TestSelectorListaVerticalPorValor(t *testing.T) {
	s := NewSelector("Versiones", sampleItems())
	if s.Value() != "1.21.4" {
		t.Fatalf("el cursor arranca en el primer valor, tengo %q", s.Value())
	}
	s.Move(1)
	s.Move(1)
	if s.Value() != "1.20.6" {
		t.Fatalf("tras ↓↓ el valor debe ser 1.20.6, tengo %q", s.Value())
	}
	s.Move(-1)
	if s.Value() != "1.21.1" {
		t.Fatalf("tras ↑ el valor debe ser 1.21.1, tengo %q", s.Value())
	}
	// Los extremos no se salen de la lista.
	s.Move(-10)
	if s.Cursor() != 0 {
		t.Fatalf("↑ no debe salir de la lista, cursor = %d", s.Cursor())
	}
	s.Move(10)
	if s.Cursor() != s.Len()-1 {
		t.Fatalf("↓ no debe salir de la lista, cursor = %d", s.Cursor())
	}
	// Restaurar por valor solo mueve el cursor.
	if !s.SetValue("1.20.6") || s.Value() != "1.20.6" {
		t.Fatalf("SetValue debe seleccionar por valor, tengo %q", s.Value())
	}
	if s.SetValue("no-existe") {
		t.Fatal("SetValue debe devolver false si el valor no está")
	}
}

// P-01: en rejilla funcionan las cuatro flechas y las columnas se adaptan al ancho.
func TestSelectorGridCuatroFlechas(t *testing.T) {
	s := NewSelectorMode("Versiones", sampleItems(), SelectGrid)
	s.Cols = 3 // rejilla determinista para probar la navegación
	cols := s.GridColumns(60, "")
	if cols != 3 {
		t.Fatalf("quiero 3 columnas forzadas, tengo %d", cols)
	}
	if natural := NewSelectorMode("Versiones", sampleItems(), SelectGrid).GridColumns(60, ""); natural < 2 {
		t.Fatalf("con 60 celdas deben caber varias columnas por defecto, tengo %d", natural)
	}
	s.SetCursor(0)
	s.MoveGrid(1, 0, cols) // →
	if s.Cursor() != 1 {
		t.Fatalf("→ debe avanzar una columna, cursor = %d", s.Cursor())
	}
	s.MoveGrid(0, 1, cols) // ↓
	if s.Cursor() != 1+cols {
		t.Fatalf("↓ debe bajar una fila (cursor=%d, quiero %d)", s.Cursor(), 1+cols)
	}
	s.MoveGrid(-1, 0, cols) // ←
	if s.Cursor() != cols {
		t.Fatalf("← debe retroceder una columna, cursor = %d", s.Cursor())
	}
	// ←/→ no escapan de la fila.
	s.SetCursor(0)
	s.MoveGrid(-1, 0, cols)
	if s.Cursor() != 0 {
		t.Fatalf("← en la primera columna no debe moverse, cursor = %d", s.Cursor())
	}
	// Columnas adaptables: menos ancho, menos columnas (sin forzar Cols).
	auto := NewSelectorMode("Versiones", sampleItems(), SelectGrid)
	wide, narrow := auto.GridColumns(80, ""), auto.GridColumns(12, "")
	if narrow > wide {
		t.Fatalf("con menos ancho no pueden caber más columnas (%d vs %d)", narrow, wide)
	}
	if narrow != 1 {
		t.Fatalf("en 12 celdas debe quedar una sola columna, tengo %d", narrow)
	}
}

// La rejilla queda alineada: ancho de columna fijo y todas las filas del mismo ancho.
func TestSelectorGridAlineado(t *testing.T) {
	s := NewSelectorMode("Versiones", sampleItems(), SelectGrid)
	lines := s.RenderGrid(70, "1.21.1", pal(), false)
	if len(lines) == 0 {
		t.Fatal("la rejilla debe pintar al menos una fila")
	}
	first := layout.Width(lines[0])
	for i, ln := range lines {
		if w := layout.Width(ln); w > first {
			t.Fatalf("la fila %d mide %d celdas, más que la primera (%d): %q", i, w, first, ln)
		}
	}
}

// La lista marca la opción guardada como «(actual)» y la enfocada con puntero.
func TestSelectorListaMarcaActualYPuntero(t *testing.T) {
	s := NewSelector("Dificultad", []SelectItem{
		{Value: "peaceful", Label: "Pacífico"},
		{Value: "normal", Label: "Normal"},
	})
	s.SetValue("normal")
	out := strings.Join(s.RenderList(40, "normal", pal(), false), "\n")
	if !strings.Contains(out, "(actual)") {
		t.Fatalf("la opción guardada debe marcarse «(actual)»:\n%s", out)
	}
	if !strings.Contains(out, Pointer(false)) {
		t.Fatalf("la opción enfocada debe llevar puntero:\n%s", out)
	}
	if !strings.Contains(out, "Normal") {
		t.Fatalf("las etiquetas deben verse:\n%s", out)
	}
}

// Una opción deshabilitada explica el motivo y no se elige por accidente.
func TestSelectorDeshabilitadoExplicaMotivo(t *testing.T) {
	s := NewSelector("Versiones", []SelectItem{{Value: "x", Label: "x", Disabled: true, DisabledWhy: "sin artefacto"}})
	out := strings.Join(s.RenderList(40, "", pal(), false), "\n")
	if !strings.Contains(out, "sin artefacto") {
		t.Fatalf("la opción deshabilitada debe explicar el motivo:\n%s", out)
	}
	it, ok := s.Selected()
	if !ok || !it.Disabled {
		t.Fatal("Selected debe devolver el elemento con su estado Disabled")
	}
}

// TestSelectorGridAgrupadoNavega: con cabeceras de grupo, la rejilla mantiene la
// alineación, cada grupo se reparte en sus propias filas y las cuatro flechas
// navegan sin cruzar a una columna inexistente.
func TestSelectorGridAgrupadoNavega(t *testing.T) {
	items := []SelectItem{
		{Value: "1.21.4", Label: "1.21.4", Group: "Minecraft 1.21.x"},
		{Value: "1.21.1", Label: "1.21.1", Group: "Minecraft 1.21.x"},
		{Value: "1.20.6", Label: "1.20.6", Group: "Minecraft 1.20.x"},
		{Value: "26.2-snap", Label: "26.2-snap", Group: "Snapshots"},
	}
	s := NewSelectorMode("Versiones", items, SelectGrid)
	s.Cols = 2
	cols := s.GridColumns(60, "")
	if cols != 2 {
		t.Fatalf("quiero 2 columnas forzadas, tengo %d", cols)
	}
	lines := s.RenderGrid(60, "", pal(), false)
	out := strings.Join(lines, "\n")
	for _, want := range []string{"MINECRAFT 1.21.X", "MINECRAFT 1.20.X", "SNAPSHOTS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("la cabecera de grupo %q debe verse:\n%s", want, out)
		}
	}
	// 3 cabeceras (una por grupo) + 3 filas de celdas (2+1+1 opciones con 2 columnas).
	if len(lines) != 6 {
		t.Fatalf("la rejilla agrupada debe ocupar 6 líneas, tengo %d:\n%s", len(lines), out)
	}
	first := layout.Width(lines[1])
	for i, ln := range lines {
		if layout.Width(ln) > first {
			t.Fatalf("la fila %d mide %d celdas, más que la primera fila de celdas (%d): %q", i, layout.Width(ln), first, ln)
		}
	}
	// → no escapa de la fila (el grupo 1.21.x solo tiene dos celdas).
	s.SetCursor(1)
	s.MoveGrid(1, 0, cols)
	if s.Cursor() != 1 {
		t.Fatalf("→ en la última columna de la fila no debe moverse, cursor = %d", s.Cursor())
	}
	// ↓ baja a la fila del grupo siguiente.
	s.SetCursor(0)
	s.MoveGrid(0, 1, cols)
	if s.Value() != "1.20.6" {
		t.Fatalf("↓ debe bajar a la fila del siguiente grupo, tengo %q", s.Value())
	}
	// ↑ vuelve al grupo anterior manteniendo la columna.
	s.MoveGrid(0, -1, cols)
	if s.Cursor() != 0 {
		t.Fatalf("↑ debe volver a la primera fila, cursor = %d", s.Cursor())
	}
}

// UX-03/SEL-08: el ancho de columna de la rejilla es FIJO y todas las celdas se
// rellenan a su columna, así que la columna k arranca siempre en la misma celda
// de pantalla en TODAS las filas, sin depender de la longitud del texto (mismo
// criterio de relleno que el grid de la Ayuda). Además, la separación entre
// columnas es visible (≥ 2 celdas).
func TestSelectorGridColumnasAlineadasConTextosDeDistintaLongitud(t *testing.T) {
	items := []SelectItem{
		{Value: "1.7", Label: "1.7"},
		{Value: "26.1-snapshot-largo", Label: "26.1-snapshot-largo"},
		{Value: "1.21.4", Label: "1.21.4", Recommended: true},
		{Value: "x", Label: "x"},
		{Value: "1.20.6", Label: "1.20.6", Disabled: true, DisabledWhy: "sin artefacto"},
		{Value: "1.20.1", Label: "1.20.1"},
		{Value: "1.19", Label: "1.19"},
	}
	const current = "1.21.4"
	s := NewSelectorMode("VERSIONES", items, SelectGrid)
	s.Cols = 3
	s.SetCursor(2)
	colW := s.ColumnWidth(current)
	if colW < 8 {
		t.Fatalf("el ancho de columna no puede ser menor que 8, tengo %d", colW)
	}
	rendered := s.RenderGrid(70, current, pal(), false)
	vis := s.lines(3)
	if len(vis) != len(rendered) {
		t.Fatalf("cada línea visual debe tener su línea pintada (%d vs %d)", len(vis), len(rendered))
	}
	for i, vl := range vis {
		if vl.header != "" {
			continue
		}
		plain := ansi.Strip(rendered[i])
		chunks := cellsOfWidth(plain, colW)
		for k, idx := range vl.items {
			want := s.Items[idx].cellText(current)
			if !strings.HasPrefix(chunks[k], want) {
				t.Fatalf("la celda %d de la fila %q debe arrancar en la columna %d con %q:\n%s",
					k, plain, k*colW, want, plain)
			}
			if k < len(vl.items)-1 {
				used := layout.Width(strings.TrimRight(chunks[k], " "))
				if gap := layout.Width(chunks[k]) - used; gap < 2 {
					t.Fatalf("las columnas deben separarse de forma visible (hueco=%d): %q", gap, plain)
				}
			}
		}
	}
	// El ancho de columna no depende de la celda enfocada: mover el cursor no
	// cambia la rejilla.
	s.SetCursor(0)
	if got := s.ColumnWidth(current); got != colW {
		t.Fatalf("el ancho de columna debe ser el mismo con el cursor en otra celda (%d vs %d)", got, colW)
	}
}

// cellsOfWidth divide una línea ya sin ANSI en trozos de `w` celdas visuales, que
// es exactamente lo que ocupa cada columna de la rejilla.
func cellsOfWidth(plain string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	var cur strings.Builder
	used := 0
	for _, r := range plain {
		rw := layout.Width(string(r))
		if used+rw > w {
			out = append(out, cur.String())
			cur.Reset()
			used = 0
		}
		cur.WriteRune(r)
		used += rw
	}
	return append(out, cur.String())
}

// UX-03: en modo rejilla el detalle de una opción no cabe en una celda de ancho
// fijo; FocusedDetail lo expone para pintarlo en su propia línea, de modo que
// «última estable», «incompatible» o «sin verificar» siguen distinguiéndose.
func TestSelectorFocusedDetailAportaElDetalleEnRejilla(t *testing.T) {
	items := []SelectItem{
		{Value: "1.21.4", Label: "1.21.4", Detail: "Java 21 · build 123 · última estable", Recommended: true},
		{Value: "1.7.10", Label: "1.7.10", Detail: "Java 8 · incompatible", Disabled: true, DisabledWhy: "sin artefacto para paper"},
		{Value: "1.20.1", Label: "1.20.1", Detail: "Java 17 · sin verificar"},
	}
	s := NewSelectorMode("VERSIONES", items, SelectGrid)
	s.Cols = 2
	if got := s.FocusedDetail(); !strings.Contains(got, "última estable") {
		t.Fatalf("la opción enfocada debe aportar su detalle, tengo %q", got)
	}
	s.SetCursor(1)
	got := s.FocusedDetail()
	if !strings.Contains(got, "incompatible") || !strings.Contains(got, "sin artefacto para paper") {
		t.Fatalf("una opción no elegible debe aportar el detalle y el motivo, tengo %q", got)
	}
	s.SetCursor(2)
	if got := s.FocusedDetail(); !strings.Contains(got, "sin verificar") {
		t.Fatalf("una opción sin verificar debe decirlo, tengo %q", got)
	}
	// El detalle no se mete en la celda (rompería la alineación): en la rejilla
	// solo van las marcas breves.
	out := strings.Join(s.RenderGrid(70, "", pal(), false), "\n")
	plain := ansi.Strip(out)
	if strings.Contains(plain, "última estable") || strings.Contains(plain, "sin artefacto") {
		t.Fatalf("el detalle largo no debe pintarse dentro de la celda:\n%s", plain)
	}
	if !strings.Contains(plain, "✖") {
		t.Fatalf("una opción no elegible debe llevar su marca en la rejilla:\n%s", plain)
	}
}
