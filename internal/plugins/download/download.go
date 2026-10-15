package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MrZudro/badomens/internal/errx"
)

// Request describe el artefacto que se quiere traer al directorio destino.
type Request struct {
	// URL es la dirección https:// de origen (http:// solo con AllowHTTP).
	URL string
	// DestDir es el directorio destino (plugins/ o mods/ de la instancia).
	DestDir string
	// Name es el nombre final; vacío ⇒ se deriva del último segmento de la URL.
	Name string
	// SHA256 es el checksum esperado en hexadecimal (opcional). Si está
	// presente, la instalación se rechaza si no coincide.
	SHA256 string
	// Policy reúne los límites y las excepciones.
	Policy Policy
}

// Result describe un artefacto ya instalado en su destino final.
type Result struct {
	// Path es la ruta final del archivo.
	Path string
	// Name es el nombre final (puede llevar sufijo si hubo conflicto).
	Name string
	// Bytes es el tamaño instalado.
	Bytes int64
	// SHA256 es el checksum calculado durante la descarga/copia.
	SHA256 string
	// Source es la URL o la ruta local de origen.
	Source string
	// Replaced indica si se sustituyó un archivo existente.
	Replaced bool
	// RenamedFrom es el nombre original cuando se renombró por conflicto.
	RenamedFrom string
}

// Staged es un artefacto ya escrito en un temporal del directorio destino y
// verificado en tamaño y checksum, pero que AÚN no está instalado: solo Commit
// lo mueve a su sitio. Hasta entonces el servidor no lo ve.
type Staged struct {
	path   string
	dest   string
	name   string
	expect string
	bytes  int64
	sha256 string
	source string
	policy Policy
	done   bool
}

// Path devuelve la ruta del archivo temporal (para verificaciones adicionales,
// por ejemplo la estructura ZIP/JAR).
func (s *Staged) Path() string { return s.path }

// Name devuelve el nombre final previsto.
func (s *Staged) Name() string { return s.name }

// Bytes devuelve el tamaño descargado.
func (s *Staged) Bytes() int64 { return s.bytes }

// SHA256 devuelve el checksum calculado.
func (s *Staged) SHA256() string { return s.sha256 }

// Source devuelve el origen (URL o ruta local).
func (s *Staged) Source() string { return s.source }

// StageURL descarga la URL a un temporal del directorio destino. No instala
// nada: hay que llamar a Commit (mueve) o Discard (borra). El límite de tamaño
// se aplica durante la copia, nunca cargando el artefacto en memoria.
func StageURL(ctx context.Context, req Request) (st *Staged, err error) {
	pol := req.Policy.withDefaults()
	if err := pol.Validate(); err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(req.URL)
	if raw == "" {
		return nil, errx.New(errx.CodeUsage, "falta la URL de descarga", "indica una URL https:// a un .jar o .zip").
			WithDetail("reason", "url_vacia")
	}
	u, perr := url.Parse(raw)
	if perr != nil || u.Host == "" {
		return nil, errx.New(errx.CodeUsage, "URL de descarga inválida: "+raw, "revisa la URL").
			WithDetail("reason", "url_invalida")
	}
	if err := pol.checkURL(u); err != nil {
		return nil, err
	}
	name, err := sourceName(req.Name, u, pol)
	if err != nil {
		return nil, err
	}

	st, err = newStaged(req.DestDir, name, raw, strings.TrimSpace(req.SHA256), pol)
	if err != nil {
		return nil, err
	}
	defer discardOnError(st, &err)

	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errx.New(errx.CodeUsage, "no se pudo preparar la descarga de "+raw, "revisa la URL").
			WithDetail("reason", "url_invalida")
	}
	hreq.Header.Set("User-Agent", pol.UserAgent)
	hreq.Header.Set("Accept", "application/java-archive, application/zip, application/octet-stream;q=0.8, */*;q=0.1")

	resp, err := pol.HTTPClient().Do(hreq)
	if err != nil {
		return nil, transportError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp.StatusCode)
	}
	if resp.ContentLength > pol.MaxBytes {
		return nil, tooLarge(resp.ContentLength, pol.MaxBytes)
	}
	if err := st.store(resp.Body, pol.MaxBytes); err != nil {
		return nil, err
	}
	return st, nil
}

// StageFile copia un archivo local al temporal del directorio destino con las
// mismas garantías (límite aplicado durante la copia, checksum, nombre validado
// y sin sobrescribir el destino).
func StageFile(src string, req Request) (st *Staged, err error) {
	pol := req.Policy.withDefaults()
	if err := pol.Validate(); err != nil {
		return nil, err
	}
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, errx.New(errx.CodeUsage, "falta la ruta del archivo", "indica un .jar o .zip local").
			WithDetail("reason", "origen_vacio")
	}
	info, serr := os.Stat(src)
	if serr != nil {
		if errors.Is(serr, os.ErrNotExist) {
			return nil, errx.NotFound("no existe el archivo "+src, "revisa la ruta")
		}
		return nil, errx.Wrap(errx.CodeGeneral, serr, "no se pudo leer el archivo "+src, "revisa la ruta y los permisos")
	}
	if info.IsDir() {
		return nil, errx.New(errx.CodeUsage, "la ruta es un directorio: "+src, "indica un archivo .jar o .zip").
			WithDetail("reason", "origen_invalido")
	}
	if info.Size() > pol.MaxBytes {
		return nil, tooLarge(info.Size(), pol.MaxBytes)
	}
	name := req.Name
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(filepath.Clean(src))
	}
	name, err = NormalizeName(name, pol.AllowedExtensions)
	if err != nil {
		return nil, err
	}
	st, err = newStaged(req.DestDir, name, src, strings.TrimSpace(req.SHA256), pol)
	if err != nil {
		return nil, err
	}
	defer discardOnError(st, &err)

	f, oerr := os.Open(src) // #nosec G304 -- ruta local de plugin indicada por el usuario (vía avanzada)
	if oerr != nil {
		return nil, errx.NotFound("no se pudo abrir el archivo "+src, "revisa la ruta y los permisos")
	}
	defer func() { _ = f.Close() }()
	if err := st.store(f, pol.MaxBytes); err != nil {
		return nil, err
	}
	return st, nil
}

// discardOnError borra el temporal cuando la función que lo creó termina con
// error, de modo que nunca queda un archivo parcial.
func discardOnError(st *Staged, errp *error) {
	if errp != nil && *errp != nil && st != nil {
		_ = st.Discard()
	}
}

// sourceName obtiene (y valida) el nombre final: el explícito si lo hay, o el
// último segmento de la URL tal y como aparece codificado. Se descodifica
// después de recortarlo para que `%2e%2e%2f` o `a%2fb.jar` se rechacen en lugar
// de colarse como ruta.
func sourceName(explicit string, u *url.URL, pol Policy) (string, error) {
	name := explicit
	if strings.TrimSpace(name) == "" {
		base := path.Base(u.EscapedPath())
		if dec, derr := url.PathUnescape(base); derr == nil {
			base = dec
		}
		name = base
	}
	return NormalizeName(name, pol.AllowedExtensions)
}

// newStaged crea el archivo temporal dentro del directorio destino: el
// movimiento posterior es un rename en el mismo sistema de archivos, por tanto
// atómico. Guarda el checksum esperado para verificarlo al cerrar la escritura.
func newStaged(destDir, name, source, expect string, pol Policy) (*Staged, error) {
	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return nil, errx.New(errx.CodeGeneral, "no se indicó el directorio destino", "")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, errx.Wrap(errx.CodeGeneral, err, "no se pudo preparar la carpeta "+destDir, "revisa permisos")
	}
	f, err := os.CreateTemp(destDir, tempPattern)
	if err != nil {
		return nil, errx.Wrap(errx.CodeGeneral, err, "no se pudo crear el archivo temporal en "+destDir, "revisa permisos y espacio en disco")
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(f.Name())
		return nil, errx.Wrap(errx.CodeGeneral, cerr, "no se pudo preparar el archivo temporal", "")
	}
	return &Staged{path: f.Name(), dest: destDir, name: name, expect: expect, source: source, policy: pol}, nil
}

// store copia el contenido al temporal aplicando el límite durante la copia,
// calculando el SHA-256 al vuelo y comprobando el checksum esperado.
func (s *Staged) store(src io.Reader, limit int64) error {
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- temporal creado por nosotros
	if err != nil {
		return errx.Wrap(errx.CodeGeneral, err, "no se pudo escribir el archivo temporal", "revisa permisos y espacio en disco")
	}
	sum, cerr := copyLimited(f, src, limit)
	if cerr != nil {
		_ = f.Close()
		return cerr
	}
	if serr := f.Sync(); serr != nil {
		_ = f.Close()
		return errx.Wrap(errx.CodeGeneral, serr, "no se pudo asegurar el archivo temporal en disco", "")
	}
	if cerr := f.Close(); cerr != nil {
		return errx.Wrap(errx.CodeGeneral, cerr, "no se pudo cerrar el archivo temporal", "")
	}
	// El temporal se crea con 0o600; los artefactos instalados quedan legibles
	// (0o644) para que el servidor pueda cargarlos desde el contenedor.
	if cerr := os.Chmod(s.path, 0o644); cerr != nil {
		return errx.Wrap(errx.CodeGeneral, cerr, "no se pudieron fijar los permisos del artefacto", "")
	}
	s.bytes, s.sha256 = sum.bytes, sum.sha256
	if sum.bytes == 0 {
		return errx.New(errx.CodeUsage, "el artefacto está vacío (0 bytes)", "comprueba el origen").
			WithDetail("reason", "artefacto_vacio")
	}
	if s.expect != "" && !strings.EqualFold(s.expect, sum.sha256) {
		return checksumMismatch(s.expect, sum.sha256)
	}
	return nil
}

// checksum describe el resultado de una copia limitada.
type checksum struct {
	bytes  int64
	sha256 string
}

// copyLimited copia src→dst aplicando el límite DURANTE la copia (nada se carga
// en memoria) y devuelve el tamaño y el SHA-256. Si el origen entrega más bytes
// que el límite se aborta en cuanto se detecta.
func copyLimited(dst io.Writer, src io.Reader, limit int64) (checksum, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, limit+1))
	if err != nil {
		return checksum{}, errx.Wrap(errx.CodeUnavailable, err, "la transferencia se interrumpió", "reintenta la descarga")
	}
	if n > limit {
		return checksum{}, tooLarge(n, limit)
	}
	return checksum{bytes: n, sha256: hex.EncodeToString(h.Sum(nil))}, nil
}

// tooLarge construye el error de límite excedido.
func tooLarge(got, limit int64) error {
	message := fmt.Sprintf("el artefacto supera el límite de %s", HumanBytes(limit))
	if got > 0 {
		message = fmt.Sprintf("el artefacto (%s) supera el límite de %s", HumanBytes(got), HumanBytes(limit))
	}
	return errx.New(errx.CodeCapacity, message,
		"sube el límite con --max-size solo si confías en el origen").
		WithDetail("reason", "limite_excedido").
		WithDetail("limit_bytes", limit)
}

// checksumMismatch construye el error de integridad: no se instala nada.
func checksumMismatch(expected, got string) error {
	return errx.New(errx.CodeGeneral,
		"el checksum SHA-256 no coincide: se esperaba "+expected+" y se obtuvo "+got,
		"no se instala nada; comprueba el checksum publicado por el origen").
		WithDetail("reason", "checksum_no_coincide").
		WithDetail("expected_sha256", expected).
		WithDetail("got_sha256", got)
}

// transportError conserva los errores tipados (política de destino, TLS…) y
// envuelve el resto como servicio no disponible.
func transportError(err error) error {
	var typed *errx.Error
	if errors.As(err, &typed) {
		return typed
	}
	return errx.Wrap(errx.CodeUnavailable, err, "no se pudo descargar el artefacto", "comprueba la URL y tu conexión")
}

// httpError traduce un código HTTP de error. Se conserva el código estable
// `error_unavailable` del contrato previo y se añade `details.http_status`, que
// es aditivo.
func httpError(status int) error {
	message := fmt.Sprintf("la descarga respondió %d %s", status, http.StatusText(status))
	hint := "revisa la URL"
	switch {
	case status == http.StatusNotFound:
		message = "el origen respondió 404: el archivo no existe en esa URL"
	case status == http.StatusForbidden:
		hint = "el origen exige autenticación o ha bloqueado la descarga"
	case status >= 500:
		hint = "reintenta más tarde o usa otro origen"
	}
	return errx.Unavailable(message, hint).
		WithDetail("reason", "http_error").
		WithDetail("http_status", status)
}

// Discard borra el archivo temporal. Es idempotente y seguro después de Commit.
func (s *Staged) Discard() error {
	if s == nil || s.done {
		return nil
	}
	s.done = true
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Commit resuelve el conflicto de nombre (si lo hay) y mueve el temporal a su
// destino final. Es el único punto en el que el artefacto pasa a existir como
// plugin/mods.
func (s *Staged) Commit() (Result, error) {
	if s == nil || s.done {
		return Result{}, errx.New(errx.CodeGeneral, "el artefacto temporal ya se cerró", "")
	}
	dest, replaced, renamedFrom, err := s.resolveDest()
	if err != nil {
		return Result{}, err
	}
	if rerr := os.Rename(s.path, dest); rerr != nil {
		// Windows no reemplaza con os.Rename: con política explícita de
		// reemplazo se borra antes el destino y se reintenta (ventana no
		// atómica, documentada en docs/plugins-seguridad.md).
		if !replaced || runtime.GOOS != "windows" {
			return Result{}, errx.Wrap(errx.CodeGeneral, rerr, "no se pudo mover el artefacto a "+dest, "revisa permisos y espacio en disco")
		}
		if derr := os.Remove(dest); derr != nil {
			return Result{}, errx.Wrap(errx.CodeGeneral, derr, "no se pudo reemplazar "+dest, "elimina el archivo anterior a mano")
		}
		if rerr = os.Rename(s.path, dest); rerr != nil {
			return Result{}, errx.Wrap(errx.CodeGeneral, rerr, "no se pudo mover el artefacto a "+dest, "revisa permisos y espacio en disco")
		}
	}
	s.done = true
	return Result{
		Path:        dest,
		Name:        filepath.Base(dest),
		Bytes:       s.bytes,
		SHA256:      s.sha256,
		Source:      s.source,
		Replaced:    replaced,
		RenamedFrom: renamedFrom,
	}, nil
}

// resolveDest decide el nombre final aplicando la política de conflicto.
func (s *Staged) resolveDest() (dest string, replaced bool, renamedFrom string, err error) {
	dest = filepath.Join(s.dest, s.name)
	_, statErr := os.Lstat(dest)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		return dest, false, "", nil
	case statErr != nil:
		return "", false, "", errx.Wrap(errx.CodeGeneral, statErr, "no se pudo comprobar el destino "+dest, "revisa permisos")
	case s.policy.Conflict == ConflictReplace:
		return dest, true, "", nil
	case s.policy.Conflict == ConflictRename:
		alt, aerr := uniqueName(s.dest, s.name)
		if aerr != nil {
			return "", false, "", aerr
		}
		return alt, false, s.name, nil
	}
	return "", false, "", errx.New(errx.CodeUsage,
		"ya existe un archivo con el nombre "+s.name+" en el destino",
		"elimínalo con 'plugin remove', usa --on-conflict=rename para conservar ambos o --on-conflict=replace para sustituirlo").
		WithDetail("reason", "destino_existente").
		WithDetail("name", s.name)
}

// uniqueName busca <base>-N<ext> libre, hasta 99 intentos.
func uniqueName(dir, name string) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 2; n <= 99; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, n, ext))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", errx.New(errx.CodeUsage,
		"hay demasiados archivos con el nombre "+name+" en el destino",
		"elimina los antiguos con 'plugin remove' o usa --on-conflict=replace").
		WithDetail("reason", "destino_saturado")
}
