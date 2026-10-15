package install

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrZudro/badomens/internal/engines"
	"github.com/MrZudro/badomens/internal/errx"
	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/plugins"
	"github.com/MrZudro/badomens/internal/plugins/download"
	"github.com/MrZudro/badomens/internal/plugins/marketplace"
	"github.com/MrZudro/badomens/internal/plugins/marketplace/modrinth"
)

// --- Dobles del esquema de Modrinth (el parser real es el de modrinth) ------

type wireFile struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

type wireDep struct {
	ProjectID      *string `json:"project_id"`
	VersionID      *string `json:"version_id"`
	FileName       *string `json:"file_name"`
	DependencyType string  `json:"dependency_type"`
}

type wireVersion struct {
	GameVersions  []string   `json:"game_versions"`
	Loaders       []string   `json:"loaders"`
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	Name          string     `json:"name"`
	VersionNumber string     `json:"version_number"`
	DatePublished string     `json:"date_published"`
	VersionType   string     `json:"version_type"`
	Status        string     `json:"status"`
	Files         []wireFile `json:"files"`
	Dependencies  []wireDep  `json:"dependencies"`
}

type wireProject struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	ProjectType  string   `json:"project_type"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions"`
}

// harness reúne un marketplace simulado, un servidor de artefactos y el cliente
// de instalación sobre un data dir temporal.
type harness struct {
	client  *Client
	spec    *instance.Spec
	dataDir string
	jar     []byte
	sha256  string
	sha512  string
	// versions es la respuesta viva de /project/demo/version.
	versions []wireVersion
	// badChecksum sirve un checksum que no corresponde.
	badChecksum bool
	// privateURL hace que el artefacto apunte a una IP privada.
	privateURL bool
	// artifactStatus fuerza un estado en la descarga del artefacto.
	artifactStatus int
	// artifactHits cuenta las descargas reales del artefacto.
	artifactHits int
}

// jarMinimo construye un JAR válido.
func jarMinimo(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, data string }{
		{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"},
		{"plugin.yml", "name: Demo\nversion: 1.0\n"},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// newHarness monta el entorno completo.
func newHarness(t *testing.T, versions ...wireVersion) *harness {
	t.Helper()
	jar := jarMinimo(t)
	sum256 := sha256.Sum256(jar)
	sum512 := sha512.Sum512(jar)
	h := &harness{
		spec:     &instance.Spec{Slug: "demo", Engine: "paper", Version: "1.21.1"},
		dataDir:  t.TempDir(),
		jar:      jar,
		sha256:   hex.EncodeToString(sum256[:]),
		sha512:   hex.EncodeToString(sum512[:]),
		versions: versions,
	}

	// Servidor del artefacto.
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.artifactStatus != 0 {
			http.Error(w, "boom", h.artifactStatus)
			return
		}
		h.artifactHits++
		w.Header().Set("Content-Type", "application/java-archive")
		_, _ = w.Write(h.jar)
	}))
	t.Cleanup(artifact.Close)

	// Servidor del marketplace.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/project/demo":
			kind := "plugin"
			if strings.HasPrefix(h.spec.Engine, "modpack") {
				kind = "modpack"
			}
			_ = json.NewEncoder(w).Encode(wireProject{
				ID: "P1OZGk5p", Slug: "demo", ProjectType: kind, Title: "Demo",
				Status: "approved", Loaders: []string{"paper"}, GameVersions: []string{"1.21.1"},
			})
		case "/project/demo/version", "/project/P1OZGk5p/version":
			// El artefacto se materializa con la URL real del servidor de arriba.
			payload := make([]wireVersion, len(h.versions))
			copy(payload, h.versions)
			for i := range payload {
				for j := range payload[i].Files {
					payload[i].Files[j].URL = artifact.URL + "/" + payload[i].Files[j].Filename
					if h.badChecksum {
						payload[i].Files[j].Hashes["sha512"] = strings.Repeat("0", 128)
					}
					if h.privateURL {
						payload[i].Files[j].URL = "https://10.0.0.1/evil.jar"
					}
				}
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)

	httpClient := engines.DefaultClient()
	httpClient.MaxAttempts = 1
	mod := modrinth.New(httpClient)
	mod.BaseURL = api.URL

	catalog := marketplace.NewCatalog()
	catalog.Register(mod)

	h.client = New(catalog, &plugins.Installer{DataDir: h.dataDir}, plugins.NewProvenance(h.dataDir))
	return h
}

// versionOf construye una versión publicada de Modrinth con el artefacto dado.
func (h *harness) versionOf(number, date, channel string, hashes map[string]string, deps ...wireDep) wireVersion {
	return wireVersion{
		GameVersions: []string{"1.21.1"}, Loaders: []string{"paper"},
		ID: "id-" + number, ProjectID: "P1OZGk5p", Name: "Demo " + number,
		VersionNumber: number, DatePublished: date, VersionType: channel, Status: "listed",
		Files: []wireFile{{
			Hashes: hashes, Filename: "demo-" + number + ".jar", Primary: true, Size: int64(len(h.jar)),
		}},
		Dependencies: deps,
	}
}

// testPolicy es la política de las pruebas: httptest sirve por http en 127.0.0.1,
// así que se habilitan loopback y http EXPLÍCITAMENTE. Los casos que comprueban
// la política por defecto usan download.DefaultPolicy().
func testPolicy() download.Policy {
	p := download.DefaultPolicy()
	p.AllowHTTP = true
	p.AllowLoopback = true
	return p
}

// request arma una petición de instalación estándar.
func (h *harness) request() Request {
	return Request{
		Spec:         h.spec,
		Source:       marketplace.SourceModrinth,
		Project:      "demo",
		Download:     testPolicy(),
		VersionLimit: 20,
	}
}

// pluginDir es la carpeta destino.
func (h *harness) pluginDir() string { return filepath.Join(h.dataDir, "instances", "demo", "plugins") }

// provenancePath es el archivo del registro de procedencia.
func (h *harness) provenancePath() string {
	return filepath.Join(h.dataDir, "instances", "demo", plugins.ProvenanceFile)
}

// readRegister lee el registro de procedencia del disco.
func (h *harness) readRegister(t *testing.T) *plugins.Register {
	t.Helper()
	reg, err := plugins.NewProvenance(h.dataDir).Load(h.spec)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return reg
}

// assertClean comprueba que no queda ni archivo instalado ni registro ni
// temporales.
func (h *harness) assertClean(t *testing.T) {
	t.Helper()
	if entries, err := os.ReadDir(h.pluginDir()); err == nil {
		for _, e := range entries {
			t.Errorf("quedó un archivo tras el fallo: %s", e.Name())
		}
	}
	if reg := h.readRegister(t); reg.Len() != 0 {
		t.Errorf("quedó registro tras el fallo: %+v", reg.Records)
	}
	if _, err := os.Stat(h.provenancePath()); err == nil {
		t.Error("no debería haberse creado el registro de procedencia")
	}
}

// TestInstallCompleto: resuelve, descarga, verifica el sha512 publicado (Modrinth
// no publica sha256), mueve atómicamente y registra procedencia, versión y
// checksum.
func TestInstallCompleto(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512, "sha1": strings.Repeat("a", 40)})}

	res, err := h.client.Install(t.Context(), h.request())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Installed.Name != "demo-5.5.0.jar" {
		t.Errorf("nombre instalado = %q", res.Installed.Name)
	}
	if !res.Installed.ChecksumVerified {
		t.Error("el checksum publicado se verificó: ChecksumVerified debe ser true")
	}
	if res.Installed.SHA256 != h.sha256 {
		t.Errorf("sha256 calculado = %q, quiero %q", res.Installed.SHA256, h.sha256)
	}
	if res.Record.SHA512 != h.sha512 {
		t.Errorf("sha512 registrado = %q", res.Record.SHA512)
	}
	if res.Record.Version != "5.5.0" {
		t.Errorf("versión registrada = %q", res.Record.Version)
	}
	if res.Record.Origin.Source != "modrinth" || res.Record.Origin.ProjectSlug != "demo" {
		t.Errorf("procedencia = %+v", res.Record.Origin)
	}
	if res.Record.Origin.VersionID != "id-5.5.0" {
		t.Errorf("VersionID = %q", res.Record.Origin.VersionID)
	}
	if res.Record.TargetVersion != "" {
		t.Errorf("una instalación no debe dejar versión objetivo: %q", res.Record.TargetVersion)
	}
	if res.Record.Origin.URL == "" || strings.Contains(res.Record.Origin.URL, "?") {
		t.Errorf("URL de origen saneada = %q", res.Record.Origin.URL)
	}
	if res.Record.InstalledAt == "" {
		t.Error("falta el instante de instalación")
	}

	// El archivo está en su sitio y NO quedan temporales.
	path := filepath.Join(h.pluginDir(), "demo-5.5.0.jar")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("el artefacto no está instalado: %v", err)
	}
	entries, err := os.ReadDir(h.pluginDir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("archivos en destino = %d, quiero 1", len(entries))
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".badomens-") {
			t.Errorf("quedó un temporal: %s", e.Name())
		}
	}
	// Registro con una única entrada.
	reg := h.readRegister(t)
	if reg.Len() != 1 || reg.Records[0].Name != "demo-5.5.0.jar" {
		t.Errorf("registro = %+v", reg.Records)
	}
}

// TestInstallChecksumNoCoincide: un checksum que no cuadra no deja NADA (ni
// archivo, ni registro, ni temporales).
func TestInstallChecksumNoCoincide(t *testing.T) {
	h := newHarness(t)
	h.badChecksum = true
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512})}

	_, err := h.client.Install(t.Context(), h.request())
	if err == nil {
		t.Fatal("se esperaba error de checksum")
	}
	if d := errx.Details(err); d["reason"] != "checksum_no_coincide" {
		t.Errorf("reason = %v (%v)", d["reason"], err)
	}
	h.assertClean(t)
}

// TestInstall404SinRastro: un 404 en la descarga del artefacto deja el registro y
// el disco limpios.
func TestInstall404SinRastro(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512})}
	h.artifactStatus = http.StatusNotFound

	_, err := h.client.Install(t.Context(), h.request())
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if d := errx.Details(err); d["http_status"] != http.StatusNotFound || d["reason"] != "http_error" {
		t.Errorf("detalles = %v (%v)", d, err)
	}
	h.assertClean(t)
}

// TestInstall500SinRastro: un 5xx tampoco deja rastro.
func TestInstall500SinRastro(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512})}
	h.artifactStatus = http.StatusInternalServerError

	_, err := h.client.Install(t.Context(), h.request())
	if err == nil {
		t.Fatal("se esperaba error")
	}
	if !errx.Is(err, errx.CodeUnavailable) {
		t.Errorf("código = %v", err)
	}
	h.assertClean(t)
}

// TestInstallRechazaIPPrivada: la política de la fase 11.1 sigue aplicándose a los
// artefactos que publica el marketplace: una URL a una IP privada se rechaza.
func TestInstallRechazaIPPrivada(t *testing.T) {
	h := newHarness(t)
	h.privateURL = true
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512})}

	req := h.request()
	req.Download = download.DefaultPolicy() // sin excepciones: la política de 11.1
	_, err := h.client.Install(t.Context(), req)
	if err == nil {
		t.Fatal("se esperaba rechazo del destino privado")
	}
	if d := errx.Details(err); d["reason"] != "destino_no_permitido" {
		t.Errorf("reason = %v (%v)", d["reason"], err)
	}
	if !errx.Is(err, errx.CodeUsage) {
		t.Errorf("código = %v", err)
	}
	h.assertClean(t)
}

// TestInstallRechazaHTTP: sin --allow-http un artefacto por http:// se rechaza.
func TestInstallRechazaHTTP(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512})}

	req := h.request()
	req.Download = download.DefaultPolicy()
	_, err := h.client.Install(t.Context(), req)
	if err == nil {
		t.Fatal("se esperaba rechazo de http sin cifrado")
	}
	if d := errx.Details(err); d["reason"] != "tls_requerido" {
		t.Errorf("reason = %v (%v)", d["reason"], err)
	}
	h.assertClean(t)
}

// TestInstallActualizaElRegistroUnaSolaVez: reinstalar el mismo artefacto deja
// una única entrada, con los datos de la última instalación.
func TestInstallActualizaElRegistroUnaSolaVez(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512})}

	req := h.request()
	if _, err := h.client.Install(t.Context(), req); err != nil {
		t.Fatalf("primera instalación: %v", err)
	}
	req.Download.Conflict = download.ConflictReplace
	if _, err := h.client.Install(t.Context(), req); err != nil {
		t.Fatalf("segunda instalación: %v", err)
	}
	reg := h.readRegister(t)
	if reg.Len() != 1 {
		t.Fatalf("entradas = %d, quiero 1 (nunca duplicados)", reg.Len())
	}
	if reg.Records[0].Version != "5.5.0" || reg.Records[0].SHA512 != h.sha512 {
		t.Errorf("entrada = %+v", reg.Records[0])
	}
}

// TestInstallSinChecksumPublicado: Modrinth publica sha512; si un proveedor no
// publica checksum utilizable, la instalación FALLA salvo permiso explícito.
func TestInstallSinChecksumPublicado(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha1": strings.Repeat("a", 40)})}

	_, err := h.client.Install(t.Context(), h.request())
	if err == nil {
		t.Fatal("se esperaba error por falta de checksum")
	}
	if d := errx.Details(err); d["reason"] != "checksum_ausente" {
		t.Errorf("reason = %v (%v)", d["reason"], err)
	}
	h.assertClean(t)

	req := h.request()
	req.AllowUnverified = true
	res, err := h.client.Install(t.Context(), req)
	if err != nil {
		t.Fatalf("con AllowUnverified: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("debe avisar de que no hay verificación disponible")
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "SIN verificación") {
		t.Errorf("avisos = %v", res.Warnings)
	}
	if res.Record.SHA512 != "" {
		t.Errorf("no debe fingir un checksum publicado: %q", res.Record.SHA512)
	}
	if res.Record.SHA256 != h.sha256 {
		t.Errorf("el sha256 calculado siempre se registra: %q", res.Record.SHA256)
	}
}

// TestInstallDependenciaObligatoria: no se resuelve como exitoso si falta una
// dependencia obligatoria, y nunca se instala sola.
func TestInstallDependenciaObligatoria(t *testing.T) {
	h := newHarness(t)
	projectID := "YlKdE5VK"
	fileName := "ViaBackwards"
	dep := wireDep{DependencyType: "required", ProjectID: &projectID, FileName: &fileName}
	version := h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512}, dep)
	h.versions = []wireVersion{version}

	_, err := h.client.Install(t.Context(), h.request())
	if err == nil {
		t.Fatal("se esperaba error por dependencia ausente")
	}
	if d := errx.Details(err); d["reason"] != "dependencia_no_satisfecha" {
		t.Errorf("reason = %v (%v)", d["reason"], err)
	}
	if h.artifactHits != 0 {
		t.Errorf("no debía descargarse nada, descargas = %d", h.artifactHits)
	}
	h.assertClean(t)

	req := h.request()
	req.AllowMissingDeps = true
	res, err := h.client.Install(t.Context(), req)
	if err != nil {
		t.Fatalf("con AllowMissingDeps: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("debe advertir de la dependencia ausente")
	}
	// Solo se instaló el proyecto pedido: las dependencias no se instalan solas.
	entries, _ := os.ReadDir(h.pluginDir())
	if len(entries) != 1 {
		t.Errorf("archivos instalados = %d, quiero 1", len(entries))
	}
}

// TestInstallTipoNoInstalable: un modpack no entra en un servidor de plugins.
func TestInstallTipoNoInstalable(t *testing.T) {
	h := newHarness(t)
	h.spec.Engine = "modpack"
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release",
		map[string]string{"sha512": h.sha512})}

	_, err := h.client.Install(t.Context(), h.request())
	if err == nil {
		t.Fatal("se esperaba error de tipo")
	}
	if d := errx.Details(err); d["reason"] != "tipo_no_instalable" {
		t.Errorf("reason = %v (%v)", d["reason"], err)
	}
	if h.artifactHits != 0 {
		t.Errorf("no debía descargarse nada, descargas = %d", h.artifactHits)
	}
}

// TestInstallMarketplaceDesconocido y vanilla.
func TestInstallValidacionesPrevias(t *testing.T) {
	h := newHarness(t)
	req := h.request()
	req.Source = "curseforge"
	_, err := h.client.Install(t.Context(), req)
	if err == nil || errx.Details(err)["reason"] != ReasonFuente {
		t.Errorf("marketplace desconocido: %v", err)
	}

	req = h.request()
	req.Spec = &instance.Spec{Slug: "v", Engine: "vanilla", Version: "1.21.1"}
	if _, err := h.client.Install(t.Context(), req); !errx.Is(err, errx.CodeUsage) {
		t.Errorf("vanilla: %v", err)
	}

	req = h.request()
	req.Spec = nil
	if _, err := h.client.Install(t.Context(), req); !errx.Is(err, errx.CodeUsage) {
		t.Errorf("sin instancia: %v", err)
	}
}

// --- 11.3 Comprobación de actualizaciones ----------------------------------

// TestCheckUpdatesDetectaSinInstalar: detecta la versión nueva, NO la instala y
// no toca el archivo ni el registro.
func TestCheckUpdatesDetectaSinInstalar(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{
		h.versionOf("5.4.0", "2025-01-01T00:00:00Z", "release", map[string]string{"sha512": h.sha512}),
		h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release", map[string]string{"sha512": h.sha512}),
	}
	req := h.request()
	req.Version = "5.4.0"
	if _, err := h.client.Install(t.Context(), req); err != nil {
		t.Fatalf("Install: %v", err)
	}
	path := filepath.Join(h.pluginDir(), "demo-5.4.0.jar")
	before := fileDigest(t, path)

	// Aparece una versión más reciente.
	h.versions = append(h.versions,
		h.versionOf("5.6.0", "2025-09-01T00:00:00Z", "release", map[string]string{"sha512": h.sha512}))

	checks, err := h.client.CheckUpdates(t.Context(), UpdateRequest{Spec: h.spec})
	if err != nil {
		t.Fatalf("CheckUpdates: %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("comprobaciones = %d", len(checks))
	}
	check := checks[0]
	if check.Status != UpdateAvailable {
		t.Errorf("estado = %q (%s)", check.Status, check.Detail)
	}
	if check.TargetVersion != "5.6.0" {
		t.Errorf("versión objetivo = %q", check.TargetVersion)
	}
	if check.Version != "5.4.0" {
		t.Errorf("versión instalada = %q", check.Version)
	}
	if check.Compatibility != marketplace.Compatible {
		t.Errorf("compatibilidad = %q", check.Compatibility)
	}
	if !strings.Contains(check.Detail, "no se instala automáticamente") {
		t.Errorf("detalle = %q", check.Detail)
	}

	// El archivo instalado no cambió y no apareció ninguna versión nueva.
	if after := fileDigest(t, path); after != before {
		t.Error("la comprobación modificó el archivo instalado")
	}
	entries, _ := os.ReadDir(h.pluginDir())
	if len(entries) != 1 || entries[0].Name() != "demo-5.4.0.jar" {
		t.Errorf("el directorio cambió: %v", entries)
	}
	reg := h.readRegister(t)
	if reg.Records[0].Version != "5.4.0" || reg.Records[0].TargetVersion != "" {
		t.Errorf("sin PersistTargets el registro no debe cambiar: %+v", reg.Records[0])
	}
}

// TestCheckUpdatesAlDia: si la instalada es la más reciente, se informa.
func TestCheckUpdatesAlDia(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release", map[string]string{"sha512": h.sha512})}
	if _, err := h.client.Install(t.Context(), h.request()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	checks, err := h.client.CheckUpdates(t.Context(), UpdateRequest{Spec: h.spec})
	if err != nil {
		t.Fatalf("CheckUpdates: %v", err)
	}
	if checks[0].Status != UpdateCurrent {
		t.Errorf("estado = %q (%s)", checks[0].Status, checks[0].Detail)
	}
	if checks[0].TargetVersion != "" {
		t.Errorf("no hay versión nueva: %q", checks[0].TargetVersion)
	}
}

// TestCheckUpdatesPersisteLaVersionObjetivo: registrar la versión objetivo es un
// dato del registro, no una instalación.
func TestCheckUpdatesPersisteLaVersionObjetivo(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release", map[string]string{"sha512": h.sha512})}
	req := h.request()
	req.Version = "5.5.0"
	if _, err := h.client.Install(t.Context(), req); err != nil {
		t.Fatalf("Install: %v", err)
	}
	path := filepath.Join(h.pluginDir(), "demo-5.5.0.jar")
	before := fileDigest(t, path)

	h.versions = append(h.versions, h.versionOf("5.6.0", "2025-09-01T00:00:00Z", "release", map[string]string{"sha512": h.sha512}))
	if _, err := h.client.CheckUpdates(t.Context(), UpdateRequest{Spec: h.spec, PersistTargets: true}); err != nil {
		t.Fatalf("CheckUpdates: %v", err)
	}
	reg := h.readRegister(t)
	if reg.Records[0].Version != "5.5.0" {
		t.Errorf("la versión instalada no debe cambiar: %+v", reg.Records[0])
	}
	if reg.Records[0].TargetVersion != "5.6.0" {
		t.Errorf("versión objetivo = %q", reg.Records[0].TargetVersion)
	}
	if after := fileDigest(t, path); after != before {
		t.Error("el archivo instalado no debe cambiar")
	}
}

// TestCheckUpdatesSinProcedencia: un registro antiguo (solo el nombre) no permite
// saber si hay versión nueva; se dice, no se inventa.
func TestCheckUpdatesSinProcedencia(t *testing.T) {
	h := newHarness(t)
	h.spec.Plugins = []string{"antiguo-1.0.jar"}
	checks, err := h.client.CheckUpdates(t.Context(), UpdateRequest{Spec: h.spec})
	if err != nil {
		t.Fatalf("CheckUpdates: %v", err)
	}
	if len(checks) != 1 || checks[0].Status != UpdateUnknown {
		t.Fatalf("comprobaciones = %+v", checks)
	}
	if checks[0].Name != "antiguo-1.0.jar" || !strings.Contains(checks[0].Detail, "procedencia") {
		t.Errorf("detalle = %+v", checks[0])
	}
	if h.artifactHits != 0 {
		t.Error("no debía descargarse nada")
	}
}

// TestCheckUpdatesBloqueadaSiElProveedorFalla: el fallo del catálogo se reporta
// por elemento, sin romper la llamada.
func TestCheckUpdatesBloqueadaSiElProveedorFalla(t *testing.T) {
	h := newHarness(t)
	h.versions = []wireVersion{h.versionOf("5.5.0", "2025-06-01T00:00:00Z", "release", map[string]string{"sha512": h.sha512})}
	if _, err := h.client.Install(t.Context(), h.request()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// El proyecto deja de existir en el marketplace.
	h.versions = nil
	checks, err := h.client.CheckUpdates(t.Context(), UpdateRequest{Spec: h.spec})
	if err != nil {
		t.Fatalf("CheckUpdates: %v", err)
	}
	if checks[0].Status != UpdateBlocked {
		t.Errorf("estado = %q", checks[0].Status)
	}
	if checks[0].Reason != "sin_version_compatible" {
		t.Errorf("reason = %q", checks[0].Reason)
	}
}

// TestCheckUpdatesSinInstancia: error de uso.
func TestCheckUpdatesSinInstancia(t *testing.T) {
	h := newHarness(t)
	if _, err := h.client.CheckUpdates(t.Context(), UpdateRequest{}); !errx.Is(err, errx.CodeUsage) {
		t.Errorf("se esperaba error_usage, got %v", err)
	}
}

// TestSafeURLQuitaSecretos: nunca se persisten credenciales ni parámetros.
func TestSafeURLQuitaSecretos(t *testing.T) {
	got := safeURL("https://user:token@cdn.example/demo.jar?X-Amz-Signature=abc#frag")
	if strings.Contains(got, "token") || strings.Contains(got, "X-Amz") || strings.Contains(got, "#") {
		t.Errorf("URL sin sanear: %q", got)
	}
	if got != "https://cdn.example/demo.jar" {
		t.Errorf("URL saneada = %q", got)
	}
	if safeURL("") != "" {
		t.Error("una URL vacía no debe producir basura")
	}
}

// fileDigest resume un archivo para comparar antes/después.
func fileDigest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- ruta de artefacto dentro del data dir de prueba
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	sum := sha256.Sum256(raw)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	return hex.EncodeToString(sum[:]) + "|" + info.ModTime().String() + "|" + string(rune('0'+info.Size()%10))
}
