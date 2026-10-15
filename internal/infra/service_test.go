package infra

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonServiceSystemd(t *testing.T) {
	user, err := DaemonService("linux", "/home/ana", "/opt/badomens", ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if user.Kind != KindSystemd {
		t.Fatalf("quiero systemd, tengo %s", user.Kind)
	}
	if !strings.HasSuffix(user.Path, filepath.Join(".config", "systemd", "user", "badomens-idle.service")) {
		t.Fatalf("ruta inesperada: %s", user.Path)
	}
	if !strings.Contains(user.Unit, "ExecStart=/opt/badomens idle daemon") {
		t.Fatalf("unidad sin ExecStart correcto:\n%s", user.Unit)
	}
	if !strings.Contains(user.Unit, "WantedBy=default.target") {
		t.Fatalf("unidad de usuario debe querer default.target:\n%s", user.Unit)
	}
	if user.RequiresElevation {
		t.Fatal("la unidad de usuario no debe pedir elevación")
	}

	sys, err := DaemonService("linux", "/home/ana", "/opt/badomens", ScopeSystem)
	if err != nil {
		t.Fatal(err)
	}
	if sys.Path != "/etc/systemd/system/badomens-idle.service" {
		t.Fatalf("ruta de sistema inesperada: %s", sys.Path)
	}
	if !sys.RequiresElevation {
		t.Fatal("la unidad de sistema debe marcar elevación")
	}
	if !strings.Contains(sys.Unit, "WantedBy=multi-user.target") {
		t.Fatalf("unidad de sistema debe querer multi-user.target:\n%s", sys.Unit)
	}
}

func TestDaemonServiceSystemdQuotesExeWithSpaces(t *testing.T) {
	spec, err := DaemonService("linux", "/home/ana", "/opt/My Apps/badomens", ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec.Unit, `ExecStart="/opt/My Apps/badomens" idle daemon`) {
		t.Fatalf("la ruta con espacios debe citarse:\n%s", spec.Unit)
	}
	// Las comillas interiores se escapan.
	if got := quoteSystemd(`C:\a b\c"d`); got != `"C:\\a b\\c\"d"` {
		t.Fatalf("quoteSystemd: %q", got)
	}
	if got := quoteSystemd("/opt/badomens"); got != "/opt/badomens" {
		t.Fatalf("sin espacios no debe citar: %q", got)
	}
}

func TestDaemonServiceLaunchd(t *testing.T) {
	user, err := DaemonService("darwin", "/Users/ana", "/opt/badomens", ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if user.Kind != KindLaunchd {
		t.Fatalf("quiero launchd, tengo %s", user.Kind)
	}
	if !strings.HasSuffix(user.Path, filepath.Join("Library", "LaunchAgents", "com.badomens-idle.plist")) {
		t.Fatalf("ruta LaunchAgent inesperada: %s", user.Path)
	}
	// Cada argumento va en su propio <string>, sin concatenación.
	if !strings.Contains(user.Unit, "<string>/opt/badomens</string>") ||
		!strings.Contains(user.Unit, "<string>idle</string>") ||
		!strings.Contains(user.Unit, "<string>daemon</string>") {
		t.Fatalf("ProgramArguments mal formados:\n%s", user.Unit)
	}
	if user.RequiresElevation {
		t.Fatal("LaunchAgent no debe pedir elevación")
	}

	sys, err := DaemonService("darwin", "/Users/ana", "/opt/badomens", ScopeSystem)
	if err != nil {
		t.Fatal(err)
	}
	if sys.Path != "/Library/LaunchDaemons/com.badomens-idle.plist" {
		t.Fatalf("ruta LaunchDaemon inesperada: %s", sys.Path)
	}
	if !sys.RequiresElevation {
		t.Fatal("LaunchDaemon debe marcar elevación")
	}
}

func TestDaemonServiceLaunchdEscapesXml(t *testing.T) {
	spec, err := DaemonService("darwin", "/Users/ana", "/opt/a&b/badomens", ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec.Unit, "<string>/opt/a&amp;b/badomens</string>") {
		t.Fatalf("el plist debe escapar XML:\n%s", spec.Unit)
	}
	if got := xmlEscape(`a<b>&"'`); got != "a&lt;b&gt;&amp;&quot;&apos;" {
		t.Fatalf("xmlEscape: %q", got)
	}
}

func TestDaemonServiceWindows(t *testing.T) {
	spec, err := DaemonService("windows", `C:\Users\ana`, `C:\Program Files\badomens\badomens.exe`, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != KindWinTask || spec.Path != "" {
		t.Fatalf("quiero tarea programada sin archivo: %+v", spec)
	}
	tr := ""
	for i, a := range spec.InstallArgs {
		if a == "/TR" && i+1 < len(spec.InstallArgs) {
			tr = spec.InstallArgs[i+1]
		}
	}
	if tr == "" {
		t.Fatalf("faltan los argumentos /TR: %v", spec.InstallArgs)
	}
	if tr != windowsCommandLine(`C:\Program Files\badomens\badomens.exe`, "idle", "daemon") {
		t.Fatalf("línea de la tarea inesperada: %q", tr)
	}
	if !strings.HasPrefix(tr, `"C:\Program Files\badomens\badomens.exe"`) {
		t.Fatalf("la ruta con espacios debe citarse: %q", tr)
	}
	// La tarea al iniciar sesión no exige elevación.
	if spec.RequiresElevation {
		t.Fatal("la tarea ONLOGON no debe pedir elevación")
	}
	if got := strings.Join(spec.InstallArgs[:6], " "); got != "/Create /SC ONLOGON /TN badomens-idle /TR" {
		t.Fatalf("argumentos de schtasks inesperados: %q", got)
	}
}

func TestDaemonServiceUnsupportedOS(t *testing.T) {
	if _, err := DaemonService("plan9", "/home", "/x", ScopeUser); err == nil {
		t.Fatal("quiero error para SO no soportado")
	}
}

func TestQuoteWindows(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `""`},
		{"simple", "simple"},
		{`C:\Program Files\x.exe`, `"C:\Program Files\x.exe"`},
		{"a b", `"a b"`},
		{`a\`, `a\`},
		{`a b\`, `"a b\\"`},
	}
	for _, tc := range cases {
		if got := quoteWindows(tc.in); got != tc.want {
			t.Fatalf("quoteWindows(%q): quiero %q, tengo %q", tc.in, tc.want, got)
		}
	}
}

func TestStartDaemonServiceArgs(t *testing.T) {
	cases := []struct {
		goos, home string
		scope      ServiceScope
		want       string
	}{
		{"linux", "/home/ana", ScopeUser, "systemctl --user start badomens-idle.service"},
		{"linux", "/home/ana", ScopeSystem, "systemctl start badomens-idle.service"},
		{"darwin", "/Users/ana", ScopeUser, "launchctl start com.badomens-idle"},
		{"darwin", "/Users/ana", ScopeSystem, "sudo -n launchctl start com.badomens-idle"},
		{"windows", `C:\Users\ana`, ScopeUser, "schtasks /Run /TN badomens-idle"},
	}
	for _, tc := range cases {
		spec, err := DaemonService(tc.goos, tc.home, "/opt/badomens", tc.scope)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(StartDaemonServiceArgs(spec), " "); got != tc.want {
			t.Fatalf("%s/%s: quiero %q, tengo %q", tc.goos, tc.scope, tc.want, got)
		}
	}
}

func TestInstallAndUninstallDaemonServiceLinuxUser(t *testing.T) {
	home := t.TempDir()
	spec, err := DaemonService("linux", home, "/opt/badomens", ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeEnv{goos: "linux"}
	if err := InstallDaemonService(context.Background(), f.env(), spec); err != nil {
		t.Fatal(err)
	}
	raw, rerr := os.ReadFile(spec.Path)
	if rerr != nil {
		t.Fatalf("no se escribió la unidad: %v", rerr)
	}
	if string(raw) != spec.Unit {
		t.Fatal("la unidad escrita no coincide")
	}
	if len(f.runCalls) != 2 {
		t.Fatalf("quiero daemon-reload y enable --now: %v", f.runCalls)
	}
	if got := strings.Join(f.runCalls[0], " "); got != "systemctl --user daemon-reload" {
		t.Fatalf("primera llamada inesperada: %q", got)
	}
	if got := strings.Join(f.runCalls[1], " "); got != "systemctl --user enable --now badomens-idle.service" {
		t.Fatalf("segunda llamada inesperada: %q", got)
	}

	f2 := &fakeEnv{goos: "linux"}
	if err := UninstallDaemonService(context.Background(), f2.env(), spec); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(spec.Path); !os.IsNotExist(serr) {
		t.Fatal("la unidad debería haberse eliminado")
	}
	if got := strings.Join(f2.runCalls[0], " "); got != "systemctl --user disable --now badomens-idle.service" {
		t.Fatalf("desinstalación inesperada: %q", got)
	}
}

func TestInstallDaemonServiceWindowsUsesSchtasks(t *testing.T) {
	spec, err := DaemonService("windows", `C:\Users\ana`, `C:\badomens.exe`, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeEnv{goos: "windows"}
	if err := InstallDaemonService(context.Background(), f.env(), spec); err != nil {
		t.Fatal(err)
	}
	if len(f.runCalls) != 1 || f.runCalls[0][0] != "schtasks" {
		t.Fatalf("quiero crear la tarea con schtasks: %v", f.runCalls)
	}
	if got := strings.Join(f.runCalls[0], " "); got != "schtasks "+strings.Join(spec.InstallArgs, " ") {
		t.Fatalf("schtasks inesperado: %q", got)
	}
}

func TestServiceInstalledLinux(t *testing.T) {
	home := t.TempDir()
	spec, err := DaemonService("linux", home, "/opt/badomens", ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if ServiceInstalled(spec) {
		t.Fatal("sin escribir la unidad no debe estar instalada")
	}
	if err := os.MkdirAll(filepath.Dir(spec.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Path, []byte(spec.Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ServiceInstalled(spec) {
		t.Fatal("con la unidad escrita debe estar instalada")
	}
}

func TestDaemonArgv(t *testing.T) {
	if got := strings.Join(DaemonArgv("/x/badomens"), " "); got != "/x/badomens idle daemon" {
		t.Fatalf("DaemonArgv: %q", got)
	}
}
