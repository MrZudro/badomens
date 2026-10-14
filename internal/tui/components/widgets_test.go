package components

import (
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/connection"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

func pal() theme.Palette { return theme.For(theme.Dark) }

func TestButtonMuestraTeclaYObjeto(t *testing.T) {
	a := Action{ID: "instance.start", Verb: "Iniciar", Object: "«Survival»", Key: "s", Hint: "arranca", Kind: Primary}
	out := Button(a, false, pal())
	if !strings.Contains(out, "[S]") || !strings.Contains(out, "Iniciar «Survival»") {
		t.Fatalf("el botón debe mostrar tecla y etiqueta: %q", out)
	}
}

func TestButtonDeshabilitadoMuestraMotivo(t *testing.T) {
	a := Action{ID: "x", Verb: "Iniciar", Object: "«S»", Key: "s", Hint: "h", Disabled: true, DisabledReason: "Docker no está activo"}
	out := Button(a, false, pal())
	if !strings.Contains(out, "Docker no está activo") {
		t.Fatalf("deshabilitado ≠ oculto: debe verse el motivo: %q", out)
	}
}

func TestActionBarCompactReduceYAnadeMas(t *testing.T) {
	acts := []Action{
		{ID: "1", Verb: "A", Object: "o", Key: "a", Hint: "h"},
		{ID: "2", Verb: "B", Object: "o", Key: "b", Hint: "h"},
		{ID: "3", Verb: "C", Object: "o", Key: "c", Hint: "h"},
		{ID: "4", Verb: "D", Object: "o", Key: "d", Hint: "h"},
		{ID: "5", Verb: "E", Object: "o", Key: "e", Hint: "h"},
	}
	out := ActionBar(acts, 50, -1, pal()) // ancho Compact
	if !strings.Contains(out, "Más") {
		t.Fatalf("en Compact debe haber «Más…»: %q", out)
	}
	if strings.Contains(out, "[D]") {
		t.Fatalf("en Compact no deben caber todos: %q", out)
	}
}

func TestSparklineDibuja(t *testing.T) {
	out := Sparkline([]float64{1, 2, 3, 4, 5, 6, 7, 8}, 8)
	if len([]rune(out)) != 8 {
		t.Fatalf("quiero 8 bloques, tengo %q", out)
	}
	if Sparkline(nil, 5) != "" {
		t.Fatal("sin datos no hay gráfica")
	}
}

func TestModalDestructivoSeguroPorDefecto(t *testing.T) {
	m := DestroyConfirm("borrar la instancia", "survival")
	if m.Focused != 1 {
		t.Fatal("el foco por defecto debe estar en el botón seguro (No, volver)")
	}
	if m.Enabled() {
		t.Fatal("sin escribir el nombre no debe habilitarse")
	}
	m.Typed = "survival"
	if !m.Enabled() {
		t.Fatal("al escribir el nombre debe habilitarse")
	}
	out := m.Render(70, pal(), false)
	if !strings.Contains(out, "No, volver") || !strings.Contains(out, "para siempre") {
		t.Fatalf("el modal debe tener botones inequívocos: %q", out)
	}
}

func TestProgressPinta(t *testing.T) {
	out := Progress{Percent: 0.5, Width: 10, Label: "creando"}.Render(pal())
	if !strings.Contains(out, "50%") || !strings.Contains(out, "creando") {
		t.Fatalf("progreso: %q", out)
	}
}

func TestConnectCard(t *testing.T) {
	info := connection.Info{
		Name: "Survival", McVersion: "1.21.4", Address: "100.64.1.7:25565",
		AddressDNS: "maquina.tail.ts.net:25565", OnlineMode: false,
		Warnings: []string{"MagicDNS desactivado"},
	}
	out := ConnectCard(info, 60, pal(), false)
	for _, want := range []string{"Cómo conectarse", "100.64.1.7:25565", "offline", "MagicDNS desactivado"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tarjeta de conexión sin %q:\n%s", want, out)
		}
	}
}

func TestPickerFiltraYSelecciona(t *testing.T) {
	p := NewPicker("Motor", []PickerItem{
		{Label: "Paper", Value: "paper"}, {Label: "Fabric", Value: "fabric"}, {Label: "Vanilla", Value: "vanilla"},
	})
	p.Type('f')
	if got := p.Filtered(); len(got) != 1 || got[0].Value != "fabric" {
		t.Fatalf("filtro 'f': %+v", got)
	}
	p.Backspace()
	if len(p.Filtered()) != 3 {
		t.Fatal("al borrar el filtro vuelven todos")
	}
	p.Move(1)
	sel, ok := p.Selected()
	if !ok || sel.Value != "fabric" {
		t.Fatalf("selección: %+v ok=%v", sel, ok)
	}
	if !strings.Contains(p.Render(40, 10, pal(), false), "Fabric") {
		t.Fatal("el render debe listar los elementos")
	}
}

func TestFormMuestraDescripcionCorta(t *testing.T) {
	f := NewForm("Básico", []Field{
		{Key: "name", Label: "Nombre", Short: "Identifica tu servidor.", Value: "demo"},
		{Key: "memory", Label: "Memoria", Short: "RAM máxima del servidor.", Value: "4G"},
	})
	out := f.Render(60, pal())
	for _, want := range []string{"Nombre", "Identifica tu servidor.", "Memoria", "RAM máxima del servidor."} {
		if !strings.Contains(out, want) {
			t.Fatalf("el formulario debe mostrar %q:\n%s", want, out)
		}
	}
	f.Move(1)
	if cur, _ := f.Current(); cur.Key != "memory" {
		t.Fatalf("cursor: %+v", cur)
	}
}

func TestBadgeYStepper(t *testing.T) {
	if !strings.Contains(Badge("running", pal(), false), "En línea") {
		t.Fatal("la insignia en línea debe llevar texto")
	}
	if !strings.Contains(Badge("idle", pal(), true), "Dormido") {
		t.Fatal("la insignia ASCII de dormido debe llevar texto")
	}
	step := Stepper([]string{"Básico", "Mundo", "Recursos"}, 1, pal())
	if !strings.Contains(step, "1 Básico") || !strings.Contains(step, "2 Mundo") {
		t.Fatalf("stepper: %q", step)
	}
}
