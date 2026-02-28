package ws

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// Conn is a minimal RFC6455 websocket client for reading server text frames.
// It is intentionally small (no permessage-deflate, no advanced options) and
// is built so this repo can run in offline build environments.
type Conn struct {
	rwc net.Conn
	r   *bufio.Reader
	// write operations are rare (pong/close). We keep it simple.
}

func Dial(ctx context.Context, urlStr string, timeout time.Duration) (*Conn, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	d := net.Dialer{Timeout: timeout}
	var c net.Conn
	if u.Scheme == "wss" {
		cfg := &tls.Config{ServerName: strings.Split(u.Host, ":")[0]}
		c, err = tls.DialWithDialer(&d, "tcp", host, cfg)
	} else {
		c, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	cn := &Conn{rwc: c, r: bufio.NewReader(c)}

	key, err := randKey()
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	// Handshake
	req := strings.Join([]string{
		fmt.Sprintf("GET %s HTTP/1.1", path),
		fmt.Sprintf("Host: %s", u.Host),
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Version: 13",
		"Sec-WebSocket-Key: " + key,
		"User-Agent: go-machine/ws",
		"\r\n",
	}, "\r\n")
	_ = c.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(c, req); err != nil {
		_ = c.Close()
		return nil, err
	}
	_ = c.SetWriteDeadline(time.Time{})

	// Read response headers
	status, headers, err := readHTTPResponse(cn.r)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if status != 101 {
		_ = c.Close()
		return nil, fmt.Errorf("ws handshake failed: status %d", status)
	}
	accept := headers["sec-websocket-accept"]
	if accept == "" {
		_ = c.Close()
		return nil, errors.New("ws handshake missing Sec-WebSocket-Accept")
	}
	if accept != computeAccept(key) {
		_ = c.Close()
		return nil, errors.New("ws handshake invalid Sec-WebSocket-Accept")
	}

	return cn, nil
}

func (c *Conn) Close() error { return c.rwc.Close() }

// ReadText blocks until it reads a TEXT frame. It automatically responds to PING.
// It ignores continuation/binary frames.
func (c *Conn) ReadText(deadline time.Duration) ([]byte, error) {
	if deadline > 0 {
		_ = c.rwc.SetReadDeadline(time.Now().Add(deadline))
	}
	defer func() { _ = c.rwc.SetReadDeadline(time.Time{}) }()
	for {
		op, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x1: // text
			return payload, nil
		case 0x9: // ping
			_ = c.writeControl(0xA, payload) // pong
			continue
		case 0xA: // pong
			continue
		case 0x8: // close
			_ = c.writeControl(0x8, nil)
			return nil, io.EOF
		default:
			continue
		}
	}
}

// --- internals ---

func randKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func computeAccept(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.Sum([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h[:])
}

func readHTTPResponse(r *bufio.Reader) (status int, headers map[string]string, err error) {
	headers = map[string]string{}
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, nil, err
	}
	line = strings.TrimSpace(line)
	parts := strings.Split(line, " ")
	if len(parts) < 2 {
		return 0, nil, errors.New("bad http status line")
	}
	status, _ = atoi(parts[1])
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return 0, nil, err
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "" {
			break
		}
		kv := strings.SplitN(l, ":", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		headers[k] = v
	}
	return status, headers, nil
}

func atoi(s string) (int, error) {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errors.New("not int")
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

func (c *Conn) readFrame() (opcode byte, payload []byte, err error) {
	// First 2 bytes
	b1, err := c.r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	b2, err := c.r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	fin := (b1 & 0x80) != 0
	_ = fin // we ignore fragmentation for now
	opcode = b1 & 0x0F
	masked := (b2 & 0x80) != 0
	ln := int64(b2 & 0x7F)
	if ln == 126 {
		buf := make([]byte, 2)
		if _, err := io.ReadFull(c.r, buf); err != nil {
			return 0, nil, err
		}
		ln = int64(buf[0])<<8 | int64(buf[1])
	} else if ln == 127 {
		buf := make([]byte, 8)
		if _, err := io.ReadFull(c.r, buf); err != nil {
			return 0, nil, err
		}
		ln = 0
		for i := 0; i < 8; i++ {
			ln = (ln << 8) | int64(buf[i])
		}
	}
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.r, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}
	if ln < 0 || ln > (8<<20) {
		return 0, nil, errors.New("frame too large")
	}
	payload = make([]byte, ln)
	if ln > 0 {
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return 0, nil, err
		}
	}
	if masked {
		for i := int64(0); i < ln; i++ {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}

func (c *Conn) writeControl(opcode byte, payload []byte) error {
	// Client-to-server frames MUST be masked.
	ln := len(payload)
	if ln > 125 {
		payload = payload[:125]
		ln = 125
	}
	// header
	first := byte(0x80 | (opcode & 0x0F))
	second := byte(0x80 | byte(ln))
	buf := []byte{first, second}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	buf = append(buf, mask[:]...)
	masked := make([]byte, ln)
	for i := 0; i < ln; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	buf = append(buf, masked...)
	_, err := c.rwc.Write(buf)
	return err
}
