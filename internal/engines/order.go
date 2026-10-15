package engines

import (
	"regexp"
	"sort"
	"strings"
)

// Este archivo implementa el algoritmo de la sección 4.3 de la spec: conservar
// el orden del proveedor durante el parsing, normalizar, clasificar estabilidad
// con las reglas de la fuente, validar identificadores, ORDENAR con una
// estrategia adecuada al ESQUEMA de versión y, por último, calcular la
// recomendación. Nunca se ordena alfabéticamente entre esquemas distintos.

// compareForEngine compara dos versiones del catálogo con la estrategia que
// corresponde al esquema del motor:
//
//   - vanilla: el manifiesto de Mojang publica una fecha de publicación
//     autoritativa, así que el orden oficial es por fecha (con la comparación
//     estructurada de versiones como respaldo).
//   - paper, fabric, forge y neoforge: sus identificadores ya son versiones de
//     Minecraft (o se normalizan a ellas), así que se comparan por componentes
//     numéricos. Esto resuelve "26.10 > 26.2" y "1.21.10 > 1.21.9", que la
//     comparación de cadenas invertiría.
func compareForEngine(engine string, a, b Version) int {
	if engine == "vanilla" {
		az, bz := a.ReleasedAt.IsZero(), b.ReleasedAt.IsZero()
		if !az && !bz && !a.ReleasedAt.Equal(b.ReleasedAt) {
			if a.ReleasedAt.Before(b.ReleasedAt) {
				return -1
			}
			return 1
		}
	}
	return compareProviderVersions(a.Version, b.Version)
}

// sortVersions ordena (ascendente: lo más antiguo primero, la última fila es la
// más reciente) con la estrategia del esquema del motor. Es un orden ESTABLE:
// ante versiones equivalentes se conserva el orden de publicación.
func sortVersions(engine string, list []Version) {
	sort.SliceStable(list, func(i, j int) bool {
		return compareForEngine(engine, list[i], list[j]) < 0
	})
}

// markRecommended marca como recomendada la ÚLTIMA versión estable de canal
// release tras haber normalizado y ordenado. Se recalcula desde cero en cada
// pasada para que una normalización posterior no deje marcas obsoletas.
func markRecommended(list []Version) {
	for i := range list {
		list[i].Recommended = false
	}
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Channel == ChannelRelease && list[i].Stable {
			list[i].Recommended = true
			return
		}
	}
}

// normalizeChannel rellena el canal cuando el proveedor no lo declara: release
// si la fuente la marca estable y snapshot en caso contrario.
func normalizeChannel(v *Version) {
	if v.Channel != "" {
		return
	}
	if v.Stable {
		v.Channel = ChannelRelease
		return
	}
	v.Channel = ChannelSnapshot
}

// normalizeJava fija el requisito de Java y su origen: si el proveedor ya lo
// declaró (JavaMajor > 0) el dato es VERIFICADO; si no, se infiere con la regla
// versionada o queda DESCONOCIDO. Nunca se asume Java 8.
func normalizeJava(v *Version) {
	if v.JavaMajor > 0 {
		if v.JavaStatus == "" {
			v.JavaStatus = JavaStatusVerified
		}
		return
	}
	v.JavaMajor, v.JavaStatus = JavaRequirementFor(v.ID())
}

// forgeBuildPattern reconoce el pin "<mc>-<build>" del catálogo Maven de Forge
// ("1.21.1-52.0.5", "26.3-66.0.11"). La parte de Minecraft debe tener forma de
// versión y el build ser numérico: así no se confunde con sufijos como "-rc".
var forgeBuildPattern = regexp.MustCompile(`^(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)*)$`)

// minecraftOf extrae la versión de Minecraft y el build de un identificador
// crudo del proveedor, con la regla PROPIa de cada esquema. Devuelve ("", "")
// cuando el identificador YA es una versión de Minecraft (nada que agrupar).
func minecraftOf(engine, version string) (mc, build string) {
	version = strings.TrimSpace(version)
	switch engine {
	case "forge":
		if m := forgeBuildPattern.FindStringSubmatch(version); m != nil {
			return m[1], m[2]
		}
	case "neoforge":
		if scheme, mc := neoforgeAssociate(version); scheme != neoforgeSchemeUnknown && mc != "" && mc != version {
			return mc, version
		}
	}
	return "", ""
}

// betterMavenCandidate indica si `cand` es mejor pin que `prev` para la MISMA
// versión de Minecraft: primero la estabilidad y, a igualdad, el build más
// reciente comparado como versión ESTRUCTURADA (nunca alfabéticamente).
func betterMavenCandidate(cand, prev Version) bool {
	if cand.Stable != prev.Stable {
		return cand.Stable
	}
	if c := compareProviderVersions(cand.Build, prev.Build); c != 0 {
		return c > 0
	}
	return compareProviderVersions(cand.Version, prev.Version) > 0
}

// normalizeMaven agrupa los pares Minecraft↔build en UNA entrada por versión de
// Minecraft y deja el build más reciente y estable como pin. Es idempotente: un
// catálogo que ya viene normalizado (una entrada por versión de Minecraft) sale
// intacto, así que aplicarlo dos veces no cambia el resultado.
func normalizeMaven(engine string, list []Version) []Version {
	byMC := make(map[string]Version, len(list))
	order := make([]string, 0, len(list))
	for _, v := range list {
		raw := strings.TrimSpace(v.Version)
		mc, build := minecraftOf(engine, raw)
		if mc == "" {
			// Ya es (o no se puede descomponer): se conserva tal cual.
			mc, build = raw, v.Build
		}
		if mc == "" {
			continue
		}
		cand := v
		cand.Version = mc
		if build != "" {
			cand.Build = build
			if cand.Detail == "" {
				cand.Detail = raw
			}
		}
		prev, ok := byMC[mc]
		if !ok {
			order = append(order, mc)
			byMC[mc] = cand
			continue
		}
		if betterMavenCandidate(cand, prev) {
			byMC[mc] = cand
		}
	}
	out := make([]Version, 0, len(order))
	for _, mc := range order {
		out = append(out, byMC[mc])
	}
	return out
}
