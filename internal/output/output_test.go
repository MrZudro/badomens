package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/result"
)

// A11Y-06 (§6.1, «salida estructurada»): el modo JSON imprime UN ÚNICO documento
// con las claves estables del contrato; el modo humano imprime texto legible y
// anota el error en stderr para que sea visible en una terminal.
func TestSalidaEstructuradaYHumana(t *testing.T) {
	var out, errBuf bytes.Buffer
	p := New(&out, &errBuf, JSON)
	if !p.IsJSON() {
		t.Fatal("el printer JSON debe reportarse como JSON")
	}
	res := result.OK("setup", "hecho", map[string]any{"slug": "demo"}).
		WithPhases([]result.Phase{{Name: "created", Status: "ok"}})
	p.Emit(res, "texto humano ignorado en modo JSON")
	raw := strings.TrimSpace(out.String())
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("la salida JSON debe ser un único documento: %v\n%s", err, raw)
	}
	for _, k := range []string{"ok", "exit_code", "action", "message", "data", "phases", "timestamp"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("falta la clave estable %q en %s", k, raw)
		}
	}
	if errBuf.Len() != 0 {
		t.Fatalf("el modo JSON no escribe en stderr: %q", errBuf.String())
	}

	// Modo humano: texto por stdout; sin error no toca stderr.
	out.Reset()
	errBuf.Reset()
	ph := New(&out, &errBuf, Human)
	if ph.IsJSON() {
		t.Fatal("el printer humano no debe reportarse como JSON")
	}
	ph.Emit(result.OK("setup", "hecho", nil), "Instancia demo creada")
	if got := strings.TrimSpace(out.String()); got != "Instancia demo creada" {
		t.Fatalf("texto humano: %q", got)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("sin error no debe escribir en stderr: %q", errBuf.String())
	}

	// El error se ve en stdout Y se anota en stderr.
	out.Reset()
	errBuf.Reset()
	ph.Error("setup", errx.Usage("falta el nombre", "usa --name"))
	if !strings.Contains(out.String(), "falta el nombre") {
		t.Fatalf("el error debe verse en stdout (modo humano): %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "falta el nombre") {
		t.Fatalf("el error debe anotarse en stderr: %q", errBuf.String())
	}
}
