package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CheckTTL es el intervalo mínimo entre comprobaciones automáticas de
// actualización (evita llamar a GitHub en cada arranque de la TUI).
const CheckTTL = 12 * time.Hour

// CacheFile es el nombre del archivo de caché dentro del directorio de
// configuración.
const CacheFile = "update-check.json"

// Cache guarda el estado del chequeo de actualizaciones para no volver a
// preguntarle a GitHub en cada arranque ni repetir una versión descartada.
type Cache struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest"`
	// Skipped es la última versión que el operador decidió no instalar.
	Skipped string `json:"skipped_version"`
}

// LoadCache lee la caché del directorio dado; si no existe devuelve una vacía
// sin error.
func LoadCache(dir string) Cache {
	var c Cache
	raw, err := os.ReadFile(filepath.Join(dir, CacheFile)) // #nosec G304 -- ruta construida desde el dir de configuración (config.Dir)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(raw, &c)
	return c
}

// SaveCache escribe la caché en el directorio dado (que debe existir).
func SaveCache(dir string, c Cache) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, CacheFile), raw, 0o600)
}

// Due indica si ha pasado ttl desde el último chequeo (o no hay caché aún).
func (c Cache) Due(now time.Time, ttl time.Duration) bool {
	if c.LastCheck.IsZero() {
		return true
	}
	return now.Sub(c.LastCheck) >= ttl
}
