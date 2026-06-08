package cockpit

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// wsGUID is the RFC 6455 magic value concatenated with the client's
// Sec-WebSocket-Key to derive the Sec-WebSocket-Accept handshake response.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket opcodes (RFC 6455 §5.2). Only text and the control frames are used:
// the cockpit pushes JSON text frames and answers ping/close.
const (
	opText  = 0x1
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

// acceptKey computes the Sec-WebSocket-Accept value for a client key.
func acceptKey(clientKey string) string {
	h := sha1.New()
	io.WriteString(h, clientKey+wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// isWebSocketUpgrade reports whether r is a WebSocket upgrade request.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// wsConn is a minimal server-side WebSocket connection: it writes text frames
// to the client and drains/handles inbound control frames. The cockpit only
// pushes events, so reads exist solely to honor ping/close and to detect a
// disconnected client.
type wsConn struct {
	rw *bufio.ReadWriter
	c  io.Closer
}

// handshake upgrades the HTTP connection to WebSocket. It validates the request,
// hijacks the underlying TCP connection, and writes the 101 response. It returns
// an error if the request is not a valid upgrade or the server cannot hijack.
func handshake(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !isWebSocketUpgrade(r) {
		return nil, fmt.Errorf("not a websocket upgrade request")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("response writer does not support hijacking")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack: %w", err)
	}

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write handshake: %w", err)
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("flush handshake: %w", err)
	}
	return &wsConn{rw: rw, c: conn}, nil
}

// writeJSON marshals v and sends it as a single unfragmented text frame. Server
// frames are never masked (RFC 6455 §5.1).
func (c *wsConn) writeJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal ws payload: %w", err)
	}
	return c.writeFrame(opText, payload)
}

// writeFrame writes a single final frame with the given opcode and payload.
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	header := make([]byte, 0, 10)
	header = append(header, 0x80|opcode) // FIN=1, opcode

	n := len(payload)
	switch {
	case n <= 125:
		header = append(header, byte(n))
	case n <= 0xFFFF:
		header = append(header, 126)
		header = binary.BigEndian.AppendUint16(header, uint16(n))
	default:
		header = append(header, 127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}

	if _, err := c.rw.Write(header); err != nil {
		return fmt.Errorf("write ws header: %w", err)
	}
	if _, err := c.rw.Write(payload); err != nil {
		return fmt.Errorf("write ws payload: %w", err)
	}
	return c.rw.Flush()
}

// readLoop consumes inbound frames until the client closes or an error occurs.
// It answers pings with pongs and treats a close frame as a clean shutdown.
// Closing the returned-by-value done channel signals the writer to stop. The
// function never delivers application data because cockpit clients are
// receive-only beyond the subscribe query parameters.
func (c *wsConn) readLoop(done chan<- struct{}) {
	defer close(done)
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return
		}
		switch opcode {
		case opClose:
			c.writeFrame(opClose, nil)
			return
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return
			}
		}
	}
}

// readFrame reads a single frame, returning its opcode and unmasked payload.
// Client frames must be masked (RFC 6455 §5.1); the mask is applied on read.
func (c *wsConn) readFrame() (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.rw, h[:]); err != nil {
		return 0, nil, err
	}
	opcode := h[0] & 0x0F
	masked := h[1]&0x80 != 0
	length := uint64(h[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.rw, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.rw, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.rw, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.rw, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}

// Close releases the underlying connection.
func (c *wsConn) Close() error { return c.c.Close() }
