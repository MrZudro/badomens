package engines

import (
	_ "embed"
	"encoding/json"
)

// embeddedRaw es el catálogo de respaldo generado en build (`make gen-versions`).
//
//go:embed embedded.json
var embeddedRaw []byte

// embeddedCatalog es el catálogo de respaldo ya decodificado por motor.
var embeddedCatalog map[string][]Version

func init() {
	embeddedCatalog = map[string][]Version{}
	if err := json.Unmarshal(embeddedRaw, &embeddedCatalog); err != nil {
		// Un catálogo embebido corrupto no debe tumbar el binario: se ignora y se
		// recurre a la red (o a un error claro de proveedor).
		embeddedCatalog = map[string][]Version{}
	}
	// Se normaliza una sola vez: canal, Java (con su origen), compatibilidad,
	// orden por esquema del motor y marca de recomendada.
	//
	// Un documento anterior a la Fase 3 solo traía `compatible: true`; la
	// migración de Enrich lo conserva como compatibilidad VERIFICADA porque el
	// catálogo embebido se generó a partir de los catálogos oficiales.
	for name, list := range embeddedCatalog {
		embeddedCatalog[name] = Enrich(name, list)
	}
}

// EmbeddedVersions devuelve el catálogo de respaldo de un motor (sin red). El
// segundo valor indica si existe catálogo embebido para ese motor.
func EmbeddedVersions(engine string) ([]Version, bool) {
	list, ok := embeddedCatalog[engine]
	if !ok || len(list) == 0 {
		return nil, false
	}
	out := make([]Version, len(list))
	copy(out, list)
	return out, true
}
