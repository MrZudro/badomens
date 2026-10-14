package result

import (
	"encoding/json"
	"testing"

	"github.com/MrZudro/badomens/internal/errx"
)

// TestContratoClavesEstables comprueba que el documento JSON conserva las claves
// estables del contrato (RF/§10.3). Si alguien añade o quita una clave de nivel
// superior, el test falla: los cambios deben ser aditivos dentro de data/error.
func TestContratoClavesEstables(t *testing.T) {
	ok := OK("demo", "hecho", map[string]any{"campo": 1}).WithPhases([]Phase{{Name: "paso", Status: "ok"}})
	raw, err := json.Marshal(ok)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	want := []string{"ok", "exit_code", "action", "message", "data", "phases", "timestamp"}
	for _, k := range want {
		if _, exists := doc[k]; !exists {
			t.Fatalf("falta la clave estable %q en el documento OK: %v", k, doc)
		}
	}
	if len(doc) != len(want) {
		t.Fatalf("hay claves de nivel superior no esperadas: %v", doc)
	}

	errDoc := FromError("demo", errx.Usage("uso incorrecto", "revisa los flags"))
	eraw, _ := json.Marshal(errDoc)
	var edoc map[string]any
	_ = json.Unmarshal(eraw, &edoc)
	for _, k := range []string{"ok", "exit_code", "action", "message", "error", "timestamp"} {
		if _, exists := edoc[k]; !exists {
			t.Fatalf("falta la clave %q en el documento de error: %v", k, edoc)
		}
	}
	ei, okCast := edoc["error"].(map[string]any)
	if !okCast {
		t.Fatal("el error debe ser un objeto")
	}
	for _, k := range []string{"code", "message"} {
		if _, exists := ei[k]; !exists {
			t.Fatalf("falta la clave error.%s: %v", k, ei)
		}
	}
}
