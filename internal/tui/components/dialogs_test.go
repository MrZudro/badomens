package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// cuerpoRegen reproduce el texto del diálogo de regeneración (UX-10/§9), largo a
// propósito: en una terminal compacta no cabe entero.
const cuerpoRegen = "Se BORRARÁ el mundo actual de «qa» (Overworld, Nether y End) y se generará otro con " +
	"una semilla aleatoria. Esta acción no se puede deshacer.\n\n" +
	"Política: apagar y regenerar. Si el servidor está en marcha, se DETENDRÁ ahora, " +
	"se borrará el mundo y se volverá a ARRANCAR con la nueva semilla."

func modalRegen() Modal {
	return Modal{
		Title: "Regenerar el mundo (se pierde el actual)",
		Body:  cuerpoRegen,
		Actions: []Action{
			{ID: "confirm", Verb: "Sí, apagar y", Object: "regenerar", Key: "y", Hint: "Detiene, borra y regenera."},
			{ID: "backup", Verb: "Backup antes y", Object: "regenerar", Key: "b", Hint: "Backup previo y regenera."},
			{ID: "cancel", Verb: "No, conservar", Object: "el mundo actual", Key: "n", Hint: "Cierra sin tocar el mundo."},
		},
		Focused: 2,
	}
}

// UX-05/§5.15: en una terminal compacta el modal reserva la altura de su barra de
// acciones; el cuerpo cede (y marca el recorte con «…»), pero las acciones siguen
// visibles y el cuadro se cierra: el usuario nunca confirma a ciegas.
func TestModalCompactoMantieneLasAccionesVisibles(t *testing.T) {
	acciones := []string{
		"[Y] Sí, apagar y regenerar",
		"[B] Backup antes y regenerar",
		"[N] No, conservar el mundo actual",
	}
	for h := 6; h <= 20; h++ {
		out := ansi.Strip(modalRegen().RenderFit(56, h, pal(), false))
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Fatalf("h=%d: el modal no puede ocupar %d líneas:\n%s", h, len(lines), out)
		}
		if last := lines[len(lines)-1]; !strings.HasPrefix(last, "╰") || !strings.HasSuffix(strings.TrimRight(last, " "), "╯") {
			t.Fatalf("h=%d: el borde inferior debe verse: %q", h, last)
		}
		for _, want := range acciones {
			if !strings.Contains(out, want) {
				t.Fatalf("h=%d: la acción %q debe ser visible:\n%s", h, want, out)
			}
		}
	}
}

// El recorte no pierde el aviso: con un alto que obliga a recortar, el cuerpo
// conserva el principio del aviso y marca con «…» que hay más texto.
func TestModalCompactoMarcaElCuerpoRecortado(t *testing.T) {
	out := ansi.Strip(modalRegen().RenderFit(56, 10, pal(), false))
	if !strings.Contains(out, "Se BORRARÁ el mundo actual") {
		t.Fatalf("el aviso de pérdida de datos debe seguir viéndose:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("el recorte del cuerpo debe marcarse con «…»:\n%s", out)
	}
}

// Si el modal cabe en el alto disponible, RenderFit devuelve EXACTAMENTE lo mismo
// que Render (el camino compacto no altera las capturas normales).
func TestModalQueCabeNoCambia(t *testing.T) {
	m := modalRegen()
	full := m.Render(56, pal(), false)
	if got := m.RenderFit(56, strings.Count(full, "\n")+1, pal(), false); got != full {
		t.Fatalf("un modal que cabe no debe cambiar:\n--- Render ---\n%s\n--- RenderFit ---\n%s", full, got)
	}
}

// La confirmación reforzada sigue pidiendo el texto exacto también en compacto.
func TestModalCompactoConservaLaConfirmacionReforzada(t *testing.T) {
	m := DestroyConfirm("eliminar", "survival")
	m.Body = strings.Repeat("Se eliminarán el contenedor y los datos. ", 8)
	out := ansi.Strip(m.RenderFit(56, 12, pal(), false))
	if !strings.Contains(out, "Escribe «survival»: ") {
		t.Fatalf("el texto exigido debe seguir visible en compacto:\n%s", out)
	}
	if !strings.Contains(out, "[N] No, volver") {
		t.Fatalf("las acciones deben seguir visibles en compacto:\n%s", out)
	}
}
