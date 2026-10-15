// Package plugins es el catálogo de plugins/mods instalados en una instancia:
// carpeta destino según el motor (paper → plugins/, fabric|forge|neoforge →
// mods/, vanilla no admite plugins), alta, baja y listado.
//
// La instalación (transporte, verificación y movimiento atómico) vive en los
// módulos internos:
//
//   - internal/plugins/download implementa la obtención segura del artefacto
//     (streaming con límite, temporal + rename atómico, https obligatorio y
//     política anti-SSRF que valida la IP resuelta de cada conexión);
//   - internal/plugins/archive valida la ESTRUCTURA del JAR/ZIP con límites de
//     recursos y sin descomprimir nada;
//   - internal/plugins/marketplace (y sus subpaquetes modrinth, hangar, resolve e
//     install) resuelve e instala desde un marketplace reutilizando exactamente
//     esa maquinaria: ver docs/plugins-marketplace.md.
//
// Este paquete mantiene además el registro de PROCEDENCIA de lo instalado
// (provenance.go): de qué proyecto y versión salió cada archivo y con qué
// checksum se verificó. Es aditivo: un registro antiguo con solo el nombre se
// sigue leyendo, con origen desconocido.
//
// Un plugin/mod es código de terceros: ver ThirdPartyWarning.
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
		return errx.Usage("nombre inválido", "usa 'plugin list'")
	}
	path := filepath.Join(dir, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return errx.NotFound("el plugin %q no está instalado", "usa 'plugin list'")
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
		return fmt.Sprintf("AVISO DE RENDIMIENTO: %s son plugins exigentes; pueden bajar el TPS. Si notas caídas, ejecuta 'doctor tps'.", strings.Join(heavy, ", "))
	}
	if len(names) >= 8 {
		return "AVISO DE RENDIMIENTO: hay más de 8 plugins/mods. Cada uno suma trabajo al servidor; vigila el TPS con 'doctor tps'."
	}
	return ""
}
