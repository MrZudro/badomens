package engines

import (
	_ "embed"
	"encoding/json"
	"sort"
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
	// Se enriquece una vez para que traiga canal, Java y recomendada.
	for name, list := range embeddedCatalog {
		enriched := Enrich(name, list)
		sortByDate(enriched)
		embeddedCatalog[name] = enriched
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

// sortByDate ordena por fecha de publicación; si falta, cae al ID como último
// recurso (nunca es la fuente principal de orden).
func sortByDate(list []Version) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].ReleasedAt.IsZero() || list[j].ReleasedAt.IsZero() {
			return list[i].Version < list[j].Version
		}
		return list[i].ReleasedAt.Before(list[j].ReleasedAt)
	})
}
