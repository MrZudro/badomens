package engines

import (
	"strconv"
	"strings"
)

// Reglas VERSIONADAS del requisito de Java de Minecraft. Son una inferencia
// declarada (nunca una verdad absoluta): el origen del dato viaja en
// `java_status` y el proveedor que declara el requisito (Paper Fill v3 expone
// `java.version.minimum`) tiene prioridad sobre ellas.
//
//	26.1+        → Java 25  (el versionado por año/entrega exige Java 25)
//	1.20.5–1.21.x→ Java 21
//	1.18–1.20.4  → Java 17
//	1.17         → Java 16
//	≤ 1.16.x     → Java 8
//
// Una versión que no encaja en ninguna regla es DESCONOCIDA: `JavaMajorFor`
// devuelve 0 y jamás Java 8 (AC-05).

// mcNumericSegments trocea una versión de Minecraft en sus componentes
// numéricos iniciales ("1.20.5" → [1 20 5], "26.4-snapshot-4" → [26 4]). Si la
// versión no empieza por dígitos (p. ej. "a1.0.5_01", "rd-132211") devuelve nil.
func mcNumericSegments(mc string) []int {
	mc = strings.ToLower(strings.TrimSpace(mc))
	if mc == "" || !isDigitByte(mc[0]) {
		return nil
	}
	segs := make([]int, 0, 3)
	i := 0
	for i < len(mc) {
		j := i
		for j < len(mc) && isDigitByte(mc[j]) {
			j++
		}
		n, err := strconv.Atoi(mc[i:j])
		if err != nil {
			return nil
		}
		segs = append(segs, n)
		if j >= len(mc) {
			break
		}
		// Solo se continúa con otro componente si viene tras un punto y empieza
		// por dígito; un sufijo ("-pre1", "-rc2", "-snapshot-4") cierra la lista.
		if mc[j] != '.' || j+1 >= len(mc) || !isDigitByte(mc[j+1]) {
			break
		}
		i = j + 1
	}
	return segs
}

// javaMajorRule aplica las reglas versionadas y devuelve (0, false) cuando la
// versión no se puede clasificar.
func javaMajorRule(mc string) (int, bool) {
	segs := mcNumericSegments(mc)
	if len(segs) == 0 {
		return 0, false
	}
	major := segs[0]
	switch {
	case major == 26:
		// Versionado por año/entrega: 26.1+ exige Java 25. Otras entregas del
		// mismo año se resuelven igual porque el salto de Java es del ciclo.
		if len(segs) >= 2 && segs[1] >= 1 {
			return 25, true
		}
		return 0, false
	case major != 1 || len(segs) < 2:
		// Series futuras (27.x…), intermedias o no reconocidas: DESCONOCIDO.
		return 0, false
	}
	minor := segs[1]
	patch := 0
	if len(segs) >= 3 {
		patch = segs[2]
	}
	switch {
	case minor <= 16:
		return 8, true
	case minor == 17:
		return 16, true
	case minor == 18 || minor == 19:
		return 17, true
	case minor == 20:
		if patch >= 5 {
			return 21, true
		}
		return 17, true
	case minor == 21:
		return 21, true
	}
	return 0, false
}

// JavaMajorFor devuelve el Java que necesita una versión de Minecraft según las
// reglas versionadas, o 0 cuando es DESCONOCIDO. Numca inventa Java 8 (AC-05).
func JavaMajorFor(mc string) int {
	if major, ok := javaMajorRule(mc); ok {
		return major
	}
	return 0
}

// JavaRequirementFor devuelve el requisito de Java junto con su ORIGEN
// (JavaStatusInferred o JavaStatusUnknown). Es la puerta que usa la
// normalización del catálogo.
func JavaRequirementFor(mc string) (int, string) {
	if major, ok := javaMajorRule(mc); ok {
		return major, JavaStatusInferred
	}
	return 0, JavaStatusUnknown
}

// JavaTag devuelve la etiqueta de imagen de itzg para un Java mayor ("java17",
// "java25"), o "" si no hay una etiqueta conocida.
func JavaTag(major int) string {
	switch major {
	case 8, 11, 16, 17, 21, 22, 25:
		return "java" + strconv.Itoa(major)
	}
	return ""
}

// JavaImageFor adapta la imagen base al Java que requiere una versión de
// Minecraft (RF-5): conserva el repositorio de `fallback` y elige la etiqueta
// `javaN`. Si el Java es DESCONOCIDO, devuelve `fallback` SIN CAMBIOS: nunca se
// degrada la imagen por una suposición.
func JavaImageFor(mc, fallback string) string {
	major, ok := javaMajorRule(mc)
	if !ok {
		return fallback
	}
	tag := JavaTag(major)
	if tag == "" {
		return fallback
	}
	repo := "itzg/minecraft-server"
	if fallback != "" {
		repo = fallback
		if i := strings.Index(repo, ":"); i >= 0 {
			repo = repo[:i]
		}
	}
	return repo + ":" + tag
}
