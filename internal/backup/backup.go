// Package backup crea, lista, restaura y poda copias de seguridad del directorio
// server/ de cada instancia (mundo + configuración). Cada copia es un zip con
// su sha256 en un archivo .sha256 al lado.
//
// Endurecimiento (plan 12.4 · spec 6.3):
//   - el nombre del backup es un IDENTIFICADOR, nunca una ruta (sin separadores,
//     sin "..", sin caracteres de control);
//   - la restauración VERIFICA el sha256 ANTES de tocar un solo byte de los datos;
//   - la extracción va a un directorio temporal DENTRO de la instancia y la
//     sustitución es por rename, de modo que un fallo deja recuperable el estado
//     anterior;
//   - hay límites de tamaño de origen y de archivo (no se empaqueta sin control).
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/worlds"
)

// Límites de las copias de seguridad. Son variables (no constantes) para poder
// acotarlos desde las pruebas y, si hiciera falta, desde la configuración.
var (
	// MaxSourceBytes es el tamaño máximo del árbol server/ que se acepta
	// empaquetar en UNA copia (8 GiB): evita llenar el disco por accidente.
	MaxSourceBytes int64 = 8 << 30
	// MaxArchiveBytes es el tamaño máximo del ZIP resultante (el anterior más un
	// margen para la cabecera y el manifiesto).
	MaxArchiveBytes int64 = MaxSourceBytes + (256 << 20)
)

// maxNameLen acota la longitud del nombre del backup.
const maxNameLen = 120

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
	if err := instance.ValidateSlug(slug); err != nil {
		return Info{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = time.Now().UTC().Format("20060102T150405")
	}
	if err := ValidateName(name); err != nil {
		return Info{}, err
	}
	dir := b.Dir(slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, err
	}
	serverDir := filepath.Join(b.DataDir, "instances", slug, "server")
	if !dirExists(serverDir) {
		return Info{}, errx.NotFound("la instancia no tiene carpeta server/", "arranca la instancia al menos una vez")
	}
	size := dirSize(serverDir)
	if size > MaxSourceBytes {
		return Info{}, errx.Capacity(
			fmt.Sprintf("el contenido de la instancia (%d bytes) supera el límite de una copia (%d bytes)", size, MaxSourceBytes),
			"excluye datos con 'backup prune'/'world' o crea la copia a mano",
		).WithDetail("reason", "limite_excedido").WithDetail("size_bytes", size)
	}
	path := filepath.Join(dir, name+".zip")
	if _, err := os.Stat(path); err == nil {
		return Info{}, errx.Usage("ya existe un backup llamado "+fmt.Sprintf("%q", name),
			"usa otro nombre o 'backup list' para ver los existentes")
	}
	// Se escribe en un temporal y se mueve de forma atómica: nunca queda un .zip
	// a medias con su .sha256 ya escrito.
	tmp := path + ".part"
	_ = os.Remove(tmp)
	if err := worlds.ZipDir(serverDir, tmp, map[string]string{
		"slug": slug, "created": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		_ = os.Remove(tmp)
		return Info{}, err
	}
	if fi, serr := os.Stat(tmp); serr == nil && fi.Size() > MaxArchiveBytes {
		_ = os.Remove(tmp)
		return Info{}, errx.Capacity("la copia resultante supera el límite de tamaño",
			"reduce el contenido de la instancia").WithDetail("reason", "limite_excedido")
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return Info{}, err
	}
	sha, err := fileSHA256(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return Info{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Info{}, err
	}
	if err := os.WriteFile(path+".sha256", []byte(sha), 0o600); err != nil {
		return Info{}, err
	}
	return Info{
		Name: name, Path: path, Size: fileSize(path), Sha256: sha,
		Created: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// List devuelve los backups de la instancia ordenados por nombre (más reciente
// primero). Verifica el checksum de cada copia sin bloquear la lista si falta.
func (b *Backend) List(slug string) ([]Info, error) {
	if err := instance.ValidateSlug(slug); err != nil {
		return nil, err
	}
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
		// El checksum se LEE (barato); verificarlo recorre el archivo entero, así
		// que solo lo hacen la restauración y Backend.Verify.
		if sha, err := os.ReadFile(path + ".sha256"); err == nil { // #nosec G304 -- path construido por List() dentro del data dir
			info.Sha256 = strings.TrimSpace(string(sha))
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// Restore sustituye server/ de la instancia por el contenido de un backup. La
// instancia debe estar detenida.
//
// Garantías (plan 12.4):
//  1. el sha256 se verifica ANTES de modificar un solo byte;
//  2. el zip se extrae a un directorio temporal DENTRO de la instancia, así que
//     un zip corrupto o inseguro no toca los datos actuales;
//  3. la sustitución es un rename con reversión: si el segundo rename falla, se
//     devuelve el directorio anterior a su sitio.
func (b *Backend) Restore(slug, name string) error {
	if err := instance.ValidateSlug(slug); err != nil {
		return err
	}
	if err := ValidateName(strings.TrimSpace(name)); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	dir := b.Dir(slug)
	path := filepath.Join(dir, name+".zip")
	if _, err := os.Stat(path); err != nil {
		return errx.NotFound("el backup "+fmt.Sprintf("%q", name)+" no existe", "ejecuta 'backup list'")
	}
	expected, rerr := os.ReadFile(path + ".sha256") // #nosec G304 -- path dentro de la carpeta de backups (nombre validado)
	if rerr != nil {
		return errx.Wrap(errx.CodeGeneral, rerr,
			"el backup no tiene checksum: no se puede verificar antes de restaurar",
			"vuelve a crear el backup")
	}
	sum := strings.TrimSpace(string(expected))
	if sum == "" {
		return errx.New(errx.CodeGeneral,
			"el backup no tiene checksum: no se puede verificar antes de restaurar",
			"vuelve a crear el backup").WithDetail("reason", "checksum_ausente")
	}
	if err := verifyChecksum(path, sum); err != nil {
		return err
	}

	serverDir := filepath.Join(b.DataDir, "instances", slug, "server")
	stageDir := filepath.Join(b.DataDir, "instances", slug, ".restore-tmp")
	backupDir := filepath.Join(b.DataDir, "instances", slug, ".restore-old")
	_ = os.RemoveAll(stageDir)
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return err
	}
	if err := worlds.Unzip(path, stageDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return errx.Wrap(errx.CodeGeneral, err, "el backup no se pudo extraer; los datos actuales NO se han tocado",
			"informa del backup corrupto y usa otro")
	}

	_ = os.RemoveAll(backupDir)
	moved := false
	if dirExists(serverDir) {
		if err := os.Rename(serverDir, backupDir); err != nil {
			_ = os.RemoveAll(stageDir)
			return err
		}
		moved = true
	}
	if err := os.Rename(stageDir, serverDir); err != nil {
		if moved {
			_ = os.Rename(backupDir, serverDir) // reversión: se conserva el estado previo
		}
		_ = os.RemoveAll(stageDir)
		return errx.Wrap(errx.CodeGeneral, err,
			"no se pudo sustituir el directorio de la instancia; se restauró el anterior", "")
	}
	_ = os.RemoveAll(backupDir)
	return nil
}

// Verify comprueba el sha256 de un backup SIN tocar los datos de la instancia.
// Es la verificación que hace la restauración antes de actuar, expuesta para
// diagnosticar una copia sospechosa.
func (b *Backend) Verify(slug, name string) error {
	if err := instance.ValidateSlug(slug); err != nil {
		return err
	}
	if err := ValidateName(strings.TrimSpace(name)); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	path := filepath.Join(b.Dir(slug), name+".zip")
	if _, err := os.Stat(path); err != nil {
		return errx.NotFound("el backup "+fmt.Sprintf("%q", name)+" no existe", "ejecuta 'backup list'")
	}
	expected, rerr := os.ReadFile(path + ".sha256") // #nosec G304 -- path dentro de la carpeta de backups
	if rerr != nil {
		return errx.Wrap(errx.CodeGeneral, rerr, "el backup no tiene checksum guardado",
			"vuelve a crear el backup").WithDetail("reason", "checksum_ausente")
	}
	return verifyChecksum(path, string(expected))
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

// ValidateName comprueba que un nombre de backup es un IDENTIFICADOR seguro y no
// una ruta: rechaza vacíos, separadores, "..", caracteres de control, puntos o
// espacios finales y nombres reservados de Windows.
func ValidateName(name string) error {
	if name == "" {
		return errx.Usage("nombre de backup vacío", "usa un nombre como 'antes-de-actualizar'")
	}
	if len(name) > maxNameLen {
		return errx.Usage("nombre de backup demasiado largo", "usa como máximo 120 caracteres")
	}
	if name != strings.TrimSpace(name) {
		return errx.Usage("nombre de backup inválido: "+fmt.Sprintf("%q", name),
			"sin espacios al principio ni al final")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return errx.Usage("nombre de backup inválido: "+fmt.Sprintf("%q", name),
			"sin separadores ni '..'")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errx.Usage("nombre de backup inválido: contiene caracteres de control",
				"usa letras, números, guiones y puntos")
		}
	}
	if strings.HasSuffix(name, ".") || strings.HasPrefix(name, ".") {
		return errx.Usage("nombre de backup inválido: "+fmt.Sprintf("%q", name),
			"no puede empezar ni terminar por punto")
	}
	if reservedWindowsName(name) {
		return errx.Usage("nombre de backup reservado por el sistema: "+fmt.Sprintf("%q", name),
			"usa otro nombre")
	}
	return nil
}

// reservedWindowsName detecta los nombres de dispositivo reservados de Windows
// (CON, PRN, AUX, NUL, COM1-9, LPT1-9), con o sin extensión.
func reservedWindowsName(name string) bool {
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(stem) == 4 {
		upper := strings.ToUpper(stem)
		if upper[:3] == "COM" || upper[:3] == "LPT" {
			return stem[3] >= '1' && stem[3] <= '9'
		}
	}
	return false
}

// verifyChecksum comprueba que el sha256 del archivo coincide con el esperado.
func verifyChecksum(path, expected string) error {
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, strings.TrimSpace(expected)) {
		return errx.New(errx.CodeGeneral,
			"el checksum del backup no coincide: restauración abortada sin tocar los datos",
			"el archivo está corrupto o fue alterado; usa otra copia").
			WithDetail("reason", "checksum_no_coincide").
			WithDetail("expected", strings.TrimSpace(expected)).
			WithDetail("actual", got)
	}
	return nil
}

// fileSHA256 calcula el sha256 de un archivo sin cargarlo en memoria.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- archivos propios dentro del data dir
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

// dirSize suma el tamaño de los archivos regulares de un árbol.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil // #nosec G104 -- el tamaño es una estimación para el límite
		}
		total += info.Size()
		return nil
	})
	return total
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
