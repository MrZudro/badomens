package dockerx

import (
	"embed"
	"fmt"
)

// scriptsFS embebe los scripts de respaldo independientes (mismos pasos que el
// plan en Go, sin dependencias).
//
//go:embed scripts/*.sh scripts/*.ps1
var scriptsFS embed.FS

// Script devuelve el script de respaldo para la plataforma ("windows" → .ps1,
// resto → .sh). Sirve a `badomens docker ensure --print-script`.
func Script(platform string) (string, error) {
	name := "scripts/ensure-docker.sh"
	if platform == "windows" {
		name = "scripts/ensure-docker.ps1"
	}
	raw, err := scriptsFS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("no se pudo leer el script %s: %w", name, err)
	}
	return string(raw), nil
}
