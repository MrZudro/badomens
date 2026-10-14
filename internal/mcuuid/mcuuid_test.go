package mcuuid

import (
	"regexp"
	"strings"
	"testing"
)

// uuidRe valida el formato canónico en minúsculas.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// TestOfflineVectoresConocidos fija el algoritmo con vectores calculados de
// forma independiente (md5 de "OfflinePlayer:"+nombre): si alguien cambia la
// implementación, el test lo detecta.
func TestOfflineVectoresConocidos(t *testing.T) {
	casos := []struct{ nombre, quiero string }{
		{"Notch", "b50ad385-829d-3141-a216-7e7d7539ba7f"},
		{"jeb_", "a762f560-4fce-3236-812a-b80efff0b62b"},
		{"Steve", "5627dd98-e6be-3c21-b8a8-e92344183641"},
		{"Dinnerbone", "4d258a81-2358-3084-8166-05b9faccad80"},
	}
	for _, c := range casos {
		if got := Offline(c.nombre); got != c.quiero {
			t.Errorf("Offline(%q) = %q, quiero %q", c.nombre, got, c.quiero)
		}
	}
}

// TestOfflinePropiedadesUUIDv3 comprueba que el resultado es un UUID v3 bien
// formado: minúsculas, 36 caracteres, versión 3 y variante RFC 4122.
func TestOfflinePropiedadesUUIDv3(t *testing.T) {
	got := Offline("UnNombreCualquiera")
	if !uuidRe.MatchString(got) {
		t.Fatalf("Offline devolvió %q; no es un UUID en minúsculas", got)
	}
	if v := got[14]; v != '3' {
		t.Errorf("versión = %q, quiero '3' (UUID v3)", string(v))
	}
	if c := got[19]; !strings.ContainsRune("89ab", rune(c)) {
		t.Errorf("variante = %q, quiero 8/9/a/b (RFC 4122)", string(c))
	}
}

// TestOfflineDeterministaYVacio comprueba que el cálculo es estable, que
// distingue mayúsculas de minúsculas y que un nombre vacío no produce UUID.
func TestOfflineDeterministaYVacio(t *testing.T) {
	a, b := Offline("Notch"), Offline("Notch")
	if a != b {
		t.Error("Offline no es determinista")
	}
	if Offline("notch") == Offline("Notch") {
		t.Error("Offline debe distinguir mayúsculas de minúsculas")
	}
	for _, vacio := range []string{"", " ", "\t"} {
		if got := Offline(vacio); got != "" {
			t.Errorf("Offline(%q) = %q, quiero cadena vacía", vacio, got)
		}
	}
}
