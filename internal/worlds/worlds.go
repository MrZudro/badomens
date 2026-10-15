// Package worlds gestiona la biblioteca de mundos compartida
// (<data>/worlds/<nombre>/) y su intercambio con el mundo activo de cada
// instancia (<data>/instances/<slug>/server/world).
package worlds

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

// Manager opera sobre la biblioteca de mundos en dataDir.
type Manager struct{ DataDir string }

// LibraryDir devuelve la carpeta de la biblioteca.
func (m *Manager) LibraryDir() string { return filepath.Join(m.DataDir, "worlds") }

// ensureLibrary crea la carpeta de la biblioteca con permisos de SOLO DUEÑO
// (plan 12.4 · mínimos privilegios). El contenedor no monta la biblioteca
// —solo el mundo activo de cada instancia—, así que su raíz no necesita acceso
// de grupo ni de otros; en cambio, los directorios internos que crea copyDir
// usan 0755 a propósito, porque root dentro del contenedor debe poder leerlos
// cuando el mundo se despliega como mundo activo.
func (m *Manager) ensureLibrary() error {
	return os.MkdirAll(m.LibraryDir(), 0o700)
}

// List devuelve los mundos guardados, ordenados.
func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(m.LibraryDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func worldPath(instDir string) string { return filepath.Join(instDir, "server", "world") }

// SaveWorld guarda el mundo activo de la instancia como <name> en la biblioteca.
func (m *Manager) SaveWorld(instDir, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	src := worldPath(instDir)
	if !isDir(src) {
		return errx.NotFound("la instancia no tiene un mundo generado", "arranca la instancia al menos una vez")
	}
	dst := filepath.Join(m.LibraryDir(), name)
	if isDir(dst) {
		return fmt.Errorf("el mundo %q ya existe en la biblioteca", name)
	}
	if err := m.ensureLibrary(); err != nil {
		return err
	}
	return copyDir(src, dst)
}

// UseWorld despliega el mundo <name> como mundo activo de la instancia.
func (m *Manager) UseWorld(instDir, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	src := filepath.Join(m.LibraryDir(), name)
	if !isDir(src) {
		return errx.NotFound("el mundo %q no está en la biblioteca", "ejecuta 'world save' primero o 'world list'")
	}
	dst := worldPath(instDir)
	if isDir(dst) {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}
	return copyDir(src, dst)
}

// Export zipea el mundo activo de la instancia a <data>/exports/world-<name>.zip
// con un manifiesto, y devuelve la ruta y el sha256.
func (m *Manager) Export(instDir, name string) (path, sha string, err error) {
	src := worldPath(instDir)
	if !isDir(src) {
		return "", "", errx.NotFound("la instancia no tiene un mundo generado", "arranca la instancia al menos una vez")
	}
	outDir := filepath.Join(m.DataDir, "exports")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", "", err
	}
	out := filepath.Join(outDir, fmt.Sprintf("world-%s-%s.zip", name, time.Now().UTC().Format("20060102T150405")))
	manifest := map[string]string{"name": name, "created": time.Now().UTC().Format(time.RFC3339)}
	if err := ZipDir(src, out, manifest); err != nil {
		return "", "", err
	}
	sha, err = Sha256(out)
	return out, sha, err
}

// ImportFromZip extrae un zip en la biblioteca como <name>. Espera el contenido
// del mundo en la raíz o dentro de una carpeta "world".
func (m *Manager) ImportFromZip(archive, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "badomens-world-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := Unzip(archive, tmp); err != nil {
		return err
	}
	src := filepath.Join(tmp, "world")
	if !isDir(src) {
		src = tmp
	}
	if !isDir(src) {
		return fmt.Errorf("el zip %s no contiene un mundo", archive)
	}
	dst := filepath.Join(m.LibraryDir(), name)
	if isDir(dst) {
		return fmt.Errorf("el mundo %q ya existe en la biblioteca", name)
	}
	if err := m.ensureLibrary(); err != nil {
		return err
	}
	return copyDir(src, dst)
}

// ZipDir crea un zip recursivo de src (con src como raíz) e incluye manifest.
func ZipDir(src, out string, manifest map[string]string) error {
	f, err := os.Create(out) // #nosec G304 -- destino controlado dentro del data dir (backups / exports)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()

	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		if w, werr := zw.Create("manifest.json"); werr == nil {
			_, _ = w.Write(raw)
		}
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return writeZipFile(zw, filepath.ToSlash(rel), path)
	})
}

// Unzip extrae un zip en dst con protección contra rutas inseguras.
func Unzip(archive, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("entrada insegura en el zip: %s", f.Name)
		}
		target := filepath.Join(dst, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil { // #nosec G301 -- el contenedor (root) debe poder leer los mundos extraídos
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { // #nosec G301 -- idem
			return err
		}
		rf, err := f.Open()
		if err != nil {
			return err
		}
		w, cerr := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644) // #nosec G304 -- ruta validada contra zip-slip y dentro del data dir
		if cerr != nil {
			_ = rf.Close()
			return cerr
		}
		_, cerr = io.Copy(w, rf) // #nosec G110 -- la importación de mundos la controla el operador (archivos propios)
		_ = rf.Close()
		_ = w.Close()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}

// Sha256 de un archivo.
func Sha256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- lectura de archivos propios dentro del data dir (backups, mundos)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeZipFile(zw *zip.Writer, name, path string) error {
	r, err := os.Open(path) // #nosec G304 -- lectura de archivos del mundo del data dir
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755) // #nosec G301 -- el contenedor (root) debe poder leer los mundos copiados
		}
		r, err := os.Open(path) // #nosec G304,G122 -- lectura de archivos del mundo del data dir (controlado por el operador)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		w, err := os.Create(target) // #nosec G304 -- ruta con rel normalizado dentro del data dir
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, r); err != nil {
			_ = w.Close()
			return err
		}
		_ = w.Close()
		return nil
	})
}
