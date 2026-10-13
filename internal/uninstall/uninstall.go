// Package uninstall retira badomens del sistema: el binario, la configuración
// global y, opcionalmente (--purge), los datos (instancias, mundos, backups).
// Por defecto los datos se conservan: el data dir vive bajo el dir de
// configuración y borrarlo es destructivo.
package uninstall

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/MrZudro/badomens/internal/config"
)

// Paths localizan los elementos que se borran o conservan.
type Paths struct {
	ConfigDir  string
	ConfigFile string
	DataDir    string
}

// DefaultPaths resuelve las rutas reales a partir de la configuración.
func DefaultPaths(cfg *config.Config) (Paths, error) {
	dir, err := config.Dir()
	if err != nil {
		return Paths{}, err
	}
	file, err := config.Path()
	if err != nil {
		return Paths{}, err
	}
	return Paths{ConfigDir: dir, ConfigFile: file, DataDir: cfg.DataDir}, nil
}

// Result resume lo que se quitó y lo que se conservó.
type Result struct {
	Removed  []string `json:"removed"`
	Kept     []string `json:"kept"`
	Warnings []string `json:"warnings,omitempty"`
}

// Options controla la desinstalación.
type Options struct {
	// Purge borra también el data dir (instancias, mundos, backups).
	Purge bool
}

// Run ejecuta la desinstalación. exe es la ruta del binario en ejecución
// (os.Executable()); stopAll (opcional) detiene las instancias en marcha para
// no dejar contenedores huérfanos.
func Run(ctx context.Context, p Paths, exe string, stopAll func(ctx context.Context) error, opt Options) (*Result, error) {
	res := &Result{}

	// 1) Detiene las instancias (best-effort).
	if stopAll != nil {
		if err := stopAll(ctx); err != nil {
			res.Warnings = append(res.Warnings, "no se pudieron detener las instancias: "+err.Error())
		} else {
			res.Removed = append(res.Removed, "instancias detenidas")
		}
	}

	// 2) Configuración global y datos.
	if opt.Purge {
		if p.DataDir != "" {
			if err := os.RemoveAll(p.DataDir); err != nil {
				res.Warnings = append(res.Warnings, "no se pudo borrar el data dir: "+err.Error())
			} else {
				res.Removed = append(res.Removed, "datos ("+p.DataDir+")")
			}
		}
		if p.ConfigDir != "" {
			if err := os.RemoveAll(p.ConfigDir); err != nil {
				res.Warnings = append(res.Warnings, "no se pudo borrar la carpeta de configuración: "+err.Error())
			} else {
				res.Removed = append(res.Removed, "configuración ("+p.ConfigDir+")")
			}
		}
	} else {
		if p.ConfigFile != "" {
			if err := os.Remove(p.ConfigFile); err != nil {
				res.Warnings = append(res.Warnings, "no se pudo borrar la configuración: "+err.Error())
			} else {
				res.Removed = append(res.Removed, "configuración ("+p.ConfigFile+")")
			}
		}
		if p.DataDir != "" {
			res.Kept = append(res.Kept, "datos conservados ("+p.DataDir+")")
		}
	}

	// 3) Binario. En Unix se puede borrar en ejecución; en Windows se intenta
	// renombrar a .old (se elimina al reiniciar) o se deja anotado.
	removeBinary(exe, res)

	return res, nil
}

// removeBinary intenta borrar (o renombrar en Windows) el binario en marcha.
func removeBinary(exe string, res *Result) {
	if exe == "" {
		return
	}
	switch err := os.Remove(exe); {
	case err == nil:
		res.Removed = append(res.Removed, "binario ("+exe+")")
	case runtime.GOOS == "windows":
		if rerr := os.Rename(exe, exe+".old"); rerr == nil {
			res.Removed = append(res.Removed, "binario renombrado a "+exe+".old")
			res.Warnings = append(res.Warnings, "elimina "+exe+".old después de cerrar badomens")
		} else {
			res.Warnings = append(res.Warnings, "no se pudo borrar el binario en uso ("+exe+"). Elimínalo manualmente y quita la carpeta del PATH.")
		}
	default:
		res.Warnings = append(res.Warnings, fmt.Sprintf("no se pudo borrar el binario (%s): %v", exe, err))
	}
}

// Plan describe qué haría Run (con --dry-run) sin borrar nada.
func Plan(p Paths, opt Options) []string {
	var steps []string
	steps = append(steps, "detener las instancias corriendo")
	if p.ConfigFile != "" {
		steps = append(steps, "borrar configuración: "+p.ConfigFile)
	}
	if opt.Purge {
		if p.DataDir != "" {
			steps = append(steps, "borrar datos: "+p.DataDir)
		}
		if p.ConfigDir != "" {
			steps = append(steps, "borrar carpeta de configuración: "+p.ConfigDir)
		}
	} else if p.DataDir != "" {
		steps = append(steps, "conservar datos: "+p.DataDir)
	}
	steps = append(steps, "borrar el binario (o renombrarlo en Windows)")
	return steps
}
