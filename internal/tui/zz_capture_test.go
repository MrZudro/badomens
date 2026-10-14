package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/dockerx"
	"github.com/MrZudro/badomens/internal/host"
	"github.com/MrZudro/badomens/internal/tui/theme"
)

// TestZZWriteGFrames es temporal (Fase G). Se elimina antes de commitear.
func TestZZWriteGFrames(t *testing.T) {
	dir := os.Getenv("CAP_DIR")
	if dir == "" {
		t.Skip("CAP_DIR no definido")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, a *App) {
		a.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stripANSI(a.View())+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("health_110x30.txt", gHealth())
	write("help_110x30.txt", gHelp())
	write("palette_110x30.txt", gPalette())
	write("modal_delete_110x30.txt", gDeleteModal())
	write("modal_quit_110x30.txt", gQuitModal())

	ascii := NewApp(goldenSvc(), AppOptions{Theme: theme.Dark, ASCII: true})
	ascii.router.Push(newHealthScreen(ascii.svc, ascii.pal, ascii.ascii))
	if h, ok := ascii.router.Top().(*healthScreen); ok {
		h.docker = dockerx.Diagnosis{State: dockerx.StateOK, Message: "Docker está listo.", ComposeVersion: "v2.30.1"}
		h.capacity = host.Capacity{NCPU: 8, MemTotalBytes: 16 << 30, Source: "runtime"}
		h.ip, h.magic = "100.64.0.1", "maquina.tail1234.ts.net"
		h.freeGB, h.totalGB, h.diskOK = 120, 500, true
		h.portsOK = true
		h.checkedUp, h.latest, h.hasUpdate = true, "1.0.1", false
	}
	write("health_ascii_110x30.txt", ascii)
}
