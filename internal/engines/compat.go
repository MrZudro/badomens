package engines

import (
	"fmt"
	"strconv"
	"strings"
)

// Compatibility es el estado TRIVALENTE de compatibilidad de una versión con su
// motor (T-03). Regla transversal: un dato desconocido NUNCA se convierte en
// "compatible". El booleano `compatible` del contrato JSON se DERIVA de este
// estado (`compatible` ⇔ verificado), nunca se rellena a ciegas.
type Compatibility string

// Estados posibles de compatibilidad.
const (
	// CompatibilityCompatible: la fuente oficial del motor publica la versión y,
	// cuando el motor exige un pin, este se ha confirmado.
	CompatibilityCompatible Compatibility = "compatible"
	// CompatibilityIncompatible: se ha comprobado que el conjunto NO se puede usar.
	CompatibilityIncompatible Compatibility = "incompatible"
	// CompatibilityUnknown: no hay dato suficiente para confirmarlo.
	CompatibilityUnknown Compatibility = "unknown"
)

// Orígenes del requisito de Java (campo `java_status`).
const (
	// JavaStatusVerified: lo declara el propio proveedor (Fill v3 expone
	// `java.version.minimum`).
	JavaStatusVerified = "verified"
	// JavaStatusInferred: se deduce de una regla versionada conocida.
	JavaStatusInferred = "inferred"
	// JavaStatusUnknown: no se puede determinar (nunca se asume Java 8 — AC-05).
	JavaStatusUnknown = "unknown"
)

// NormalizeCompatibility traduce un estado de compatibilidad, incluidos los
// documentos anteriores a la Fase 3 que solo traían el booleano `compatible`.
//
// Migración: un estado vacío o no reconocido NO se convierte en compatible. La
// única excepción es el documento heredado que afirmaba `compatible: true`: la
// caché y el catálogo embebido se generaban a partir de catálogos oficiales, así
// que esa afirmación se conserva como `compatible` verificado.
func NormalizeCompatibility(c Compatibility, legacyCompatible bool) Compatibility {
	switch c {
	case CompatibilityCompatible, CompatibilityIncompatible, CompatibilityUnknown:
		return c
	}
	if legacyCompatible {
		return CompatibilityCompatible
	}
	return CompatibilityUnknown
}

// IsCompatible indica si el estado permite seleccionar la versión.
func (c Compatibility) IsCompatible() bool { return c == CompatibilityCompatible }

// String devuelve el identificador estable del estado ("" si no está fijado).
func (c Compatibility) String() string { return string(c) }

// CompatibilityInput describe el conjunto que se quiere validar: Minecraft,
// motor, loader/build, Java e imagen Docker.
type CompatibilityInput struct {
	// Engine es el motor solicitado.
	Engine Engine
	// MinecraftVersion es la versión real del juego.
	MinecraftVersion string
	// LoaderVersion es la versión exacta del loader (Fabric), si aplica.
	LoaderVersion string
	// Build es el build exacto del motor, si aplica.
	Build string
	// Pins son los pines obligatorios ya resueltos (claves de .env).
	Pins map[string]string
	// JavaMajor es el Java requerido (0 = desconocido).
	JavaMajor int
	// JavaStatus es el origen del dato de Java (verified|inferred|unknown).
	JavaStatus string
	// DockerImage es la imagen final que se va a usar (opcional).
	DockerImage string
}

// CheckCompatibility valida el conjunto Minecraft + motor + loader/build + Java +
// imagen Docker y devuelve el estado trivalente junto con el motivo de la
// incertidumbre o de la incompatibilidad ("" cuando es compatible y verificado).
//
// NUNCA devuelve compatible si falta un dato obligatorio: un estado incompleto es
// `unknown`, no `compatible`.
func CheckCompatibility(in CompatibilityInput) (Compatibility, string) {
	if _, ok := ByName(in.Engine.Name); !ok {
		return CompatibilityIncompatible, "motor desconocido"
	}
	mc := strings.TrimSpace(in.MinecraftVersion)
	if mc == "" {
		return CompatibilityUnknown, "no se identificó la versión de Minecraft"
	}
	if in.JavaMajor <= 0 || in.JavaStatus == JavaStatusUnknown {
		return CompatibilityUnknown, "requisito de Java desconocido para Minecraft " + mc
	}
	// Imagen Docker: si se conoce la que se va a usar, su Java debe SATISFACER el
	// requisito (Java N ≥ requisito). Una imagen con un Java inferior es
	// incompatible; un Java superior se acepta (el requisito es un mínimo).
	if major, ok := javaTagMajor(imageTag(in.DockerImage)); ok && major < in.JavaMajor {
		return CompatibilityIncompatible, fmt.Sprintf(
			"la imagen Docker %s usa Java %d y Minecraft %s requiere Java %d",
			in.DockerImage, major, mc, in.JavaMajor)
	}
	for _, dep := range RequiredPins(in.Engine) {
		pin := strings.TrimSpace(in.Pins[dep])
		if pin == "" || strings.EqualFold(pin, "latest") {
			return CompatibilityUnknown, "falta el pin obligatorio " + dep + " del motor " + in.Engine.Name
		}
	}
	return CompatibilityCompatible, ""
}

// javaTagMajor extrae el Java mayor de una etiqueta de imagen ("java25" → 25).
func javaTagMajor(tag string) (int, bool) {
	tag = strings.TrimSpace(tag)
	if !strings.HasPrefix(tag, "java") {
		return 0, false
	}
	major, err := strconv.Atoi(tag[len("java"):])
	if err != nil {
		return 0, false
	}
	return major, true
}

// imageTag devuelve la etiqueta de una referencia de imagen (lo que sigue al
// último ":" de fuera del repositorio; "" si no lleva etiqueta).
func imageTag(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i+1:], "/") {
		return image[i+1:]
	}
	return ""
}
