// Package ports detecta y reserva puertos libres teniendo en cuenta, a la vez,
// el registro de badomens, los contenedores Docker (incluidos ajenos), procesos
// del host que escuchan y peers del tailnet (RF-3).
//
// El chequeo combina cuatro fuentes de verdad y devuelve la unión de conflictos:
//
//  1. Registro de badomens (todas las instancias, incluso detenidas).
//  2. Docker: contenedores con puertos publicados.
//  3. Host local: prueba de bind real + proceso que escucha (si se puede saber).
//  4. Peers del tailnet que responden en ese puerto (aviso o bloqueo, según
//     configuración; por defecto aviso).
//
// La reserva es atómica mediante un bloqueo de archivo, de forma que TUI, CLI y
// MCP simultáneos no eligen el mismo puerto.
package ports

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OwnerKind identifica quién ocupa un puerto.
type OwnerKind int

// Tipos de ocupante.
const (
	// OwnerUnknown es un ocupante no identificado.
	OwnerUnknown OwnerKind = iota
	// OwnerBadomens es una instancia registrada en badomens.
	OwnerBadomens
	// OwnerDocker es un contenedor Docker (propio o ajeno).
	OwnerDocker
	// OwnerHostProcess es un proceso del host que escucha en el puerto.
	OwnerHostProcess
	// OwnerTailnetPeer es un peer del tailnet que responde en el puerto.
	OwnerTailnetPeer
	// OwnerReserved es una reserva pendiente de badomens.
	OwnerReserved
)

// String devuelve el identificador estable del tipo de ocupante.
func (k OwnerKind) String() string {
	switch k {
	case OwnerBadomens:
		return "badomens"
	case OwnerDocker:
		return "docker"
	case OwnerHostProcess:
		return "host_process"
	case OwnerTailnetPeer:
		return "tailnet_peer"
	case OwnerReserved:
		return "reserved"
	}
	return "unknown"
}

// Severity de un conflicto.
type Severity int

// Niveles de severidad.
const (
	// SeverityInfo es informativo: no impide usar el puerto.
	SeverityInfo Severity = iota
	// SeverityWarn avisa pero no impide usar el puerto.
	SeverityWarn
	// SeverityBlock impide usar el puerto.
	SeverityBlock
)

// String devuelve el identificador estable de la severidad.
func (s Severity) String() string {
	switch s {
	case SeverityWarn:
		return "warn"
	case SeverityBlock:
		return "block"
	}
	return "info"
}

// Owner describe quién ocupa un puerto.
type Owner struct {
	Kind   OwnerKind `json:"kind"`
	Name   string    `json:"name,omitempty"`
	PID    int       `json:"pid,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Conflict es un motivo por el que un puerto no está disponible.
type Conflict struct {
	Port     int      `json:"port"`
	Owner    Owner    `json:"owner"`
	Severity Severity `json:"severity"`
}

// Result es el resultado de comprobar un puerto.
type Result struct {
	Port      int        `json:"port"`
	Free      bool       `json:"free"`
	Conflicts []Conflict `json:"conflicts,omitempty"`
	// Suggested es el siguiente puerto libre propuesto (0 si no se calculó).
	Suggested int `json:"suggested,omitempty"`
}

// Checker comprueba si un puerto está libre en un bind dado.
type Checker interface {
	Check(ctx context.Context, bind netip.Addr, port int) (Result, error)
}

// ScanMode decide cómo se tratan los peers del tailnet que responden.
type ScanMode int

// Modos de sondeo de peers.
const (
	// ScanOff desactiva el sondeo de peers.
	ScanOff ScanMode = iota
	// ScanWarn avisa (por defecto).
	ScanWarn
	// ScanBlock bloquea el puerto si algún peer responde.
	ScanBlock
)

// ParseScanMode interpreta "off", "warn" o "block".
func ParseScanMode(s string) (ScanMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "warn":
		return ScanWarn, nil
	case "off", "ninguno", "none":
		return ScanOff, nil
	case "block", "bloquear":
		return ScanBlock, nil
	}
	return ScanWarn, fmt.Errorf("modo de sondeo de tailnet inválido %q (off|warn|block)", s)
}

// Range es un rango de puertos inclusivo.
type Range struct {
	From int
	To   int
}

// DefaultRange es el rango por defecto de puertos de badomens.
var DefaultRange = Range{From: 25565, To: 25664}

// ParseRange interpreta "25565-25664" o un puerto suelto "25565".
func ParseRange(s string) (Range, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultRange, nil
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		from, err := strconv.Atoi(strings.TrimSpace(s[:i]))
		if err != nil {
			return Range{}, fmt.Errorf("rango de puertos inválido %q", s)
		}
		to, err := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		if err != nil {
			return Range{}, fmt.Errorf("rango de puertos inválido %q", s)
		}
		if from < 1 || to > 65535 || from > to {
			return Range{}, fmt.Errorf("rango de puertos fuera de 1-65535: %q", s)
		}
		return Range{From: from, To: to}, nil
	}
	p, err := strconv.Atoi(s)
	if err != nil || p < 1 || p > 65535 {
		return Range{}, fmt.Errorf("puerto inválido %q", s)
	}
	return Range{From: p, To: p}, nil
}

// Contains indica si p cae dentro del rango.
func (r Range) Contains(p int) bool { return p >= r.From && p <= r.To }

// candidates devuelve el rango empezando por preferred (si cae dentro) y
// continuando en orden, sin repetir.
func (r Range) candidates(preferred int) []int {
	if r.To < r.From {
		return nil
	}
	size := r.To - r.From + 1
	out := make([]int, 0, size)
	start := r.From
	if r.Contains(preferred) {
		start = preferred
	}
	for i := 0; i < size; i++ {
		p := start + i
		if p > r.To {
			p -= size
		}
		out = append(out, p)
	}
	return out
}

// Prober reúne las fuentes de verdad del entorno. Se inyecta para poder probar
// el checker sin Docker, red ni tailnet.
type Prober interface {
	// RegistryPorts devuelve los puertos del registro de badomens.
	RegistryPorts(ctx context.Context) ([]RegistryPort, error)
	// DockerPorts devuelve los puertos publicados por contenedores Docker.
	DockerPorts(ctx context.Context) ([]DockerPort, error)
	// TryBind prueba un bind real (true = libre).
	TryBind(ctx context.Context, bind netip.Addr, port int) (bool, error)
	// ProcessOwner intenta identificar el proceso que escucha (opcional).
	ProcessOwner(ctx context.Context, bind netip.Addr, port int) (Owner, bool)
	// Peers devuelve los peers del tailnet conocidos.
	Peers(ctx context.Context) ([]Peer, error)
	// PingPeer comprueba si un peer responde en un puerto TCP.
	PingPeer(ctx context.Context, ip string, port int) (bool, error)
}

// RegistryPort es un puerto del registro de badomens.
type RegistryPort struct {
	Instance string `json:"instance"`
	Port     int    `json:"port"`
	State    string `json:"state,omitempty"`
}

// DockerPort es un puerto publicado por un contenedor.
type DockerPort struct {
	Container string `json:"container"`
	Project   string `json:"project,omitempty"`
	Port      int    `json:"port"`
	HostIP    string `json:"host_ip,omitempty"`
}

// Peer es un nodo del tailnet.
type Peer struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Online bool   `json:"online"`
}

// DefaultChecker es la implementación de Checker sobre un Prober.
type DefaultChecker struct {
	// Prober reúne las fuentes de verdad.
	Prober Prober
	// TailnetScan decide la severidad de los peers que responden.
	TailnetScan ScanMode
	// PeerTimeout es el timeout por peer (por defecto 400 ms).
	PeerTimeout time.Duration
	// PeerWorkers es la concurrencia máxima del sondeo (por defecto 8).
	PeerWorkers int
	// Exclude son instancias del registro que no deben contar como conflicto
	// (p. ej. la propia instancia al revalidar su puerto en un arranque).
	Exclude map[string]bool
}

// Check reúne los conflictos de las cuatro fuentes. El error solo se devuelve
// si falla una fuente esencial (el registro); las fuentes opcionales degradan
// con gracia.
func (c *DefaultChecker) Check(ctx context.Context, bind netip.Addr, port int) (Result, error) {
	if port < 1 || port > 65535 {
		return Result{}, fmt.Errorf("puerto fuera de rango: %d", port)
	}
	if c.Prober == nil {
		return Result{}, fmt.Errorf("el checker no tiene Prober")
	}
	var conflicts []Conflict

	// 1) Registro de badomens.
	reg, err := c.Prober.RegistryPorts(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, rp := range reg {
		if rp.Port != port || c.Exclude[rp.Instance] {
			continue
		}
		detail := "instancia «" + rp.Instance + "» del registro de badomens"
		if rp.State != "" {
			detail += " (" + rp.State + ")"
		}
		conflicts = append(conflicts, Conflict{
			Port:     port,
			Severity: SeverityBlock,
			Owner:    Owner{Kind: OwnerBadomens, Name: rp.Instance, Detail: detail},
		})
	}

	// 2) Docker (opcional: si falla, se degrada con gracia).
	if dk, derr := c.Prober.DockerPorts(ctx); derr == nil {
		for _, dp := range dk {
			if dp.Port != port {
				continue
			}
			detail := "contenedor «" + dp.Container + "»"
			if dp.Project != "" {
				detail += " (proyecto " + dp.Project + ")"
			}
			conflicts = append(conflicts, Conflict{
				Port:     port,
				Severity: SeverityBlock,
				Owner:    Owner{Kind: OwnerDocker, Name: dp.Container, Detail: detail},
			})
		}
	}

	// 3) Host local: bind real (fuente de verdad) + proceso que escucha.
	free, berr := c.Prober.TryBind(ctx, bind, port)
	if berr != nil {
		return Result{}, berr
	}
	if !free {
		owner := Owner{Kind: OwnerHostProcess, Detail: "proceso desconocido"}
		if o, ok := c.Prober.ProcessOwner(ctx, bind, port); ok {
			o.Kind = OwnerHostProcess
			owner = o
		}
		conflicts = append(conflicts, Conflict{Port: port, Severity: SeverityBlock, Owner: owner})
	}

	// 4) Peers del tailnet.
	if c.TailnetScan != ScanOff {
		if cs, perr := c.checkPeers(ctx, port); perr == nil {
			conflicts = append(conflicts, cs...)
		}
	}

	usable := true
	for _, cf := range conflicts {
		if cf.Severity == SeverityBlock {
			usable = false
			break
		}
	}
	sort.SliceStable(conflicts, func(i, j int) bool { return conflicts[i].Severity > conflicts[j].Severity })
	return Result{Port: port, Free: usable, Conflicts: conflicts}, nil
}

// checkPeers sondea los peers online en el puerto con concurrencia limitada.
func (c *DefaultChecker) checkPeers(ctx context.Context, port int) ([]Conflict, error) {
	peers, err := c.Prober.Peers(ctx)
	if err != nil {
		return nil, err
	}
	online := make([]Peer, 0, len(peers))
	for _, p := range peers {
		if p.Online && p.IP != "" {
			online = append(online, p)
		}
	}
	if len(online) == 0 {
		return nil, nil
	}
	workers := c.PeerWorkers
	if workers <= 0 {
		workers = 8
	}
	if workers > len(online) {
		workers = len(online)
	}
	timeout := c.PeerTimeout
	if timeout <= 0 {
		timeout = 400 * time.Millisecond
	}
	sev := SeverityWarn
	if c.TailnetScan == ScanBlock {
		sev = SeverityBlock
	}

	var mu sync.Mutex
	var hits []Peer
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, p := range online {
		wg.Add(1)
		go func(p Peer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if ok, _ := c.Prober.PingPeer(pctx, p.IP, port); ok {
				mu.Lock()
				hits = append(hits, p)
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	conflicts := make([]Conflict, 0, len(hits))
	for _, p := range hits {
		conflicts = append(conflicts, Conflict{
			Port:     port,
			Severity: sev,
			Owner: Owner{
				Kind:   OwnerTailnetPeer,
				Name:   p.Name,
				Detail: "peer «" + p.Name + "» (" + p.IP + ") responde en el puerto",
			},
		})
	}
	return conflicts, nil
}
