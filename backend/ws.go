package backend

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
)

// Minimal RFC6455 server-side framing: read masked client frames, write
// unmasked server text frames. Enough for the line protocol (text only).

func wsWriteText(conn net.Conn, s string) error {
	payload := []byte(s)
	n := len(payload)
	var header []byte
	switch {
	case n < 126:
		header = []byte{0x81, byte(n)}
	case n < 65536:
		header = []byte{0x81, 126, byte(n >> 8), byte(n)}
	default:
		header = make([]byte, 10)
		header[0] = 0x81
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(n))
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func wsReadLoop(br *bufio.Reader, lc *LineConn) {
	for {
		text, opcode, err := wsReadFrame(br)
		if err != nil {
			lc.Close()
			return
		}
		switch opcode {
		case 0x8: // close
			lc.Close()
			return
		case 0x9: // ping -> pong
			wsWritePong(lc.conn)
		case 0x1, 0x0: // text / continuation
			if len(text) > MaxLineLen {
				lc.Close()
				return
			}
			lc.touchRecv()
			if lc.onLine != nil {
				// frontend may include trailing newline; strip
				t := text
				for len(t) > 0 && (t[len(t)-1] == '\n' || t[len(t)-1] == '\r') {
					t = t[:len(t)-1]
				}
				lc.onLine(t)
			}
		}
	}
}

func wsReadFrame(br *bufio.Reader) (string, byte, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(br, h); err != nil {
		return "", 0, err
	}
	opcode := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	length := int(h[1] & 0x7f)
	if length == 126 {
		ext := make([]byte, 2)
		if _, err := io.ReadFull(br, ext); err != nil {
			return "", 0, err
		}
		length = int(binary.BigEndian.Uint16(ext))
	} else if length == 127 {
		ext := make([]byte, 8)
		if _, err := io.ReadFull(br, ext); err != nil {
			return "", 0, err
		}
		n := binary.BigEndian.Uint64(ext)
		if n > uint64(MaxWSFrameLen) {
			return "", 0, errors.New("websocket frame too large")
		}
		length = int(n)
	}
	if length > MaxWSFrameLen {
		return "", 0, errors.New("websocket frame too large")
	}
	if (opcode == 0x8 || opcode == 0x9 || opcode == 0xA) && length > 125 {
		return "", 0, errors.New("websocket control frame too large")
	}
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(br, mask); err != nil {
			return "", 0, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return "", 0, err
	}
	if masked {
		for i := 0; i < length; i++ {
			payload[i] ^= mask[i&3]
		}
	}
	return string(payload), opcode, nil
}

func wsWritePong(conn net.Conn) {
	conn.Write([]byte{0x8a, 0x00})
}
