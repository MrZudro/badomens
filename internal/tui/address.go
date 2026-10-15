package tui

import (
	"net"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MrZudro/badomens/internal/instance"
	"github.com/MrZudro/badomens/internal/tui/components"
)

// Este archivo resuelve el §10 de la SPEC 2: «Copiar dirección» debe funcionar
// con CUALQUIER dirección válida, ofrecer elegir cuando hay varias y explicar
// POR QUÉ no hay ninguna y QUÉ hacer cuando no la hay. La dirección se deriva
// antes del primer arranque siempre que se pueda (IP del tailnet, nombre
// MagicDNS y red local), sin tocar la red en View: la IP local se resuelve una
// vez en un tea.Cmd y queda en el estado de la pantalla.

// Redes por las que funciona una dirección copiable.
const (
	netTailscale = "Tailscale"
	netMagicDNS  = "MagicDNS"
	netLocal     = "Red local"
)

// copyCandidate es una dirección copiable con la red por la que funciona.
type copyCandidate struct {
	Addr string
	Net  string
	Why  string
}

// addressCandidates reúne las direcciones derivables de una instancia en orden de
// preferencia (tailnet → MagicDNS → red local), descartando duplicados. `host`
// es la IP del tailnet conocida y `localIP` la IPv4 local (puede estar vacía).
func addressCandidates(spec *instance.Spec, host string, m magicState, localIP string) []copyCandidate {
	if spec == nil || spec.Port <= 0 {
		return nil
	}
	out := make([]copyCandidate, 0, 3)
	seen := map[string]bool{}
	add := func(h, network, why string) {
		h = strings.TrimSpace(h)
		if h == "" {
			return
		}
		addr := hostPortString(h, spec.Port)
		if seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, copyCandidate{Addr: addr, Net: network, Why: why})
	}
	if host == "" && spec.Connection != nil {
		// El host persistido sirve de respaldo cuando la pantalla no conoce la IP
		// en vivo (p. ej. el detalle aún no se ha cargado).
		host = spec.Connection.Host
	}
	add(host, netTailscale, "requiere que los jugadores estén en tu tailnet")
	name := ""
	if spec.Connection != nil {
		name = spec.Connection.MagicDNS
	}
	if m.Known {
		// El estado en vivo manda sobre el valor persistido (Fase 8 §5).
		name = m.Name
	}
	add(name, netMagicDNS, "nombre del tailnet: no cambia si cambia la IP")
	add(localIP, netLocal, "solo funciona en tu red local (LAN)")
	return out
}

// preferredAddress es la dirección que copia el atajo rápido (la preferida).
func preferredAddress(cands []copyCandidate) (string, string) {
	if len(cands) == 0 {
		return "", ""
	}
	return cands[0].Addr, cands[0].Net
}

// copyAddressCmd copia la dirección preferida o explica por qué no hay ninguna y
// qué hacer. La E/S es solo el portapapeles: las direcciones ya están calculadas.
func copyAddressCmd(cands []copyCandidate, name string) tea.Cmd {
	if len(cands) == 0 {
		return func() tea.Msg {
			return toastMsg{Text: "⚠ " + noAddressWhy(name), Kind: components.Destructive}
		}
	}
	addr, network := preferredAddress(cands)
	return func() tea.Msg {
		if err := copyToClipboard(addr); err != nil {
			return toastMsg{Text: "✖ No se pudo copiar: " + err.Error(), Kind: components.Destructive}
		}
		return toastMsg{Text: "✓ Copiado: " + addr + " (" + network + ")"}
	}
}

// chooseAddressCmd ofrece elegir entre varias direcciones; con una sola copia
// directamente y sin ninguna explica el motivo y la acción recomendada.
func chooseAddressCmd(cands []copyCandidate, name string) tea.Cmd {
	switch len(cands) {
	case 0:
		return copyAddressCmd(nil, name)
	case 1:
		return copyAddressCmd(cands, name)
	}
	items := addressChooserItems(cands)
	return func() tea.Msg {
		return chooserOpenMsg{
			Title: "Copiar dirección de «" + name + "»",
			Hint:  "↑/↓ eligen la dirección · Enter copia · Esc cierra",
			ID:    chooserCopyAddress,
			Items: items,
		}
	}
}

// noAddressWhy explica POR QUÉ no hay dirección y QUÉ hacer (§10): un mensaje
// accionable, nunca un «no hay dirección todavía» seco.
func noAddressWhy(name string) string {
	who := "la instancia"
	if strings.TrimSpace(name) != "" {
		who = "«" + name + "»"
	}
	return "No hay dirección que copiar todavía: sin IP del tailnet, sin nombre MagicDNS y sin IP local, " +
		"y el puerto solo se publica al arrancar. Inicia " + who + " con [S] o ejecuta 'tailscale up'."
}

// addressChooserItems convierte las candidatas en opciones del selector ÚNICO
// (P-03), exponiendo la dirección por VALOR (T-04).
func addressChooserItems(cands []copyCandidate) []components.SelectItem {
	items := make([]components.SelectItem, 0, len(cands))
	for _, c := range cands {
		items = append(items, components.SelectItem{
			Value: c.Addr, Label: c.Addr, Detail: c.Net + " · " + c.Why,
		})
	}
	return items
}

// localIPCmd resuelve la IPv4 local en un Cmd (nunca en View): permite ofrecer
// una dirección de red local antes del primer arranque.
func localIPCmd() tea.Cmd { return func() tea.Msg { return localIPMsg{ip: localIPv4()} } }

// localIPMsg trae la IPv4 local (LAN).
type localIPMsg struct{ ip string }

// localIPv4 devuelve la primera IPv4 no loopback ni link-local de una interfaz
// activa ("" si no hay ninguna).
func localIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, aerr := ifc.Addrs()
		if aerr != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

// hostPortString compone «host:puerto» (sin corchetes: son IPv4 o nombres).
func hostPortString(host string, port int) string {
	if strings.TrimSpace(host) == "" || port <= 0 {
		return ""
	}
	return host + ":" + itoa(port)
}
