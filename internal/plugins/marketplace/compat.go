package marketplace

import (
	"strconv"
	"strings"
)

// Evaluate calcula la compatibilidad DECLARADA de un artefacto a partir de los
// loaders y las versiones de Minecraft que el proveedor publica para él.
//
// Es deliberadamente conservador:
//
//   - Si no se conoce el entorno (motor/loader o versión de Minecraft), el
//     veredicto es Unknown: no hay nada que comparar.
//   - Si el proveedor no declara loaders (o no declara versiones), el veredicto
//     es Unknown aunque el resultado venga de una búsqueda filtrada por loader:
//     el filtro lo aplica el servidor del marketplace y no es una prueba.
//   - Incompatible solo cuando el proveedor declara datos y NO coinciden.
//
// La comparación de loader es por nombre exacto, sin equivalencias implícitas:
// un artefacto que declara spigot no se marca compatible con paper, porque esa
// equivalencia no la publica el proveedor. La comparación no distingue
// mayúsculas porque Hangar las usa en los nombres de plataforma.
func Evaluate(loaders, gameVersions []string, env Environment) (Compatibility, string) {
	loader := env.LoaderName()
	if loader == "" {
		return Unknown, "no se indicó el motor ni el loader del servidor"
	}
	if len(loaders) == 0 {
		return Unknown, "el proveedor no declara loaders para este artefacto"
	}
	if !containsFold(loaders, loader) {
		return Incompatible, "el artefacto declara los loaders " + listFor(loaders) + " y no incluye " + loader
	}
	if strings.TrimSpace(env.GameVersion) == "" {
		return Unknown, "no se indicó la versión de Minecraft del servidor"
	}
	if len(gameVersions) == 0 {
		return Unknown, "el proveedor no declara versiones de Minecraft para este artefacto"
	}
	if !containsFold(gameVersions, env.GameVersion) {
		return Incompatible, "el artefacto declara Minecraft " + listFor(gameVersions) + " y no incluye " + env.GameVersion
	}
	return Compatible, "el proveedor declara " + loader + " y Minecraft " + env.GameVersion
}

// Compatibility calcula la compatibilidad declarada del proyecto entero.
func (p Project) Compatibility(env Environment) (Compatibility, string) {
	return Evaluate(p.Loaders, p.GameVersions, env)
}

// Compatibility calcula la compatibilidad declarada de una versión concreta.
func (v Version) Compatibility(env Environment) (Compatibility, string) {
	return Evaluate(v.Loaders, v.GameVersions, env)
}

// PublishedLabel describe el estado de publicación en una etiqueta legible. No
// afirma nada que el proveedor no diga: sin estado, el resultado es
// "desconocido".
func PublishedLabel(published bool, status string) string {
	status = strings.TrimSpace(status)
	switch {
	case status == "" && published:
		return "publicado"
	case status == "":
		return "desconocido"
	case published:
		return "publicado (" + status + ")"
	default:
		return "no publicado (" + status + ")"
	}
}

// containsFold compara sin distinguir mayúsculas ni espacios sobrantes.
func containsFold(list []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, v := range list {
		if strings.ToLower(strings.TrimSpace(v)) == want {
			return true
		}
	}
	return false
}

// listFor arma una lista legible y acotada para los mensajes de motivo.
func listFor(list []string) string {
	const max = 8
	shown := list
	suffix := ""
	if len(shown) > max {
		shown = shown[:max]
		suffix = " (+" + strconv.Itoa(len(list)-max) + " más)"
	}
	quoted := make([]string, 0, len(shown))
	for _, v := range shown {
		quoted = append(quoted, strings.TrimSpace(v))
	}
	return strings.Join(quoted, ", ") + suffix
}
