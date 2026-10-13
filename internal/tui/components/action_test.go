package components

import (
	"testing"
)

// TestLintDeBotones (RF-7/§10.3): las etiquetas prohibidas a solas, la falta de
// Hint y Disabled sin DisabledReason deben detectarse.
func TestLintDeBotones(t *testing.T) {
	good := Action{
		ID: "instance.start", Verb: "Iniciar", Object: "«Survival»", Key: "s",
		Hint: "Arranca el servidor y recalculas la información de conexión.",
	}
	if inv := good.Invalid(); inv {
		t.Fatalf("una acción correcta no debe fallar: %v", good.Validate())
	}

	sueltas := []Action{
		{ID: "a1", Verb: "OK", Key: "o", Hint: "acepta"},
		{ID: "a2", Verb: "Aceptar", Key: "a", Hint: "acepta"},
		{ID: "a3", Verb: "Sí", Key: "s", Hint: "confirma"},
		{ID: "a4", Verb: "Cancelar", Key: "c", Hint: "vuelve"},
		{ID: "a5", Verb: "Siguiente", Key: "n", Hint: "avanza"},
	}
	for _, a := range sueltas {
		if len(a.Validate()) == 0 {
			t.Fatalf("etiqueta genérica «%s» a solas debe fallar el lint", a.Verb)
		}
	}

	sinHint := Action{ID: "i", Verb: "Iniciar", Object: "«X»", Key: "s"}
	if len(sinHint.Validate()) == 0 {
		t.Fatal("una acción sin Hint debe fallar")
	}

	disabledSinMotivo := Action{ID: "j", Verb: "Iniciar", Object: "«X»", Key: "s", Hint: "arranca", Disabled: true}
	if len(disabledSinMotivo.Validate()) == 0 {
		t.Fatal("Disabled sin DisabledReason debe fallar")
	}

	disabledConMotivo := Action{ID: "k", Verb: "Iniciar", Object: "«X»", Key: "s", Hint: "arranca", Disabled: true, DisabledReason: "Docker no está activo — pulsa H para repararlo"}
	if len(disabledConMotivo.Validate()) != 0 {
		t.Fatalf("una acción deshabilitada con motivo es correcta: %v", disabledConMotivo.Validate())
	}

	hintLargo := Action{ID: "l", Verb: "Iniciar", Object: "«X»", Key: "s", Hint: rol(91)}
	if len(hintLargo.Validate()) == 0 {
		t.Fatal("un Hint > 90 caracteres debe fallar")
	}
}

func rol(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

// TestLabelComponeVerboYObjeto.
func TestLabelComponeVerboYObjeto(t *testing.T) {
	a := Action{Verb: "Iniciar", Object: "«Survival»"}
	if a.Label() != "Iniciar «Survival»" {
		t.Fatalf("label: %q", a.Label())
	}
}

// TestKindsEstables.
func TestKindsEstables(t *testing.T) {
	cases := map[Kind]string{Secondary: "secondary", Primary: "primary", Destructive: "destructive", Navigation: "navigation"}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Fatalf("Kind(%d): quiero %q, tengo %q", int(k), want, got)
		}
	}
}
