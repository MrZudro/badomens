// Package plugins gestiona la instalación de plugins/mods de cada instancia.
// La carpeta destino depende del motor: paper → plugins/, fabric|forge|neoforge
// → mods/, vanilla no admite plugins. Al instalar se advierte del riesgo de
// pérdida de TPS (la medición real llega en la fase F2 con doctor tps).
package plugins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
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

// Add copia un archivo local o descarga una URL a la carpeta de plugins/mods.
func (i *Installer) Add(ctx context.Context, spec *instance.Spec, source string) (string, error) {
	if spec.Engine == "vanilla" {
		return "", errx.Usage("vanilla no admite plugins ni mods", "usa un motor con soporte (paper, fabric, forge, neoforge)")
	}
	targetDir := i.Dir(spec)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return i.download(ctx, source, targetDir)
	}
	name := filepath.Base(source)
	if name == "." || name == string(os.PathSeparator) || name == "" {
		return "", errx.Usage("ruta de plugin inválida", "indica un archivo .jar o una URL de descarga")
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return "", errx.NotFound("no se pudo leer el archivo "+source, "revisa la ruta")
	}
	if err := os.WriteFile(filepath.Join(targetDir, name), raw, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

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
		if !e.IsDir() {
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

func (i *Installer) download(ctx context.Context, source, targetDir string) (string, error) {
	u, err := url.Parse(source)
	if err != nil {
		return "", errx.Usage("URL de descarga inválida", "revisa la URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "badomens/0")
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", errx.Wrap(errx.CodeUnavailable, err, "no se pudo descargar el plugin", "comprueba la URL")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", errx.Unavailable(fmt.Sprintf("la descarga respondió %d", resp.StatusCode), "revisa la URL")
	}
	name := filepath.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		name = "plugin-" + time.Now().UTC().Format("20060102T150405")
	}
	if dec, derr := url.PathUnescape(name); derr == nil {
		name = dec
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) {
		name = strings.NewReplacer("/", "-", `\`, "-").Replace(name)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024*1024))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(targetDir, name), raw, 0o644); err != nil {
		return "", err
	}
	return name, nil
}
