package uninstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRunConservaDatosPorDefecto(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("repo: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "badomens.exe")
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Paths{ConfigDir: dir, ConfigFile: cfgFile, DataDir: dataDir}, exe, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfgFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("la configuración debe borrarse; existe: %v", err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("los datos deben conservarse por defecto: %v", err)
	}
	if len(res.Kept) == 0 || !contains(res.Kept, "datos conservados") {
		t.Fatalf("debe reportar datos conservados: %v", res.Kept)
	}
}

func TestRunPurgeBorraDatos(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("x: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "instances"), 0o700); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Paths{ConfigDir: dir, ConfigFile: cfgFile, DataDir: dataDir}, "", nil, Options{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("los datos deben borrarse con --purge: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("la carpeta de config debe borrarse con --purge: %v", err)
	}
	_ = res
}

func TestRunStopAllFallidoEsAviso(t *testing.T) {
	dir := t.TempDir()
	res, err := Run(context.Background(), Paths{ConfigDir: dir, ConfigFile: "", DataDir: ""}, "", func(ctx context.Context) error {
		return errors.New("docker no disponible")
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("un fallo al detener las instancias debe ser un aviso, no un error")
	}
}

func TestPlanListaPasos(t *testing.T) {
	steps := Plan(Paths{ConfigDir: "c", ConfigFile: "c/config.yaml", DataDir: "c/data"}, Options{})
	if len(steps) < 3 {
		t.Fatalf("el plan debe listar al menos 3 pasos, tengo %v", steps)
	}
}

func contains(list []string, prefix string) bool {
	for _, s := range list {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
