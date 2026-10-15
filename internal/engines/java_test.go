package engines

import "testing"

// TestJavaImagenPorVersion comprueba que la imagen de itzg se adapta al Java que
// requiere cada versión de Minecraft (RF-5), que conserva el repositorio y que
// con Java DESCONOCIDO devuelve el fallback SIN CAMBIOS.
func TestJavaImagenPorVersion(t *testing.T) {
	casos := []struct {
		mc       string
		fallback string
		want     string
	}{
		{"26.1", "itzg/minecraft-server:latest", "itzg/minecraft-server:java25"},
		{"26.3", "itzg/minecraft-server:latest", "itzg/minecraft-server:java25"},
		{"1.21.4", "itzg/minecraft-server:java25", "itzg/minecraft-server:java21"},
		{"1.20.5", "itzg/minecraft-server:java25", "itzg/minecraft-server:java21"},
		{"1.20.4", "itzg/minecraft-server:java25", "itzg/minecraft-server:java17"},
		{"1.20.1", "itzg/minecraft-server:java25", "itzg/minecraft-server:java17"},
		{"1.19", "itzg/minecraft-server:java25", "itzg/minecraft-server:java17"},
		{"1.17.1", "itzg/minecraft-server:java25", "itzg/minecraft-server:java16"},
		{"1.16.5", "itzg/minecraft-server:java25", "itzg/minecraft-server:java8"},
		{"1.12.2", "itzg/minecraft-server:java25", "itzg/minecraft-server:java8"},
		// Desconocidas: el fallback se devuelve TAL CUAL (nunca se degrada).
		{"desconocida", "itzg/minecraft-server:java25", "itzg/minecraft-server:java25"},
		{"27.1", "itzg/minecraft-server:java25", "itzg/minecraft-server:java25"},
		{"", "mi/repo:tag", "mi/repo:tag"},
		{"1.21", "", "itzg/minecraft-server:java21"},
	}
	for _, c := range casos {
		if got := JavaImageFor(c.mc, c.fallback); got != c.want {
			t.Errorf("JavaImageFor(%q, %q) = %q, quiero %q", c.mc, c.fallback, got, c.want)
		}
	}
}

// TestJavaMajorForNuncaAsumeOcho (AC-05): una versión desconocida es 0
// (desconocido), jamás Java 8.
func TestJavaMajorForNuncaAsumeOcho(t *testing.T) {
	casos := []struct {
		mc   string
		want int
	}{
		{"1.21.4", 21},
		{"1.21", 21},
		{"1.20.5", 21},
		{"1.20.4", 17},
		{"1.20.1", 17},
		{"1.19", 17},
		{"1.18", 17},
		{"1.17.1", 16},
		{"1.16.5", 8},
		{"1.16", 8},
		{"1.12.2", 8},
		{"26.1", 25},
		{"26.3", 25},
		{"26.4-snapshot-4", 25},
		// Desconocidas: 0, nunca 8.
		{"no-es-una-version", 0},
		{"", 0},
		{"27.1", 0},
		{"1.22", 0},
		{"rd-132211", 0},
		{"a1.0.5_01", 0},
	}
	for _, c := range casos {
		if got := JavaMajorFor(c.mc); got != c.want {
			t.Errorf("JavaMajorFor(%q) = %d, quiero %d", c.mc, got, c.want)
		}
	}
}

// TestJavaRequirementForExponeElOrigen: regla versionada = inferido; sin regla =
// desconocido.
func TestJavaRequirementForExponeElOrigen(t *testing.T) {
	if major, status := JavaRequirementFor("26.1"); major != 25 || status != JavaStatusInferred {
		t.Fatalf("26.1 = (%d, %q); quiero (25, inferred)", major, status)
	}
	if major, status := JavaRequirementFor("desconocida"); major != 0 || status != JavaStatusUnknown {
		t.Fatalf("desconocida = (%d, %q); quiero (0, %q)", major, status, JavaStatusUnknown)
	}
}

// TestJavaTag cubre la etiqueta java25 que publica itzg y cae al vacío con un
// Java del que no hay etiqueta.
func TestJavaTag(t *testing.T) {
	casos := map[int]string{8: "java8", 16: "java16", 17: "java17", 21: "java21", 25: "java25", 99: ""}
	for major, want := range casos {
		if got := JavaTag(major); got != want {
			t.Errorf("JavaTag(%d) = %q, quiero %q", major, got, want)
		}
	}
}

// TestNormalizeJavaRespetaElDatoDelProveedor: un Java declarado por el proveedor
// es VERIFICADO y no se pisa con la regla.
func TestNormalizeJavaRespetaElDatoDelProveedor(t *testing.T) {
	v := Version{Version: "26.3", JavaMajor: 25}
	normalizeJava(&v)
	if v.JavaStatus != JavaStatusVerified {
		t.Fatalf("estado = %q; quiero verified", v.JavaStatus)
	}
	inferred := Version{Version: "1.21.4"}
	normalizeJava(&inferred)
	if inferred.JavaMajor != 21 || inferred.JavaStatus != JavaStatusInferred {
		t.Fatalf("inferido = %+v", inferred)
	}
	unknown := Version{Version: "9.9.9"}
	normalizeJava(&unknown)
	if unknown.JavaMajor != 0 || unknown.JavaStatus != JavaStatusUnknown {
		t.Fatalf("desconocido = %+v", unknown)
	}
}
