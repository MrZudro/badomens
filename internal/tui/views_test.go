package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapLogsLimpiaAnsiYTrunca(t *testing.T) {
	in := "\x1b[1;31mbadomens-demo | [init] ERROR something\x1b[m\x1b(B\nseguir\x08ok\t"
	out := wrapLogs(in, 60)
	for _, bad := range []string{"\x1b[", "something\x1b", "\x08"} {
		if strings.Contains(out, bad) {
			t.Fatalf("wrapLogs dejó %q en la salida:\n%q", bad, out)
		}
	}
	if !strings.Contains(out, "ERROR something") {
		t.Fatalf("quiero el texto legible: %q", out)
	}
	// línea muy larga se trunca con elipsis y nunca supera el ancho
	long := strings.Repeat("x", 100)
	got := strings.Split(wrapLogs(long, 40), "\n")[0]
	if lipgloss.Width(got) > 41 {
		t.Fatalf("la línea truncada mide %d (ancho pedido 40): %q", lipgloss.Width(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("quiero elipsis final, tengo %q", got)
	}
}

func TestStripAnsiQuitaControles(t *testing.T) {
	in := "a\x1b[31mb\x1b[m c\x1b(Bd\x08e\nf\x00g"
	out := stripANSI(in)
	for _, bad := range []string{"\x1b", "\x08", "\x00"} {
		if strings.Contains(out, bad) {
			t.Fatalf("stripANSI dejó control %q: %q", bad, out)
		}
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "e") {
		t.Fatalf("stripANSI perdió texto legible: %q", out)
	}
}

func TestPinsLabelOrdenaYFormatea(t *testing.T) {
	got := pinsLabel(map[string]string{"PAPER_BUILD": "143", "FORGE_VERSION": "1.20.1"})
	if got != "FORGE_VERSION=1.20.1, PAPER_BUILD=143" {
		t.Fatalf("pinsLabel: quiero orden alfabético, tengo %q", got)
	}
	if pinsLabel(nil) != "" {
		t.Fatal("pinsLabel(nil) debe ser vacío")
	}
}

func TestListaCabeEnElAncho(t *testing.T) {
	f := newFake()
	m := nuevoTestModel(t, f)
	for _, sel := range []bool{false, true} {
		for _, r := range m.rows {
			line := m.rowLine(r, sel, 84)
			if lipgloss.Width(line) > 84 {
				t.Fatalf("fila %s (selected=%v) mide %d > 84:\n%q", r.Slug, sel, lipgloss.Width(line), line)
			}
		}
	}
}

func TestVistaDetalleSinMapaCrudo(t *testing.T) {
	f := newFake()
	f.detail = &Detail{Spec: rowSpec("alpha"), State: "running"}
	m := nuevoTestModel(t, f)
	m.detail = f.detail
	m.mode = modeDetail
	v := m.View()
	if strings.Contains(v, "map[") {
		t.Fatalf("el detalle no debe mostrar un map crudo:\n%s", v)
	}
}

// TestTodasLasVistasCabenEn84 verifica que ninguna línea visible de ningún
// modo supere el ancho objetivo del panel.
func TestTodasLasVistasCabenEn84(t *testing.T) {
	f := newFake()
	f.detail = &Detail{Spec: rowSpec("alpha"), State: "running"}
	m := nuevoTestModel(t, f)
	m.width, m.height = 84, 26

	detail := m
	detail.detail = f.detail
	detail.mode = modeDetail

	for _, tc := range []struct {
		name string
		mod  Model
	}{
		{"list", m},
		{"detail", detail},
	} {
		frame := stripANSI(tc.mod.View())
		for i, line := range strings.Split(frame, "\n") {
			if length := len([]rune(line)); length > 84 {
				t.Fatalf("vista %s línea %d mide %d > 84:\n%q", tc.name, i, length, line)
			}
		}
	}
}
