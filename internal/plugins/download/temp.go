package download

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

// tempPrefix y tempSuffix identifican los temporales que crea el descargador.
const (
	tempPrefix = ".badomens-download-"
	tempSuffix = ".tmp"
)

// IsTempFile informa si el nombre corresponde a un archivo temporal de
// badomens. El servidor nunca lo cargará (no termina en .jar) y no debe
// aparecer en los listados.
func IsTempFile(name string) bool {
	return strings.HasPrefix(name, tempPrefix) && strings.HasSuffix(name, tempSuffix)
}

// CleanStaleTemps borra los temporales huérfanos de una descarga interrumpida
// (proceso terminado a la fuerza) que sean más antiguos que olderThan. Nunca
// toca archivos ajenos ni temporales recientes, para no interferir con una
// descarga en curso. Es best-effort: devuelve cuántos archivos borró.
func CleanStaleTemps(dir string, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		olderThan = time.Hour
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, errx.Wrap(errx.CodeGeneral, err, "no se pudo revisar la carpeta "+dir, "revisa permisos")
	}
	limit := time.Now().Add(-olderThan)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !IsTempFile(e.Name()) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || info.ModTime().After(limit) {
			continue
		}
		if rerr := os.Remove(filepath.Join(dir, e.Name())); rerr == nil {
			removed++
		}
	}
	return removed, nil
}
