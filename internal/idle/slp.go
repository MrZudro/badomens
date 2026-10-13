package idle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Status es la respuesta del Server List Ping de Minecraft.
type Status struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	} `json:"players"`
	Description json.RawMessage `json:"description"`
}

// Ping consulta el estado del servidor por Server List Ping (protocolo estándar
// de Minecraft, sin RCON). Devuelve error si no se puede completar el sondeo.
func Ping(ctx context.Context, host string, port int, timeout time.Duration) (Status, error) {
	var st Status
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return st, err
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	// Handshake: id 0x00, protocolo, host, puerto, next state = 1 (status).
	var hs bytes.Buffer
	writeVarInt(&hs, 0x00)
	writeVarInt(&hs, -1)
	writeString(&hs, host)
	hs.WriteByte(byte(port >> 8))
	hs.WriteByte(byte(port & 0xFF))
	writeVarInt(&hs, 1)

	// Status request: id 0x00 sin campos.
	var req bytes.Buffer
	writeVarInt(&req, 0x00)

	var out bytes.Buffer
	writeFramed(&out, hs.Bytes())
	writeFramed(&out, req.Bytes())
	if _, err := conn.Write(out.Bytes()); err != nil {
		return st, err
	}

	r := bufio.NewReader(conn)
	length, err := readVarInt(r)
	if err != nil {
		return st, err
	}
	if length < 0 || length > 1<<21 {
		return st, fmt.Errorf("respuesta de estado de tamaño inválido: %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return st, err
	}
	br := bytes.NewReader(payload)
	if _, err := readVarInt(br); err != nil { // packet id (0x00)
		return st, err
	}
	raw, err := readString(br)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return st, fmt.Errorf("respuesta de estado ilegible: %w", err)
	}
	return st, nil
}

// writeFramed escribe un paquete con su longitud por delante.
func writeFramed(buf *bytes.Buffer, packet []byte) {
	writeVarInt(buf, int32(len(packet)))
	buf.Write(packet)
}

// writeVarInt escribe un VarInt del protocolo de Minecraft.
func writeVarInt(buf *bytes.Buffer, v int32) {
	u := uint32(v)
	for {
		b := byte(u & 0x7F)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if u == 0 {
			return
		}
	}
}

// readVarInt lee un VarInt del protocolo de Minecraft.
func readVarInt(r io.ByteReader) (int32, error) {
	var result uint32
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift >= 35 {
			return 0, fmt.Errorf("VarInt demasiado largo")
		}
	}
	return int32(result), nil
}

// writeString escribe una cadena precedida de su longitud en VarInt.
func writeString(buf *bytes.Buffer, s string) {
	writeVarInt(buf, int32(len(s)))
	buf.WriteString(s)
}

// readString lee una cadena precedida de su longitud en VarInt.
func readString(r *bytes.Reader) (string, error) {
	n, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if n < 0 || int(n) > r.Len() {
		return "", fmt.Errorf("longitud de cadena inválida: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}
