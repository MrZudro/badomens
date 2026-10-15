// Package plugins es el catÃ¡logo de plugins/mods instalados en una instancia:
// carpeta destino segÃºn el motor (paper â†’ plugins/, fabric|forge|neoforge â†’
// mods/, vanilla no admite plugins), alta, baja y listado.
//
// La instalaciÃ³n (transporte, verificaciÃ³n y movimiento atÃ³mico) vive en los
// mÃ³dulos internos:
//
//   - internal/plugins/download implementa la obtenciÃ³n segura del artefacto
//     (streaming con lÃ­mite, temporal + rename atÃ³mico, https obligatorio y
//     polÃ­tica anti-SSRF que valida la IP resuelta de cada conexiÃ³n);
//   - internal/plugins/archive valida la ESTRUCTURA del JAR/ZIP con lÃ­mites de
//     recursos y sin descomprimir nada.
//
// Un plugin/mod es cÃ³digo de terceros: ver ThirdPartyWarning.
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins/download"
)

// KnownHeavy son plugins/mods conocidos por su mayor coste de CPU/servidor. No
// es una lista de "prohibidos", solo de aviso.
var KnownHeavy = map[string]bool{
	"citizens": true, "mythicmobs": true, "coreprotect": true, "logblock": true,
	"dynmap": true, "squaremap": true, "bluemap": true, "worldguard": true,
	"essentialsx": true, "authme": true, "chunky": true, "economyapi": true,
}

// Installer opera sobre la carpeta de plugins/mods de una instancia.
type Installer struct{ DataDir string }

// Dir devuelve la carpeta destino de plugins/mods de la instancia.
func (i *Installer) Dir(spec *instance.Spec) string {
	dir := "mods"
	if spec.Engine == "paper" {
		dir = "plugins"
	}
	return filepath.Join(i.DataDir, "instances", spec.Slug, dir)
}

// List lista los archivos instalados.
func (i *Installer) List(spec *instance.Spec) ([]string, error) {
	if spec.Engine == "vanilla" {
		return []string{}, nil
	}
	dir := i.Dir(spec)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		// Los temporales de badomens no son plugins instalados.
		if !e.IsDir() && !download.IsTempFile(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Remove borra un plugin/mods por nombre.
func (i *Installer) Remove(spec *instance.Spec, name string) error {
	dir := i.Dir(spec)
	if strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return errx.Usage("nombre invÃ¡lido", "usa 'plugin list'")
	}
	path := filepath.Join(dir, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return errx.NotFound("el plugin %q no estÃ¡ instalado", "usa 'plugin list'")
		}
		return err
	}
	return nil
}

// WarnFor devuelve un texto de aviso de rendimiento si aplica, o "".
func WarnFor(names []string) string {
	var heavy []string
	for _, n := range names {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(n), ".jar"))
		if KnownHeavy[base] {
			heavy = append(heavy, filepath.Base(n))
		}
	}
	if len(heavy) > 0 {
		return fmt.Sprintf("AVISO DE RENDIMIENTO: %s son plugins exigentes; pueden bajar el TPS. Si notas caÃ­das, ejecuta 'doctor tps'.", strings.Join(heavy, ", "))
	}
	if len(names) >= 8 {
		return "AVISO DE RENDIMIENTO: hay mÃ¡s de 8 plugins/mods. Cada uno suma trabajo al servidor; vigila el TPS con 'doctor tps'."
	}
	return ""
}
