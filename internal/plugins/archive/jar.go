// Package archive valida la ESTRUCTURA de los JAR/ZIP que se van a instalar
// como plugins o mods, con límites de recursos.
//
// Nunca descomprime ni extrae nada: solo lee la firma y el directorio central
// del archivo. Un "zip bomb" declarado se detecta por sus cabeceras y se
// rechaza sin materializar un solo byte descomprimido, y jamás se escribe en el
// directorio de ejecución.
//
// Lo que NO garantiza: que el contenido sea seguro, esté libre de código
// malicioso o sea compatible con el motor. Solo comprueba forma y tamaño.
package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
)

// Límites por defecto al inspeccionar un JAR/ZIP.
const (
	// DefaultMaxEntries es el número máximo de entradas del archivo.
	DefaultMaxEntries = 4096
	// DefaultMaxUncompressedBytes es la suma máxima de tamaños declarados.
	DefaultMaxUncompressedBytes int64 = 512 << 20
	// DefaultMaxEntryBytes es el tamaño declarado máximo de una entrada.
	DefaultMaxEntryBytes int64 = 128 << 20
	// DefaultMaxRatio es la ratio descomprimido/comprimido máxima tolerada en
	// entradas de 1 MiB o más (compresión "sospechosa").
	DefaultMaxRatio = 200.0
	// DefaultMaxNameLen es la longitud máxima del nombre de una entrada.
	DefaultMaxNameLen = 512
	// ratioFloorBytes es el tamaño a partir del cual se evalúa la ratio: en
	// archivos diminutos la compresión normal da ratios altos y sería un falso
	// positivo.
	ratioFloorBytes int64 = 1 << 20
)

// Limits son los límites de recursos aplicables a la inspección.
type Limits struct {
	// MaxEntries 0 â‡’ DefaultMaxEntries.
	MaxEntries int
	// MaxUncompressedBytes 0 â‡’ DefaultMaxUncompressedBytes.
	MaxUncompressedBytes int64
	// MaxEntryBytes 0 â‡’ DefaultMaxEntryBytes.
	MaxEntryBytes int64
	// MaxRatio 0 ⇒ DefaultMaxRatio (en entradas de 1 MiB o más).
	MaxRatio float64
	// MaxNameLen 0 â‡’ DefaultMaxNameLen.
	MaxNameLen int
}

// DefaultLimits devuelve los límites por defecto.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:           DefaultMaxEntries,
		MaxUncompressedBytes: DefaultMaxUncompressedBytes,
		MaxEntryBytes:        DefaultMaxEntryBytes,
		MaxRatio:             DefaultMaxRatio,
		MaxNameLen:           DefaultMaxNameLen,
	}
}

// WithDefaults completa los límites no indicados.
func (l Limits) WithDefaults() Limits {
	d := DefaultLimits()
	if l.MaxEntries <= 0 {
		l.MaxEntries = d.MaxEntries
	}
	if l.MaxUncompressedBytes <= 0 {
		l.MaxUncompressedBytes = d.MaxUncompressedBytes
	}
	if l.MaxEntryBytes <= 0 {
		l.MaxEntryBytes = d.MaxEntryBytes
	}
	if l.MaxRatio <= 0 {
		l.MaxRatio = d.MaxRatio
	}
	if l.MaxNameLen <= 0 {
		l.MaxNameLen = d.MaxNameLen
	}
	return l
}

// Report resume lo observado en el artefacto. Es informativo y aditivo: se
// incluye en el documento JSON de `plugin add`.
type Report struct {
	// Entries es el número de entradas del archivo.
	Entries int `json:"entries"`
	// UncompressedBytes es la suma de tamaños declarados (no verificados byte a
	// byte: no se descomprime).
	UncompressedBytes int64 `json:"uncompressed_bytes"`
	// CompressedBytes es la suma de tamaños comprimidos declarados.
	CompressedBytes int64 `json:"compressed_bytes"`
	// MaxRatio es la mayor ratio descomprimido/comprimido observada.
	MaxRatio float64 `json:"max_ratio"`
	// HasManifest indica si contiene META-INF/MANIFEST.MF.
	HasManifest bool `json:"has_manifest"`
	// Descriptor nombra el descriptor de plugin/mod encontrado (vacío si no hay).
	Descriptor string `json:"descriptor,omitempty"`
}

// descriptors son los ficheros que anuncian un plugin/mod según el cargador.
var descriptors = map[string]bool{
	"plugin.yml":           true,
	"paper-plugin.yml":     true,
	"bungee.yml":           true,
	"velocity-plugin.json": true,
	"fabric.mod.json":      true,
	"quilt.mod.json":       true,
	"mods.toml":            true,
	"mcmod.info":           true,
	"neoforge.mods.toml":   true,
}

// Validate comprueba que el archivo es un JAR/ZIP estructuralmente válido y
// respeta los límites. `size` es el tamaño del archivo en disco (ya acotado por
// el descargador). No extrae nada.
func Validate(file string, size int64, lim Limits) (Report, error) {
	l := lim.WithDefaults()
	if size <= 0 {
		return Report{}, invalid("el archivo está vacío", nil)
	}
	f, err := os.Open(file) // #nosec G304 -- ruta del temporal que controla badomens
	if err != nil {
		return Report{}, errx.Wrap(errx.CodeGeneral, err, "no se pudo leer el artefacto para validarlo", "revisa permisos")
	}
	defer func() { _ = f.Close() }()

	var sig [4]byte
	if _, err := io.ReadFull(f, sig[:]); err != nil {
		return Report{}, invalid("el archivo no parece un JAR/ZIP (no se pudo leer su firma)", nil)
	}
	if sig[0] != 'P' || sig[1] != 'K' {
		return Report{}, invalid("el archivo no es un JAR/ZIP: falta la firma 'PK'", nil)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Report{}, errx.Wrap(errx.CodeGeneral, err, "no se pudo volver a leer el artefacto", "")
	}

	zr, err := zip.NewReader(f, size)
	if err != nil {
		if errors.Is(err, zip.ErrFormat) || strings.Contains(err.Error(), "not a valid zip") {
			return Report{}, invalid("el JAR/ZIP está corrupto o truncado", nil)
		}
		return Report{}, invalid("el JAR/ZIP está corrupto o truncado: "+err.Error(), nil)
	}
	if len(zr.File) == 0 {
		return Report{}, invalid("el JAR/ZIP no contiene ninguna entrada", nil)
	}
	if len(zr.File) > l.MaxEntries {
		return Report{}, excess(fmt.Sprintf("el JAR/ZIP tiene %d entradas y el límite es %d", len(zr.File), l.MaxEntries),
			map[string]any{"entries": len(zr.File), "max_entries": l.MaxEntries})
	}

	rep := Report{Entries: len(zr.File)}
	for _, e := range zr.File {
		if e.Name == "" || len(e.Name) > l.MaxNameLen {
			return Report{}, invalid("el JAR/ZIP contiene una entrada con nombre vacío o demasiado largo", nil)
		}
		clean := path.Clean(strings.ReplaceAll(e.Name, `\`, "/"))
		if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
			return Report{}, invalid("el JAR/ZIP contiene una entrada con ruta insegura: "+e.Name, nil)
		}
		if e.Flags&0x1 != 0 {
			return Report{}, invalid("el JAR/ZIP contiene una entrada cifrada: "+e.Name, nil)
		}
		if strings.EqualFold(clean, "META-INF/MANIFEST.MF") {
			rep.HasManifest = true
		}
		base := path.Base(clean)
		if descriptors[strings.ToLower(base)] && rep.Descriptor == "" {
			rep.Descriptor = base
		}
		// El límite por entrada se comprueba ANTES de convertir a int64.
		if e.UncompressedSize64 > uint64(l.MaxEntryBytes) {
			return Report{}, excess(fmt.Sprintf("la entrada %s declara %d bytes y el límite por entrada es %d", e.Name, e.UncompressedSize64, l.MaxEntryBytes),
				map[string]any{"entry": e.Name, "entry_bytes": e.UncompressedSize64, "max_entry_bytes": l.MaxEntryBytes})
		}
		if e.CompressedSize64 > 0 && e.UncompressedSize64 >= uint64(ratioFloorBytes) {
			ratio := float64(e.UncompressedSize64) / float64(e.CompressedSize64)
			if ratio > rep.MaxRatio {
				rep.MaxRatio = ratio
			}
			if ratio > l.MaxRatio {
				return Report{}, excess(fmt.Sprintf("la entrada %s declara una compresión sospechosa (%.0f:1)", e.Name, ratio),
					map[string]any{"entry": e.Name, "ratio": ratio, "max_ratio": l.MaxRatio})
			}
		}
		rep.UncompressedBytes += int64(e.UncompressedSize64)
		rep.CompressedBytes += int64(e.CompressedSize64)
		if rep.UncompressedBytes > l.MaxUncompressedBytes {
			return Report{}, excess(fmt.Sprintf("el JAR/ZIP declara %d bytes descomprimidos y el límite es %d", rep.UncompressedBytes, l.MaxUncompressedBytes),
				map[string]any{"uncompressed_bytes": rep.UncompressedBytes, "max_uncompressed_bytes": l.MaxUncompressedBytes})
		}
	}
	return rep, nil
}

// invalid construye el error de estructura inválida.
func invalid(message string, extra map[string]any) error {
	e := errx.New(errx.CodeUsage, message, "instala solo JAR/ZIP de plugins o mods; revisa el origen").
		WithDetail("reason", "artefacto_invalido")
	for k, v := range extra {
		e = e.WithDetail(k, v)
	}
	return e
}

// excess construye el error de límite de recursos superado.
func excess(message string, extra map[string]any) error {
	e := errx.New(errx.CodeCapacity, message, "no se instala un artefacto que excede los límites de recursos").
		WithDetail("reason", "limites_recursos")
	for k, v := range extra {
		e = e.WithDetail(k, v)
	}
	return e
}
