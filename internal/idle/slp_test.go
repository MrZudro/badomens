package idle

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// startFakeServer levanta un servidor de prueba que responde al ping de estado.
func startFakeServer(t *testing.T, status string) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		// Handshake y request de estado: se leen y se ignoran.
		if _, err := readPacket(r); err != nil {
			return
		}
		if _, err := readPacket(r); err != nil {
			return
		}
		// Respuesta: paquete con id 0x00 y la cadena JSON.
		var payload bytes.Buffer
		writeVarInt(&payload, 0x00)
		writeString(&payload, status)
		var out bytes.Buffer
		writeVarInt(&out, int32(payload.Len()))
		out.Write(payload.Bytes())
		_, _ = conn.Write(out.Bytes())
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

// readPacket lee un paquete completo (longitud + payload) y lo devuelve.
func readPacket(r *bufio.Reader) ([]byte, error) {
	n, err := readVarInt(r)
	if err != nil {
		return nil, err
	}
	if n < 0 || n > 1<<21 {
		return nil, io.ErrUnexpectedEOF
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func TestPingLeeEstado(t *testing.T) {
	status := `{"version":{"name":"1.21.4","protocol":765},"players":{"max":20,"online":3},"description":{"text":"hola"}}`
	host, port := startFakeServer(t, status)
	st, err := Ping(context.Background(), host, port, 2*time.Second)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if st.Players.Online != 3 || st.Players.Max != 20 {
		t.Fatalf("jugadores: %+v", st.Players)
	}
	if st.Version.Name != "1.21.4" {
		t.Fatalf("versión: %+v", st.Version)
	}
}

func TestPingFallaSinServidor(t *testing.T) {
	if _, err := Ping(context.Background(), "127.0.0.1", 1, 500*time.Millisecond); err == nil {
		t.Fatal("quiero error contra un puerto cerrado")
	}
}

func TestSLPCounterEndpoint(t *testing.T) {
	status := `{"players":{"max":10,"online":7}}`
	host, port := startFakeServer(t, status)
	c := &SLPCounter{
		Endpoint: func(_ string) (string, int, bool) { return host, port, true },
		Timeout:  time.Second,
	}
	n, known, err := c.Online(context.Background(), "alpha")
	if err != nil || !known || n != 7 {
		t.Fatalf("Online: n=%d known=%v err=%v", n, known, err)
	}
	// Sin endpoint: desconocido, sin error.
	no := &SLPCounter{}
	if n, known, _ := no.Online(context.Background(), "alpha"); known || n != 0 {
		t.Fatalf("sin endpoint quiero desconocido: n=%d known=%v", n, known)
	}
}
