package ports

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
	gproc "github.com/shirou/gopsutil/v4/process"

	"github.com/MrZudro/badomens/internal/execx"
)

// RealProber es la implementación de Prober sobre el sistema: registro de
// badomens, Docker, bind real, proceso que escucha y tailnet.
type RealProber struct {
	// RegistryFn devuelve los puertos del registro de badomens. Puede ser nil.
	RegistryFn func(ctx context.Context) ([]RegistryPort, error)
	// Timeout es el timeout de las sondas TCP (por defecto 400 ms).
	Timeout time.Duration
}

// NewRealProber construye un prober con el registro indicado.
func NewRealProber(registry func(ctx context.Context) ([]RegistryPort, error)) *RealProber {
	return &RealProber{RegistryFn: registry}
}

// RegistryPorts devuelve los puertos del registro de badomens.
func (p *RealProber) RegistryPorts(ctx context.Context) ([]RegistryPort, error) {
	if p.RegistryFn == nil {
		return nil, nil
	}
	return p.RegistryFn(ctx)
}

// dockerPSLine es una línea de `docker ps --format json`.
type dockerPSLine struct {
	Names  string `json:"Names"`
	Labels string `json:"Labels"`
	Ports  string `json:"Ports"`
}

var hostPortRe = regexp.MustCompile(`:(\d+)->`)

// DockerPorts consulta los contenedores con puertos publicados.
func (p *RealProber) DockerPorts(ctx context.Context) ([]DockerPort, error) {
	res, err := execx.Run(ctx, "docker", "ps", "--format", "json")
	if err != nil {
		return nil, err
	}
	var out []DockerPort
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row dockerPSLine
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		project := labelValue(row.Labels, "com.docker.compose.project")
		for _, m := range hostPortRe.FindAllStringSubmatch(row.Ports, -1) {
			port, _ := strconv.Atoi(m[1])
			if port <= 0 {
				continue
			}
			out = append(out, DockerPort{Container: row.Names, Project: project, Port: port})
		}
	}
	return out, nil
}

// TryBind prueba a escuchar en el puerto, tanto en el bind indicado como en
// 0.0.0.0. Devuelve true si ambos quedan libres.
func (p *RealProber) TryBind(ctx context.Context, bind netip.Addr, port int) (bool, error) {
	hosts := []string{"0.0.0.0"}
	if bind.IsValid() && !bind.IsUnspecified() {
		hosts = append([]string{bind.String()}, hosts...)
	}
	seen := map[string]bool{}
	for _, host := range hosts {
		if seen[host] {
			continue
		}
		seen[host] = true
		lc := net.ListenConfig{}
		ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return false, nil
		}
		_ = ln.Close()
	}
	return true, nil
}

// ProcessOwner identifica el proceso que escucha en el puerto (mejor esfuerzo:
// puede faltar por permisos).
func (p *RealProber) ProcessOwner(_ context.Context, _ netip.Addr, port int) (Owner, bool) {
	conns, err := gnet.Connections("tcp")
	if err != nil {
		return Owner{}, false
	}
	for _, c := range conns {
		if int(c.Laddr.Port) != port {
			continue
		}
		if c.Status != "LISTEN" {
			continue
		}
		owner := Owner{Kind: OwnerHostProcess, PID: int(c.Pid), Detail: "PID " + strconv.Itoa(int(c.Pid))}
		if proc, perr := gproc.NewProcess(c.Pid); perr == nil {
			if name, nerr := proc.Name(); nerr == nil && name != "" {
				owner.Name = name
				owner.Detail = name + " (PID " + strconv.Itoa(int(c.Pid)) + ")"
			}
		}
		return owner, true
	}
	return Owner{}, false
}

// tailscaleStatus es el subconjunto de `tailscale status --json` que usamos.
type tailscaleStatus struct {
	Self *tailscaleNode            `json:"Self"`
	Peer map[string]*tailscaleNode `json:"Peer"`
}

type tailscaleNode struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

// Peers devuelve los peers del tailnet (incluido el propio nodo).
func (p *RealProber) Peers(ctx context.Context) ([]Peer, error) {
	res, err := execx.Run(ctx, "tailscale", "status", "--json")
	if err != nil {
		return nil, err
	}
	var st tailscaleStatus
	if json.Unmarshal([]byte(res.Stdout), &st) != nil {
		return nil, nil
	}
	var out []Peer
	add := func(n *tailscaleNode) {
		if n == nil {
			return
		}
		ip := firstIPv4(n.TailscaleIPs)
		if ip == "" {
			return
		}
		name := strings.TrimSuffix(n.DNSName, ".")
		if name == "" {
			name = n.HostName
		}
		out = append(out, Peer{Name: name, IP: ip, Online: n.Online})
	}
	add(st.Self)
	for _, n := range st.Peer {
		add(n)
	}
	return out, nil
}

// PingPeer comprueba si un peer acepta conexión TCP en el puerto.
func (p *RealProber) PingPeer(ctx context.Context, ip string, port int) (bool, error) {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 400 * time.Millisecond
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return false, nil
	}
	_ = conn.Close()
	return true, nil
}

func labelValue(labels, key string) string {
	for _, part := range strings.Split(labels, ",") {
		if i := strings.IndexByte(part, '='); i >= 0 {
			if strings.TrimSpace(part[:i]) == key {
				return strings.TrimSpace(part[i+1:])
			}
		}
	}
	return ""
}

func firstIPv4(ips []string) string {
	for _, ip := range ips {
		if strings.Count(ip, ":") == 0 && strings.Contains(ip, ".") {
			return ip
		}
	}
	return ""
}
