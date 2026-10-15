package engines

import (
	"fmt"
	"strings"
)

// pickVersion elige, para un motor, la versión pedida o la última ESTABLE más
// reciente del catálogo. Sustituye a la versión histórica que asumía "el último
// elemento de la lista es el más reciente" (API-05): el resultado es el mismo
// con independencia del orden en que el proveedor publique sus versiones.
//
//   - Con versión explícita: debe existir EXACTAMENTE y ser seleccionable. Una
//     versión inexistente o marcada como incompatible es un error (nunca una
//     resolución parcial).
//   - Sin versión: se elige el máximo por la comparación estructurada del
//     esquema del motor entre las versiones estables; si ninguna es estable, el
//     máximo de todo el catálogo.
func pickVersion(engine string, list []Version, version string) (Version, error) {
	if len(list) == 0 {
		return Version{}, fmt.Errorf("el catálogo no devolvió versiones")
	}
	version = strings.TrimSpace(version)
	if version != "" {
		for _, v := range list {
			if v.Version != version {
				continue
			}
			if v.Compatibility == CompatibilityIncompatible {
				why := strings.TrimSpace(v.IncompatibleWhy)
				if why == "" {
					why = "incompatible con el motor " + engine
				}
				return Version{}, fmt.Errorf("la versión %q no se puede usar: %s", version, why)
			}
			return v, nil
		}
		return Version{}, fmt.Errorf("la versión %q no existe para este motor", version)
	}
	best, found := Version{}, false
	for _, v := range list {
		if !v.Stable || v.Compatibility == CompatibilityIncompatible {
			continue
		}
		if !found || compareForEngine(engine, v, best) > 0 {
			best, found = v, true
		}
	}
	if !found {
		for _, v := range list {
			if v.Compatibility == CompatibilityIncompatible {
				continue
			}
			if !found || compareForEngine(engine, v, best) > 0 {
				best, found = v, true
			}
		}
	}
	if !found {
		return Version{}, fmt.Errorf("no hay ninguna versión utilizable en el catálogo")
	}
	return best, nil
}
