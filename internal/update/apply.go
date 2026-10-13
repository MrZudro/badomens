// Uso de la API pública de GitHub (read-only) para releases.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/version"
)

// applyClient admite descargas grandes con margen (el binario comprimido puede
// superar los 10 s del cliente de Check).
var applyClient = &http.Client{Timeout: 120 * time.Second}

// currentExecutable localiza el binario en marcha; es inyectable en pruebas.
var currentExecutable = os.Executable

// releaseURL y releaseAPI son inyectables para las pruebas (httptest).
var releaseURL = func(repo, version, asset string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", repo, version, asset)
}

var releaseAPI = func(repo string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
	HTMLURL string         `json:"html_url"`
}

// ApplyResult describe una actualización aplicada.
type ApplyResult struct {
	From            string `json:"from"`
	To              string `json:"to"`
	RestartRequired bool   `json:"restart_required"`
	Notes           string `json:"notes,omitempty"`
}

// LatestRelease consulta la última release con sus assets.
func LatestRelease(ctx context.Context, repo string) (tag, htmlURL string, err error) {
	rel, err := latestRelease(ctx, repo)
	if err != nil {
		return "", "", err
	}
	return rel.TagName, rel.HTMLURL, nil
}

func latestRelease(ctx context.Context, repo string) (release, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" || !strings.Contains(repo, "/") {
		return release{}, errx.Usage(
			"repositorio inválido: "+repo,
			"configura 'repo: owner/nombre' en config.yaml",
		)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI(repo), nil)
	if err != nil {
		return release{}, errx.Wrap(errx.CodeGeneral, err, "no se pudo preparar la consulta de la release", "")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "badomens/"+version.Version)

	resp, err := applyClient.Do(req)
	if err != nil {
		return release{}, errx.Wrap(errx.CodeUnavailable, err, "no se pudo consultar GitHub", "comprueba tu conexión")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return release{}, errx.NotFound("el repositorio "+repo+" no tiene releases publicadas", "")
	}
	if resp.StatusCode != http.StatusOK {
		return release{}, errx.Unavailable(fmt.Sprintf("GitHub respondió %d", resp.StatusCode), "reintenta más tarde")
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, errx.Wrap(errx.CodeGeneral, err, "respuesta de GitHub ilegible", "")
	}
	return rel, nil
}

// assetExistente devuelve la URL del asset pedido si ya está publicado; si el
// asset no aparece (releases previas a F5), devuelve "" para construir la URL
// por composición, como hacía el instalador.
func (r release) asset(name string) string {
	for _, a := range r.Assets {
		if a.Name == name && a.URL != "" {
			return a.URL
		}
	}
	return ""
}

// Apply descarga la última release, verifica su checksum y reemplaza el
// binario en marcha. Nunca ejecuta código desconocido: si el sha256 no
// coincide, aborta sin tocar nada.
func Apply(ctx context.Context, repo string) (ApplyResult, error) {
	rel, err := latestRelease(ctx, repo)
	if err != nil {
		return ApplyResult{}, err
	}
	tag := strings.TrimPrefix(rel.TagName, "v")
	applyRes := ApplyResult{From: version.Version, To: tag}

	assetName := fmt.Sprintf("badomens_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName = fmt.Sprintf("badomens_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	}
	repoClean := strings.TrimSpace(repo)
	assetURL := rel.asset(assetName)
	if assetURL == "" {
		assetURL = releaseURL(repoClean, tag, assetName)
	}
	checksumURL := rel.asset("checksums.txt")
	if checksumURL == "" {
		checksumURL = releaseURL(repoClean, tag, "checksums.txt")
	}

	assetBytes, err := download(ctx, assetURL)
	if err != nil {
		return ApplyResult{}, errx.Wrap(errx.CodeUnavailable, err, "no se pudo descargar "+assetName, "reintenta más tarde")
	}
	checksumBytes, err := download(ctx, checksumURL)
	if err != nil {
		return ApplyResult{}, errx.Wrap(errx.CodeUnavailable, err, "no se pudo descargar checksums.txt", "reintenta más tarde")
	}
	expected, ok := checksumFor(checksumBytes, assetName)
	if !ok {
		return ApplyResult{}, errx.Unavailable("checksums.txt no contiene "+assetName, "no se actualiza sin verificar el checksum")
	}
	got := strings.ToLower(sha256hex(assetBytes))
	if !strings.EqualFold(got, expected) {
		return ApplyResult{}, errx.New(
			errx.CodeUnavailable,
			"checksum no coincide: se esperaba "+expected+" y llegó "+got,
			"no se aplica nada; reintenta más tarde o descarga manual desde la release",
		)
	}

	exe, err := currentExecutable()
	if err != nil || exe == "" {
		return ApplyResult{}, errx.New(errx.CodeGeneral, "no se pudo localizar el binario en ejecución", "")
	}
	bin, err := extractBinary(assetBytes, assetName)
	if err != nil {
		return ApplyResult{}, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".badomens-new-*")
	if err != nil {
		return ApplyResult{}, errx.Wrap(errx.CodeGeneral, err, "no se pudo preparar el binario nuevo", "")
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		return ApplyResult{}, errx.Wrap(errx.CodeGeneral, err, "no se pudo escribir el binario nuevo", "")
	}
	if err := tmp.Close(); err != nil {
		return ApplyResult{}, errx.Wrap(errx.CodeGeneral, err, "no se pudo finalizar el binario nuevo", "")
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return ApplyResult{}, errx.Wrap(errx.CodeGeneral, err, "no se pudieron fijar permisos del binario", "")
	}

	restart, err := replaceExecutable(tmpName, exe)
	if err != nil {
		return ApplyResult{}, errx.Wrap(errx.CodeGeneral, err,
			"no se pudo instalar el binario en "+exe,
			"descárgalo manualmente desde la release o reinicia y reintenta")
	}
	applyRes.RestartRequired = restart
	if restart {
		applyRes.Notes = "cierra y vuelve a abrir 'badomens' para usar la versión " + tag
	}
	return applyRes, nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "badomens/"+version.Version)
	resp, err := applyClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s respondió %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256*1024*1024))
}

// checksumFor busca "hash  asset" en el contenido de checksums.txt.
func checksumFor(checksums []byte, asset string) (string, bool) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasSuffix(fields[1], asset) {
			return fields[0], true
		}
	}
	return "", false
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// extractBinary saca el binario del paquete (zip en Windows, tar.gz en el
// resto) leyendo desde memoria.
func extractBinary(pkg []byte, asset string) ([]byte, error) {
	want := "badomens"
	if strings.HasSuffix(asset, ".zip") {
		want = "badomens.exe"
	}
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
		if err != nil {
			return nil, errx.Wrap(errx.CodeGeneral, err, "paquete zip ilegible", "")
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, errx.Wrap(errx.CodeGeneral, err, "no se pudo abrir el binario del zip", "")
				}
				defer func() { _ = rc.Close() }()
				return io.ReadAll(io.LimitReader(rc, 512*1024*1024))
			}
		}
		return nil, errx.NotFound("el paquete no contiene "+want, "")
	}

	gz, err := gzip.NewReader(bytes.NewReader(pkg))
	if err != nil {
		return nil, errx.Wrap(errx.CodeGeneral, err, "paquete tar.gz ilegible", "")
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errx.Wrap(errx.CodeGeneral, err, "paquete tar.gz ilegible", "")
		}
		if filepath.Base(hdr.Name) == want {
			return io.ReadAll(io.LimitReader(tr, 512*1024*1024))
		}
	}
	return nil, errx.NotFound("el paquete no contiene "+want, "")
}

// replaceExecutable instala el binario nuevo sobre el actual. En Unix se puede
// renombrar sobre un binario en ejecución; en Windows se intenta renombrar el
// actual a .old y, si no se puede (binario abierto), se deja el nuevo como
// .new para que el operador lo mueva/reinicie.
func replaceExecutable(tmp, current string) (restart bool, err error) {
	if runtime.GOOS == "windows" {
		if rerr := os.Rename(current, current+".old"); rerr == nil {
			if ierr := os.Rename(tmp, current); ierr != nil {
				_ = os.Rename(current+".old", current) // restaura
				return true, ierr
			}
			return true, nil // el proceso actual sigue con el .old
		}
		newPath := current + ".new"
		if cerr := os.Rename(tmp, newPath); cerr != nil {
			return true, cerr
		}
		return true, nil
	}
	if err := os.Rename(tmp, current); err != nil {
		return false, err
	}
	return false, nil
}
