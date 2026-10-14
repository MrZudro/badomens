package ports

import (
	"context"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MrZudro/badomens/internal/errx"
)

func errDetails(err error) map[string]any { return errx.Details(err) }

// fakeProber implementa Prober sin Docker, red ni tailnet.
type fakeProber struct {
	registry []RegistryPort
	docker   []DockerPort
	// hostUsed marca los puertos ocupados localmente (TryBind = false).
	hostUsed map[int]bool
	// owners enriquece el proceso que escucha.
	owners map[int]Owner
	peers  []Peer
	// ping marca respuestas de peers por "ip:puerto".
	ping map[string]bool

	dockerErr   error
	registryErr error
}

func (f *fakeProber) RegistryPorts(context.Context) ([]RegistryPort, error) {
	return f.registry, f.registryErr
}

func (f *fakeProber) DockerPorts(context.Context) ([]DockerPort, error) {
	return f.docker, f.dockerErr
}

func (f *fakeProber) TryBind(_ context.Context, _ netip.Addr, port int) (bool, error) {
	return !f.hostUsed[port], nil
}

func (f *fakeProber) ProcessOwner(_ context.Context, _ netip.Addr, port int) (Owner, bool) {
	o, ok := f.owners[port]
	return o, ok
}

func (f *fakeProber) Peers(context.Context) ([]Peer, error) { return f.peers, nil }

func (f *fakeProber) PingPeer(_ context.Context, ip string, port int) (bool, error) {
	return f.ping[ip+":"+strconv.Itoa(port)], nil
}

func checker(p *fakeProber, mode ScanMode) *DefaultChecker {
	return &DefaultChecker{Prober: p, TailnetScan: mode, PeerTimeout: time.Second}
}

func hasBlock(res Result) bool {
	for _, c := range res.Conflicts {
		if c.Severity == SeverityBlock {
			return true
		}
	}
	return false
}

func TestCheckFuentesUnidas(t *testing.T) {
	p := &fakeProber{
		registry: []RegistryPort{{Instance: "alpha", Port: 25565, State: "exited"}},
		docker:   []DockerPort{{Container: "mina", Project: "otro", Port: 25566}},
		hostUsed: map[int]bool{25567: true},
		owners:   map[int]Owner{25567: {Kind: OwnerHostProcess, Name: "java.exe", PID: 42}},
		peers:    []Peer{{Name: "laptop-ana", IP: "100.64.1.9", Online: true}, {Name: "offline", IP: "100.64.1.10"}},
		ping:     map[string]bool{"100.64.1.9:25568": true},
	}
	c := checker(p, ScanWarn)
	ctx := context.Background()

	res, err := c.Check(ctx, netip.Addr{}, 25565)
	if err != nil || res.Free || !hasBlock(res) {
		t.Fatalf("registro: quiero bloqueo, tengo %+v err=%v", res, err)
	}
	res, _ = c.Check(ctx, netip.Addr{}, 25566)
	if res.Free || !hasBlock(res) {
		t.Fatalf("docker: quiero bloqueo, tengo %+v", res)
	}
	res, _ = c.Check(ctx, netip.Addr{}, 25567)
	if res.Free || !hasBlock(res) {
		t.Fatalf("host: quiero bloqueo, tengo %+v", res)
	}
	if res.Conflicts[0].Owner.Name != "java.exe" {
		t.Fatalf("host: quiero el nombre del proceso, tengo %+v", res.Conflicts[0].Owner)
	}
	res, _ = c.Check(ctx, netip.Addr{}, 25568)
	if !res.Free || hasBlock(res) {
		t.Fatalf("peer en modo warn: quiero libre con aviso, tengo %+v", res)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Severity != SeverityWarn {
		t.Fatalf("peer: quiero un aviso, tengo %+v", res.Conflicts)
	}
	res, _ = c.Check(ctx, netip.Addr{}, 25569)
	if !res.Free || len(res.Conflicts) != 0 {
		t.Fatalf("puerto libre: quiero sin conflictos, tengo %+v", res)
	}
}

func TestCheckPeerBlockYOff(t *testing.T) {
	p := &fakeProber{
		peers: []Peer{{Name: "ana", IP: "100.64.1.9", Online: true}},
		ping:  map[string]bool{"100.64.1.9:25565": true},
	}
	ctx := context.Background()

	res, _ := checker(p, ScanBlock).Check(ctx, netip.Addr{}, 25565)
	if res.Free || !hasBlock(res) {
		t.Fatalf("modo block: quiero bloqueo, tengo %+v", res)
	}
	res, _ = checker(p, ScanOff).Check(ctx, netip.Addr{}, 25565)
	if !res.Free || len(res.Conflicts) != 0 {
		t.Fatalf("modo off: quiero sin sondeo, tengo %+v", res)
	}
}

func TestCheckRegistroErrorEsFatal(t *testing.T) {
	p := &fakeProber{registryErr: context.DeadlineExceeded}
	if _, err := checker(p, ScanOff).Check(context.Background(), netip.Addr{}, 25565); err == nil {
		t.Fatal("un fallo del registro debe propagarse")
	}
}

func TestCheckDockerErrorDegrada(t *testing.T) {
	p := &fakeProber{dockerErr: context.DeadlineExceeded}
	res, err := checker(p, ScanOff).Check(context.Background(), netip.Addr{}, 25565)
	if err != nil {
		t.Fatalf("un fallo de docker no debe abortar: %v", err)
	}
	if !res.Free {
		t.Fatalf("quiero libre, tengo %+v", res)
	}
}

func TestCandidatesEnvuelveElRango(t *testing.T) {
	r := Range{From: 10, To: 12}
	got := r.candidates(11)
	want := []int{11, 12, 10}
	if len(got) != len(want) {
		t.Fatalf("candidates: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates: quiero %v, tengo %v", want, got)
		}
	}
}

func TestParseRangeYScanMode(t *testing.T) {
	if r, err := ParseRange("25565-25664"); err != nil || r.From != 25565 || r.To != 25664 {
		t.Fatalf("ParseRange rango: %+v err=%v", r, err)
	}
	if r, err := ParseRange(""); err != nil || r != DefaultRange {
		t.Fatalf("ParseRange vacío: %+v err=%v", r, err)
	}
	if _, err := ParseRange("100-50"); err == nil {
		t.Fatal("rango invertido debe fallar")
	}
	for in, want := range map[string]ScanMode{"off": ScanOff, "warn": ScanWarn, "block": ScanBlock, "": ScanWarn} {
		got, err := ParseScanMode(in)
		if err != nil || got != want {
			t.Fatalf("ParseScanMode(%q): quiero %v, tengo %v err=%v", in, want, got, err)
		}
	}
	if _, err := ParseScanMode("quizá"); err == nil {
		t.Fatal("modo inválido debe fallar")
	}
}

func TestAllocatorEligeSiguienteLibre(t *testing.T) {
	p := &fakeProber{hostUsed: map[int]bool{25565: true}}
	a := &Allocator{Checker: checker(p, ScanOff), Range: Range{From: 25565, To: 25570}, Store: NewMemoryStore()}
	res, err := a.Reserve(context.Background(), "alpha", 25565)
	if err != nil {
		t.Fatal(err)
	}
	if res.Port != 25566 {
		t.Fatalf("quiero 25566 (25565 ocupado), tengo %d", res.Port)
	}
}

func TestAllocatorEsIdempotente(t *testing.T) {
	a := &Allocator{Checker: checker(&fakeProber{}, ScanOff), Range: Range{From: 25565, To: 25570}, Store: NewMemoryStore()}
	ctx := context.Background()
	first, _ := a.Reserve(ctx, "alpha", 25565)
	second, _ := a.Reserve(ctx, "alpha", 25569)
	if first.Port != second.Port {
		t.Fatalf("reservas idempotentes: %d != %d", first.Port, second.Port)
	}
}

func TestAllocatorPurgaCaducadas(t *testing.T) {
	now := time.Now()
	store := NewMemoryStore()
	_ = store.Update(context.Background(), func(m map[string]Reservation) (map[string]Reservation, error) {
		m["muerta"] = Reservation{Instance: "muerta", Port: 25565, At: now.Add(-time.Hour), Expires: now.Add(-time.Minute)}
		return m, nil
	})
	a := &Allocator{Checker: checker(&fakeProber{}, ScanOff), Range: Range{From: 25565, To: 25570}, Store: store, Now: func() time.Time { return now }}
	res, err := a.Reserve(context.Background(), "alpha", 25565)
	if err != nil {
		t.Fatal(err)
	}
	if res.Port != 25565 {
		t.Fatalf("una reserva caducada no debe bloquear: tengo %d", res.Port)
	}
	rs, _ := a.Reservations(context.Background())
	if len(rs) != 1 {
		t.Fatalf("quiero una reserva viva, tengo %+v", rs)
	}
}

func TestAllocatorRangoAgotado(t *testing.T) {
	used := map[int]bool{25565: true, 25566: true}
	a := &Allocator{Checker: checker(&fakeProber{hostUsed: used}, ScanOff), Range: Range{From: 25565, To: 25566}, Store: NewMemoryStore()}
	_, err := a.Reserve(context.Background(), "alpha", 25565)
	if err == nil {
		t.Fatal("quiero error de rango agotado")
	}
	if d := errDetails(err); d["reason"] != "port_range_exhausted" {
		t.Fatalf("quiero reason=port_range_exhausted, tengo %+v", d)
	}
}

func TestAllocatorConcurrenteRepartePuertos(t *testing.T) {
	p := &fakeProber{}
	a := &Allocator{Checker: checker(p, ScanOff), Range: Range{From: 25565, To: 25664}, Store: NewMemoryStore()}
	const n = 24
	var wg sync.WaitGroup
	got := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := a.Reserve(context.Background(), "i"+strconv.Itoa(i), 25565)
			if err != nil {
				t.Errorf("reserva %d: %v", i, err)
				return
			}
			got[i] = r.Port
		}(i)
	}
	wg.Wait()
	seen := map[int]bool{}
	for i, p := range got {
		if p == 0 {
			continue
		}
		if seen[p] {
			t.Fatalf("puerto repetido %d (índice %d): %v", p, i, got)
		}
		seen[p] = true
	}
	if len(seen) != n {
		t.Fatalf("quiero %d puertos únicos, tengo %d", n, len(seen))
	}
}

func TestAllocatorCheckSumaReservas(t *testing.T) {
	p := &fakeProber{}
	a := &Allocator{Checker: checker(p, ScanOff), Range: Range{From: 25565, To: 25570}, Store: NewMemoryStore()}
	ctx := context.Background()
	if _, err := a.Reserve(ctx, "alpha", 25565); err != nil {
		t.Fatal(err)
	}
	res, err := a.Check(ctx, netip.Addr{}, 25565)
	if err != nil {
		t.Fatal(err)
	}
	if res.Free {
		t.Fatalf("una reserva pendiente debe bloquear: %+v", res)
	}
	if res.Suggested != 25566 {
		t.Fatalf("quiero sugerir 25566, tengo %d", res.Suggested)
	}
	if err := a.Release(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	res, _ = a.Check(ctx, netip.Addr{}, 25565)
	if !res.Free {
		t.Fatalf("tras liberar debe estar libre: %+v", res)
	}
}
