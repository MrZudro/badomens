// Package install es el DESCARGADOR de marketplaces: toma una resolución
// completa del paquete resolve y la materializa sobre una instancia, reutilizando
// la maquinaria de seguridad de la fase 11.1 (internal/plugins/download y
// internal/plugins/archive). No crea ningún transporte paralelo ni ninguna ruta
// de escritura nueva.
//
// Flujo (el mismo del plan, en orden):
//
//  1. resolver proyecto → versión exacta → archivo exacto (resolve.Resolve);
//  2. descargar a un temporal DENTRO de la carpeta destino (streaming con
//     límite, https obligatorio, anti-SSRF, redirecciones validadas);
//  3. verificar tamaño, checksum publicado e integridad estructural JAR/ZIP;
//  4. mover el temporal a su destino (rename atómico);
//  5. actualizar el registro de procedencia (proyecto, versión, checksum) UNA
//     sola vez, y solo si todo lo anterior fue bien.
//
// Un fallo en cualquier paso no deja registro ni archivos parciales.
package install

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/archive"
	"github.com/MrZudro/badomens/internal/plugins/download"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/resolve"
)

// Motivos estables de este paquete (error.details.reason).
const (
	// ReasonFuente: el marketplace pedido no está registrado en el catálogo.
	ReasonFuente = "marketplace_no_registrado"
	// ReasonRegistro: falló el guardado del registro de procedencia.
	ReasonRegistro = "registro_no_guardado"
)

// Client orquesta la instalación desde marketplaces.
type Client struct {
	// Sources es el catálogo de marketplaces disponibles.
	Sources *marketplace.Catalog
	// Installer resuelve la carpeta destino según el motor de la instancia.
	Installer *plugins.Installer
	// Provenance es el registro de procedencia persistente.
	Provenance *plugins.Provenance
	// Now permite fijar el reloj en pruebas; nil ⇒ time.Now.
	Now func() time.Time
}

// New construye el cliente de instalación.
func New(cat *marketplace.Catalog, inst *plugins.Installer, prov *plugins.Provenance) *Client {
	return &Client{Sources: cat, Installer: inst, Provenance: prov}
}

// Request describe una instalación desde marketplace.
type Request struct {
	// Spec es la instancia destino.
	Spec *instance.Spec
	// Source identifica el marketplace.
	Source marketplace.SourceID
	// Project es el slug o el identificador del proyecto.
	Project string
	// Version fija la versión exacta; vacío ⇒ la más reciente compatible.
	Version string
	// Loader y GameVersion describen el entorno; si faltan se toman de la
	// instancia (motor y versión de Minecraft).
	Loader      string
	GameVersion string
	// Channels son los canales aceptados (vacío ⇒ solo release).
	Channels []string
	// AllowPrerelease acepta canales preliminares.
	AllowPrerelease bool
	// AllowUnverified acepta un artefacto sin checksum publicado.
	AllowUnverified bool
	// AllowMissingDeps convierte las dependencias obligatorias ausentes en aviso.
	AllowMissingDeps bool
	// Download es la política de 11.1 (límites, https, anti-SSRF, conflicto).
	Download download.Policy
	// Archive son los límites de inspección del JAR/ZIP.
	Archive archive.Limits
	// VersionLimit acota cuántas versiones se piden al proveedor.
	VersionLimit int
}

// Result es el resultado de una instalación completada.
type Result struct {
	// Resolution es la resolución aplicada (versión y archivo exactos).
	Resolution *resolve.Resolution `json:"resolution"`
	// Installed describe el archivo ya colocado en su destino.
	Installed plugins.Installed `json:"installed"`
	// Record es la entrada escrita en el registro de procedencia.
	Record plugins.Record `json:"record"`
	// Warnings son avisos que no impidieron la instalación.
	Warnings []string `json:"warnings,omitempty"`
}

// Install resuelve, descarga, verifica, mueve y registra. Devuelve error si
// cualquier paso falla; en ese caso no queda ni archivo instalado ni entrada de
// registro.
func (c *Client) Install(ctx context.Context, req Request) (*Result, error) {
	if req.Spec == nil {
		return nil, errx.New(errx.CodeUsage, "falta la instancia destino",
			"indica la instancia con --instance <slug>").WithDetail("reason", "instancia_ausente")
	}
	if req.Spec.Engine == "vanilla" {
		return nil, errx.New(errx.CodeUsage, "vanilla no admite plugins ni mods",
			"usa un motor con soporte (paper, fabric, forge, neoforge)").
			WithDetail("reason", "motor_sin_soporte")
	}
	src, err := c.source(req.Source)
	if err != nil {
		return nil, err
	}

	loader, mc := c.environment(req)
	resolution, err := resolve.Resolve(ctx, resolve.Request{
		Source:           src,
		Project:          req.Project,
		Env:              marketplace.Environment{Engine: req.Spec.Engine, Loader: loader, GameVersion: mc},
		Version:          req.Version,
		Channels:         req.Channels,
		AllowPrerelease:  req.AllowPrerelease,
		AllowUnverified:  req.AllowUnverified,
		AllowMissingDeps: req.AllowMissingDeps,
		Installed:        c.installedRecords(req.Spec),
		VersionLimit:     req.VersionLimit,
	})
	if err != nil {
		return nil, err
	}
	if resolution == nil || !resolution.Complete {
		// Defensa en profundidad: Resolve nunca devuelve una resolución
		// incompleta sin error, pero si eso cambiara, aquí NO se descarga nada.
		return nil, errx.New(errx.CodeUsage,
			"la resolución del proyecto no es completa",
			"revisa el motor, la versión de Minecraft y las dependencias").
			WithDetail("reason", "resolucion_incompleta")
	}

	installed, sha512, err := c.stage(ctx, req, resolution)
	if err != nil {
		return nil, err
	}

	record := c.record(req, resolution, installed, sha512)
	if err := c.persist(req.Spec, record); err != nil {
		// El registro y el disco no deben quedar desincronizados: se retira el
		// archivo recién instalado (mismo criterio que 11.1).
		if rerr := os.Remove(installed.Path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			return nil, errx.Wrap(errx.CodeGeneral, err, "no se pudo guardar el registro de plugins y tampoco retirar "+
				installed.Name, "revisa permisos y borra el archivo a mano")
		}
		return nil, errx.Wrap(errx.CodeGeneral, err, "no se pudo guardar el registro de plugins; se retiró "+
			installed.Name+" para no dejar el registro y el disco desincronizados",
			"revisa permisos en la carpeta de la instancia").WithDetail("reason", ReasonRegistro)
	}

	warnings := append([]string{}, resolution.Warnings...)
	if resolution.Compat != marketplace.Compatible {
		warnings = append(warnings, "compatibilidad declarada: "+string(resolution.Compat))
	}
	return &Result{Resolution: resolution, Installed: *installed, Record: record, Warnings: warnings}, nil
}

// stage descarga a temporal, verifica checksum e integridad y mueve a destino.
// Devuelve el resultado de la instalación y el SHA-512 calculado (si hubo que
// verificar con ese algoritmo).
func (c *Client) stage(ctx context.Context, req Request, resolution *resolve.Resolution) (*plugins.Installed, string, error) {
	if c.Installer == nil {
		return nil, "", errx.New(errx.CodeGeneral, "no se configuró el instalador de la instancia", "")
	}
	policy := req.Download
	if strings.TrimSpace(policy.UserAgent) == "" {
		policy.UserAgent = engines.DefaultUserAgent()
	}

	dir := c.Installer.Dir(req.Spec)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", errx.Wrap(errx.CodeGeneral, err, "no se pudo preparar la carpeta "+dir, "revisa permisos")
	}
	if _, cerr := download.CleanStaleTemps(dir, time.Hour); cerr != nil {
		return nil, "", cerr
	}

	expectedSHA256 := ""
	if strings.EqualFold(resolution.Hash.Algorithm, marketplace.HashSHA256) {
		expectedSHA256 = resolution.Hash.Value
	}
	staged, err := download.StageURL(ctx, download.Request{
		URL:     resolution.File.URL,
		DestDir: dir,
		Name:    resolution.File.Name,
		SHA256:  expectedSHA256,
		Policy:  policy,
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = staged.Discard() }() // tras Commit no tiene efecto

	// Verificación con SHA-512 (Modrinth no publica sha256): se lee el temporal
	// ya acotado en tamaño y se comprueba el checksum publicado.
	sha512sum := ""
	if strings.EqualFold(resolution.Hash.Algorithm, marketplace.HashSHA512) {
		got, herr := hashFile(staged.Path(), marketplace.HashSHA512)
		if herr != nil {
			return nil, "", herr
		}
		sha512sum = got
		if !strings.EqualFold(got, resolution.Hash.Value) {
			return nil, "", checksumMismatch(marketplace.HashSHA512, resolution.Hash.Value, got)
		}
	}

	rep, err := archive.Validate(staged.Path(), staged.Bytes(), req.Archive)
	if err != nil {
		return nil, "", err
	}
	res, err := staged.Commit()
	if err != nil {
		return nil, "", err
	}
	return &plugins.Installed{
		Name:             res.Name,
		Path:             res.Path,
		Bytes:            res.Bytes,
		SHA256:           res.SHA256,
		Source:           res.Source,
		Archive:          rep,
		ChecksumVerified: expectedSHA256 != "" || sha512sum != "",
		Replaced:         res.Replaced,
		RenamedFrom:      res.RenamedFrom,
	}, sha512sum, nil
}

// record compone la entrada de procedencia.
func (c *Client) record(req Request, resolution *resolve.Resolution, installed *plugins.Installed, sha512sum string) plugins.Record {
	loader, mc := c.environment(req)
	rec := plugins.Record{
		Name:        installed.Name,
		Version:     resolution.Version.Number,
		SHA256:      installed.SHA256,
		SHA512:      sha512sum,
		SizeBytes:   installed.Bytes,
		Loader:      loader,
		GameVersion: mc,
		InstalledAt: plugins.Timestamp(c.now()),
		Origin: plugins.Origin{
			Source:      string(resolution.Source),
			ProjectID:   resolution.Project.ID,
			ProjectSlug: resolution.Project.Slug,
			ProjectName: resolution.Project.Title,
			VersionID:   resolution.Version.ID,
			URL:         safeURL(resolution.File.URL),
			Channel:     resolution.Version.Channel,
			Kind:        string(resolution.Project.Kind),
		},
	}
	return rec
}

// persist escribe la entrada en el registro de procedencia, sin duplicarla.
func (c *Client) persist(spec *instance.Spec, rec plugins.Record) error {
	if c.Provenance == nil {
		return nil
	}
	reg, err := c.Provenance.Load(spec)
	if err != nil {
		return err
	}
	reg.Upsert(rec)
	return c.Provenance.Save(spec, reg)
}

// installedRecords devuelve el registro de procedencia ya cruzado con la lista de
// plugins instalados de la instancia, para que el resolvedor evalúe dependencias
// con lo que realmente hay.
func (c *Client) installedRecords(spec *instance.Spec) []plugins.Record {
	if c.Provenance == nil {
		return nil
	}
	reg, err := c.Provenance.Load(spec)
	if err != nil {
		return nil
	}
	return reg.Installed(spec.Plugins)
}

// source busca el marketplace en el catálogo.
func (c *Client) source(id marketplace.SourceID) (marketplace.Source, error) {
	if c.Sources == nil {
		return nil, errx.New(errx.CodeUsage, "no hay ningún marketplace configurado",
			"configura Modrinth o Hangar").WithDetail("reason", ReasonFuente)
	}
	src, ok := c.Sources.Get(id)
	if !ok {
		labels := strings.Join(c.Sources.Labels(), ", ")
		if labels == "" {
			labels = "ninguno"
		}
		return nil, errx.New(errx.CodeUsage,
			"marketplace desconocido: "+string(id),
			"usa uno de los marketplaces disponibles: "+labels).
			WithDetail("reason", ReasonFuente).
			WithDetail("requested", string(id))
	}
	return src, nil
}

// environment resuelve el entorno efectivo: lo indicado o, si falta, los datos
// de la instancia.
func (c *Client) environment(req Request) (loader, gameVersion string) {
	loader = strings.TrimSpace(req.Loader)
	if loader == "" {
		loader = strings.ToLower(strings.TrimSpace(req.Spec.Engine))
	}
	gameVersion = strings.TrimSpace(req.GameVersion)
	if gameVersion == "" {
		gameVersion = strings.TrimSpace(req.Spec.Version)
	}
	return loader, gameVersion
}

// now devuelve el reloj del cliente.
func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// hashFile calcula el checksum de un archivo local con el algoritmo pedido.
func hashFile(path, algorithm string) (string, error) {
	var h hash.Hash
	switch strings.ToLower(algorithm) {
	case marketplace.HashSHA256:
		h = sha256.New()
	case marketplace.HashSHA512:
		h = sha512.New()
	default:
		return "", errx.New(errx.CodeUsage, "algoritmo de checksum no soportado: "+algorithm,
			"usa sha256 o sha512").WithDetail("reason", "checksum_no_soportado")
	}
	f, err := os.Open(path) // #nosec G304 -- temporal creado por nosotros
	if err != nil {
		return "", errx.Wrap(errx.CodeGeneral, err, "no se pudo leer el artefacto temporal", "")
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(h, f); err != nil {
		return "", errx.Wrap(errx.CodeGeneral, err, "no se pudo calcular el checksum del artefacto", "")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checksumMismatch construye el error de integridad.
func checksumMismatch(algorithm, expected, got string) error {
	return errx.New(errx.CodeGeneral,
		"el checksum "+strings.ToUpper(algorithm)+" no coincide: se esperaba "+expected+" y se obtuvo "+got,
		"no se instala nada; comprueba el checksum publicado por el origen").
		WithDetail("reason", "checksum_no_coincide").
		WithDetail("algorithm", algorithm).
		WithDetail("expected", expected).
		WithDetail("got", got)
}

// safeURL quita credenciales y parámetros de una URL antes de guardarla en el
// registro: nunca se persisten tokens ni URLs con secretos.
func safeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
