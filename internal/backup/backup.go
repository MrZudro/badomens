// Package backup crea, lista, restaura y poda copias de seguridad del directorio
// server/ de cada instancia (mundo + configuración). Cada copia es un zip con
// su sha256 en un archivo .sha256 al lado.
package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/worlds"
)

// Backend opera sobre los backups de una instancia.
type Backend struct{ DataDir string }

// Info describe una copia de seguridad.
type Info struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Sha256  string `json:"sha256"`
	Created string `json:"created"`
}

// Dir devuelve la carpeta de backups de la instancia.
func (b *Backend) Dir(slug string) string {
	return filepath.Join(b.DataDir, "instances", slug, "backups")
}

// Create zipea server/ de la instancia como backups/<name>.zip.
func (b *Backend) Create(slug, name string) (Info, error) {
	dir := b.Dir(slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Info{}, err
	}
	serverDir := filepath.Join(b.DataDir, "instances", slug, "server")
	if !dirExists(serverDir) {
		return Info{}, errx.NotFound("la instancia no tiene carpeta server/", "arranca la instancia al menos una vez")
	}
	if name == "" {
		name = time.Now().UTC().Format("20060102T150405")
	}
	path := filepath.Join(dir, name+".zip")
	if _, err := os.Stat(path); err == nil {
		return Info{}, fmt.Errorf("ya existe un backup llamado %q", name)
	}
	if err := worlds.ZipDir(serverDir, path, map[string]string{
		"slug": slug, "created": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return Info{}, err
	}
	sha, err := worlds.Sha256(path)
	if err != nil {
		return Info{}, err
	}
	if err := os.WriteFile(path+".sha256", []byte(sha), 0o600); err != nil {
		return Info{}, err
	}
	return Info{Name: name, Path: path, Size: fileSize(path), Sha256: sha, Created: time.Now().UTC().Format(time.RFC3339)}, nil
}

// List devuelve los backups de la instancia ordenados por nombre.
func (b *Backend) List(slug string) ([]Info, error) {
	dir := b.Dir(slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Info{}, nil
		}
		return nil, err
	}
	var out []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".zip")
		path := filepath.Join(dir, e.Name())
		info := Info{Name: name, Path: path, Size: fileSize(path)}
		if sha, err := os.ReadFile(path + ".sha256"); err == nil {
			info.Sha256 = strings.TrimSpace(string(sha))
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// Restore sustituye server/ de la instancia por el contenido de un backup.
// La instancia debe estar detenida.
func (b *Backend) Restore(slug, name string) error {
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		return errx.Usage("nombre de backup inválido", "usa 'backup list' para ver los nombres")
	}
	path := filepath.Join(b.Dir(slug), name+".zip")
	if _, err := os.Stat(path); err != nil {
		return errx.NotFound("el backup %q no existe", "ejecuta 'backup list'")
	}
	serverDir := filepath.Join(b.DataDir, "instances", slug, "server")
	if dirExists(serverDir) {
		if err := os.RemoveAll(serverDir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		return err
	}
	return worlds.Unzip(path, serverDir)
}

// Prune elimina los backups más antiguos dejando solo los keep más recientes
// (por nombre, que es la marca de tiempo). Devuelve los nombres eliminados.
func (b *Backend) Prune(slug string, keep int) ([]string, error) {
	list, err := b.List(slug)
	if err != nil {
		return nil, err
	}
	if keep < 0 {
		keep = 0
	}
	var removed []string
	for i, info := range list {
		if i < keep {
			continue
		}
		if err := os.Remove(info.Path); err != nil {
			return removed, err
		}
		_ = os.Remove(info.Path + ".sha256")
		removed = append(removed, info.Name)
	}
	return removed, nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}
