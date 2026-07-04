//go:build windows

package backend

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func DetectGraphwar2LocalRooms() ([]Graphwar2LocalRoom, error) {
	pids, err := graphwar2ProcessPIDs()
	if len(pids) == 0 {
		if err != nil {
			return graphwar2CompatLocalRooms(), err
		}
		return graphwar2CompatLocalRooms(), nil
	}
	rooms, scanErr := graphwar2TCPListeners(pids)
	rooms = append(rooms, graphwar2CompatLocalRooms()...)
	if scanErr != nil {
		return rooms, scanErr
	}
	return rooms, nil
}

func graphwar2ProcessPIDs() (map[int]string, error) {
	wanted := map[string]bool{"graphwar.exe": true}
	if exe := FindGraphwar2Executable(); exe != "" {
		wanted[strings.ToLower(filepath.Base(exe))] = true
	}
	cmd := exec.Command("tasklist", "/FO", "CSV", "/NH")
	hideChildWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return map[int]string{}, err
	}
	rd := csv.NewReader(bytes.NewReader(out))
	rd.FieldsPerRecord = -1
	rd.LazyQuotes = true
	rows, err := rd.ReadAll()
	if err != nil {
		return map[int]string{}, err
	}
	pids := map[int]string{}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		name := strings.TrimSpace(row[0])
		lower := strings.ToLower(name)
		if !wanted[lower] {
			continue
		}
		pid, convErr := strconv.Atoi(strings.TrimSpace(row[1]))
		if convErr != nil || pid <= 0 {
			continue
		}
		pids[pid] = name
	}
	return pids, nil
}

func graphwar2TCPListeners(pids map[int]string) ([]Graphwar2LocalRoom, error) {
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	hideChildWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return []Graphwar2LocalRoom{}, err
	}
	return parseGraphwar2Netstat(string(out), pids), nil
}

func parseGraphwar2Netstat(raw string, pids map[int]string) []Graphwar2LocalRoom {
	seen := map[string]bool{}
	rooms := []Graphwar2LocalRoom{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		state := fields[len(fields)-2]
		if !strings.EqualFold(state, "LISTENING") {
			continue
		}
		pid, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil || pids[pid] == "" {
			continue
		}
		listenHost, port, ok := parseNetstatAddr(fields[1])
		if !ok || port <= 0 || port > 65535 {
			continue
		}
		host := localDialHost(listenHost)
		key := fmt.Sprintf("%d/%s/%d", pid, host, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		rooms = append(rooms, Graphwar2LocalRoom{
			Host:       host,
			Port:       port,
			Address:    net.JoinHostPort(host, strconv.Itoa(port)),
			ListenHost: listenHost,
			PID:        pid,
			Process:    pids[pid],
		})
	}
	sort.SliceStable(rooms, func(i, j int) bool {
		if rooms[i].Port == rooms[j].Port {
			return rooms[i].PID < rooms[j].PID
		}
		return rooms[i].Port < rooms[j].Port
	})
	return rooms
}

func parseNetstatAddr(addr string) (string, int, bool) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		idx := strings.LastIndex(addr, ":")
		if idx < 0 {
			return "", 0, false
		}
		host = addr[:idx]
		portText = addr[idx+1:]
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	port, err := strconv.Atoi(strings.TrimSpace(portText))
	if err != nil {
		return "", 0, false
	}
	return host, port, true
}

func localDialHost(listenHost string) string {
	host := strings.Trim(strings.TrimSpace(listenHost), "[]")
	switch host {
	case "", "::", "*":
		return "::1"
	case "0.0.0.0":
		return "127.0.0.1"
	default:
		return host
	}
}
