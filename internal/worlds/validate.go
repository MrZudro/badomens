package worlds

import (
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
)

// ValidateName comprueba que un nombre de mundo es un IDENTIFICADOR seguro y no
// una ruta (plan 12.4 · spec 6.3): rechaza vacíos, separadores, "..", caracteres
// de control y nombres que empiezan por punto. Todo nombre que llegue del CLI
// pasa por aquí antes de componer una ruta dentro del data dir.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errx.Usage("nombre de mundo vacío", "indica un nombre, por ejemplo 'arena'")
	}
	if name != strings.TrimSpace(name) {
		return errx.Usage("nombre de mundo inválido", "sin espacios al principio ni al final")
	}
	if len(name) > 100 {
		return errx.Usage("nombre de mundo demasiado largo", "usa como máximo 100 caracteres")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return errx.Usage("nombre de mundo inválido", "sin separadores ni '..'; no es una ruta")
	}
	if strings.HasPrefix(name, ".") {
		return errx.Usage("nombre de mundo inválido", "no puede empezar por punto")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errx.Usage("nombre de mundo inválido", "contiene caracteres de control")
		}
	}
	return nil
}
