package engines

import "testing"

// TestJavaImagenPorVersion comprueba que la imagen de itzg se adapta al Java que
// requiere cada versión de Minecraft (RF-5) y que se conserva el repositorio.
func TestJavaImagenPorVersion(t *testing.T) {
	casos := []struct {
		mc       string
		fallback string
		want     string
	}{
		{"1.21.4", "itzg/minecraft-server:java25", "itzg/minecraft-server:java21"},
		{"1.20.5", "itzg/minecraft-server:java25", "itzg/minecraft-server:java21"},
		{"1.20.1", "itzg/minecraft-server:java25", "itzg/minecraft-server:java17"},
		{"1.19", "itzg/minecraft-server:java25", "itzg/minecraft-server:java17"},
		{"1.17.1", "itzg/minecraft-server:java25", "itzg/minecraft-server:java16"},
		{"1.16.5", "itzg/minecraft-server:java25", "itzg/minecraft-server:java8"},
		{"desconocida", "itzg/minecraft-server:java25", "itzg/minecraft-server:java25"},
		{"1.21", "", "itzg/minecraft-server:java21"},
		{"", "mi/repo:tag", "mi/repo:tag"},
	}
	for _, c := range casos {
		if got := JavaImageFor(c.mc, c.fallback); got != c.want {
			t.Errorf("JavaImageFor(%q, %q) = %q, quiero %q", c.mc, c.fallback, got, c.want)
		}
	}
}

// TestJavaMajorFour comprueba la compatibilidad hacia atrás de JavaMajorFor.
func TestJavaMajorFour(t *testing.T) {
	if got := JavaMajorFor("1.21.4"); got != 21 {
		t.Errorf("JavaMajorFor(1.21.4) = %d, quiero 21", got)
	}
	if got := JavaMajorFor("1.20.1"); got != 17 {
		t.Errorf("JavaMajorFor(1.20.1) = %d, quiero 17", got)
	}
	if got := JavaMajorFor("no-es-una-version"); got != 8 {
		t.Errorf("JavaMajorFor(desc.) = %d, quiero 8", got)
	}
}

// TestJavaTag cae al vacío con Java desconocido, para no inventar etiquetas.
func TestJavaTag(t *testing.T) {
	if got := JavaTag(21); got != "java21" {
		t.Errorf("JavaTag(21) = %q", got)
	}
	if got := JavaTag(99); got != "" {
		t.Errorf("JavaTag(99) = %q, quiero vacío", got)
	}
}
