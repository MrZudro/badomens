package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/errx"
)

// entry es una entrada de prueba del ZIP.
type entry struct {
	name string
	data []byte
}

// writeZip escribe un ZIP con las entradas dadas y devuelve su ruta y tamaño.
func writeZip(t *testing.T, entries []entry) (string, int64) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("create %s: %v", e.name, err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatalf("write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return writeRaw(t, buf.Bytes())
}

// writeRaw escribe bytes en un archivo temporal y devuelve ruta y tamaño.
func writeRaw(t *testing.T, raw []byte) (string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artefacto.jar")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path, int64(len(raw))
}

// jarMinimo es la estructura interna de un plugin típico.
func jarMinimo() []entry {
	return []entry{
		{name: "META-INF/MANIFEST.MF", data: []byte("Manifest-Version: 1.0\n")},
		{name: "plugin.yml", data: []byte("name: Demo\nversion: 1.0\n")},
	}
}

// TestValidateAceptaJarValido comprueba el camino feliz y el informe.
func TestValidateAceptaJarValido(t *testing.T) {
	path, size := writeZip(t, jarMinimo())
	rep, err := Validate(path, size, Limits{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.Entries != 2 {
		t.Errorf("Entries = %d; se esperaban 2", rep.Entries)
	}
	if !rep.HasManifest {
		t.Error("HasManifest = false; se esperaba true")
	}
	if rep.Descriptor != "plugin.yml" {
		t.Errorf("Descriptor = %q; se esperaba plugin.yml", rep.Descriptor)
	}
	if rep.UncompressedBytes <= 0 || rep.CompressedBytes <= 0 {
		t.Errorf("tamaños declarados inesperados: %+v", rep)
	}
}

// TestValidateRechazaArtefactosInvalidos: no es un zip, está truncado o vacío.
func TestValidateRechazaArtefactosInvalidos(t *testing.T) {
	// No es un ZIP (sin firma PK).
	path, size := writeRaw(t, bytes.Repeat([]byte("no soy un zip"), 64))
	if _, err := Validate(path, size, Limits{}); err == nil {
		t.Error("se esperaba rechazo de un archivo sin firma PK")
	} else if errx.Details(err)["reason"] != "artefacto_invalido" {
		t.Errorf("reason = %v", errx.Details(err)["reason"])
	}

	// ZIP truncado: se corta el directorio central.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("plugin.yml")
	_, _ = w.Write([]byte("name: Demo\n"))
	_ = zw.Close()
	full := buf.Bytes()
	truncated := full[:len(full)-30]
	path2, size2 := writeRaw(t, truncated)
	if _, err := Validate(path2, size2, Limits{}); err == nil {
		t.Error("se esperaba rechazo de un JAR truncado")
	}

	// Firma PK pero contenido basura: no es un zip válido.
	path3, size3 := writeRaw(t, append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0x41}, 64)...))
	if _, err := Validate(path3, size3, Limits{}); err == nil {
		t.Error("se esperaba rechazo de un zip ilegible")
	}

	// Vacío.
	path4, _ := writeRaw(t, nil)
	if _, err := Validate(path4, 0, Limits{}); err == nil {
		t.Error("se esperaba rechazo de un archivo vacío")
	}
}

// TestValidateLimitesDeRecursos: número de entradas, tamaño por entrada, suma
// declarada y compresión sospechosa.
func TestValidateLimitesDeRecursos(t *testing.T) {
	muchas := make([]entry, 0, 8)
	for i := 0; i < 8; i++ {
		muchas = append(muchas, entry{name: filepath.Join("paquete", "f"+string(rune('a'+i))+".class"), data: []byte("x")})
	}
	path, size := writeZip(t, muchas)
	if _, err := Validate(path, size, Limits{MaxEntries: 3}); err == nil {
		t.Error("se esperaba rechazo por número de entradas")
	} else if !errx.Is(err, errx.CodeCapacity) || errx.Details(err)["reason"] != "limites_recursos" {
		t.Errorf("error inesperado: %v", err)
	}

	// Tamaño por entrada.
	path2, size2 := writeZip(t, []entry{{name: "grande.bin", data: bytes.Repeat([]byte("A"), 4096)}})
	if _, err := Validate(path2, size2, Limits{MaxEntryBytes: 128}); err == nil {
		t.Error("se esperaba rechazo por tamaño de entrada")
	}

	// Suma declarada.
	if _, err := Validate(path2, size2, Limits{MaxUncompressedBytes: 64}); err == nil {
		t.Error("se esperaba rechazo por suma de bytes descomprimidos")
	}

	// Compresión sospechosa (zip bomb declarado): 4 MiB de ceros comprimidos.
	bomba, sizeBomba := writeZip(t, []entry{{name: "bomba.bin", data: make([]byte, 4<<20)}})
	if _, err := Validate(bomba, sizeBomba, Limits{}); err == nil {
		t.Error("se esperaba rechazo por compresión sospechosa con los límites por defecto")
	} else if !errx.Is(err, errx.CodeCapacity) {
		t.Errorf("error inesperado: %v", err)
	}
	if _, err := Validate(bomba, sizeBomba, Limits{MaxRatio: 1_000_000}); err != nil {
		t.Errorf("con un límite de ratio alto debería aceptarse: %v", err)
	}
}

// TestValidateRechazaRutasInseguras: entradas con traversals o rutas absolutas.
func TestValidateRechazaRutasInseguras(t *testing.T) {
	cases := [][]entry{
		{{name: "../evil.txt", data: []byte("x")}},
		{{name: "/absoluta.txt", data: []byte("x")}},
		{{name: "a/../../evil.txt", data: []byte("x")}},
	}
	for _, entries := range cases {
		path, size := writeZip(t, entries)
		if _, err := Validate(path, size, Limits{}); err == nil {
			t.Errorf("se esperaba rechazo de la entrada %q", entries[0].name)
		}
	}
}

// TestValidateRechazaNombreLargo: nombres desmesurados.
func TestValidateRechazaNombreLargo(t *testing.T) {
	path, size := writeZip(t, []entry{{name: "a/" + strings.Repeat("b", 700) + ".class", data: []byte("x")}})
	if _, err := Validate(path, size, Limits{}); err == nil {
		t.Error("se esperaba rechazo por nombre de entrada demasiado largo")
	}
}

// TestValidateRechazaEntradaCifrada: el bit de cifrado se rechaza (no se puede
// validar el contenido de una entrada cifrada).
func TestValidateRechazaEntradaCifrada(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("plugin.yml")
	_, _ = w.Write([]byte("name: Demo\n"))
	_ = zw.Close()
	path, size := writeRaw(t, markEncrypted(buf.Bytes()))
	if _, err := Validate(path, size, Limits{}); err == nil {
		t.Error("se esperaba rechazo de una entrada cifrada")
	}
}

// markEncrypted activa el bit 0 (cifrado) en las cabeceras del ZIP: offset 6 en
// la cabecera local y offset 8 en la entrada del directorio central.
func markEncrypted(raw []byte) []byte {
	out := append([]byte(nil), raw...)
	for i := 0; i+10 < len(out); i++ {
		if out[i] != 'P' || out[i+1] != 'K' {
			continue
		}
		switch {
		case out[i+2] == 3 && out[i+3] == 4:
			out[i+6] |= 1
		case out[i+2] == 1 && out[i+3] == 2:
			out[i+8] |= 1
		}
	}
	return out
}
