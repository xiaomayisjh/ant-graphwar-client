package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

type Graphwar2RoomProbeResult struct {
	OK      bool     `json:"ok"`
	Host    string   `json:"host"`
	Port    int      `json:"port"`
	Events  []string `json:"events"`
	Error   string   `json:"error,omitempty"`
	Address string   `json:"address"`
}

func ProbeGraphwar2Room(ctx context.Context, host string, port int) Graphwar2RoomProbeResult {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	res := Graphwar2RoomProbeResult{Host: host, Port: port}
	if host == "" || port <= 0 || port > 65535 {
		res.Error = "bad Graphwar II room address"
		return res
	}
	res.Address = net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", res.Address)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}
	payload, err := graphwar2BrokerConnectionRequestPayload()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if err := writeV2Frame(conn, payload); err != nil {
		res.Error = err.Error()
		return res
	}
	_ = writeV2Frame(conn, []byte(`{"TickRequest":{}}`))
	for len(res.Events) < 8 {
		frame, err := readV2Frame(conn)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				break
			}
			if len(res.Events) == 0 {
				res.Error = err.Error()
			}
			break
		}
		kind := graphwar2EventKind(frame)
		if kind == "" {
			var raw map[string]interface{}
			if json.Unmarshal(frame, &raw) == nil {
				for k := range raw {
					kind = k
					break
				}
			}
		}
		if kind == "" {
			kind = "Unknown"
		}
		res.Events = append(res.Events, kind)
		if kind == "ConnectedToServer" || kind == "NewConnection" || kind == "TickReply" || kind == "GameInfo" {
			res.OK = true
			res.Error = ""
			return res
		}
	}
	if !res.OK && res.Error == "" {
		if len(res.Events) == 0 {
			res.Error = "no Graphwar II response"
		} else {
			res.Error = "unexpected Graphwar II events: " + strings.Join(res.Events, ",")
		}
	}
	return res
}
