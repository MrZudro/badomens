// Package setup agrupa la lógica del asistente de creación de instancias:
// recomendaciones y utilidades de lectura de prompt. El asistente interactivo
// vive en la capa CLI.
package setup

// RecommendMaxPlayers sugiere el máximo de jugadores según la RAM de la
// instancia (y si el motor es modded). La tabla es deliberadamente prudente.
func RecommendMaxPlayers(memoryGB float64, modded bool) int {
	if modded {
		memoryGB = memoryGB / 2
	}
	switch {
	case memoryGB >= 8:
		return 40
	case memoryGB >= 6:
		return 25
	case memoryGB >= 4:
		return 15
	case memoryGB >= 2:
		return 8
	case memoryGB >= 1:
		return 4
	default:
		return 2
	}
}

// ParseCommaList convierte "a, b, c" en una lista limpia (sin vacíos).
func ParseCommaList(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		switch r {
		case ',', '\n':
			if cur = trimSpace(cur); cur != "" {
				out = append(out, cur)
			}
			cur = ""
		default:
			cur += string(r)
		}
	}
	if cur = trimSpace(cur); cur != "" {
		out = append(out, cur)
	}
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}
