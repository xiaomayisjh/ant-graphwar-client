package main

import (
	"context"
	"embed"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"graphwar-desktop/backend"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// App is bound to the frontend; it exposes backend controls to JS.
type App struct {
	ctx          context.Context
	backend      *backend.Backend
	started      bool
	lastErr      string
	bridge       *backend.Bridge
	bridgePort   int
	v2Bridge     *backend.V2Bridge
	v2BridgePort int
	translator   *backend.Translator
	gw2DLLInfo   backend.GW2DLLInfo
}

func NewApp() *App { return &App{translator: backend.NewTranslator()} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Always start the embedded WS<->TCP bridge so the WebView can reach the
	// OFFICIAL global server (raw TCP) and play with worldwide players.
	a.bridge = backend.NewBridge()
	if p, err := a.bridge.Start(); err != nil {
		log.Printf("bridge start failed: %v", err)
	} else {
		a.bridgePort = p
		log.Printf("embedded WS<->TCP bridge on 127.0.0.1:%d", p)
	}
	a.v2Bridge = backend.NewV2Bridge()
	if p, err := a.v2Bridge.Start(); err != nil {
		log.Printf("graphwar2 bridge start failed: %v", err)
	} else {
		a.v2BridgePort = p
		log.Printf("embedded Graphwar II bridge on 127.0.0.1:%d", p)
	}
	a.gw2DLLInfo = backend.DetectGraphwar2DLL()
	log.Printf("Graphwar II DLL capability: %s (%s)", a.gw2DLLInfo.Capability, a.gw2DLLInfo.Reason)
}

// BridgePort is the local WS port the frontend uses to reach raw-TCP servers
// (official lobby / rooms) via the embedded bridge. 0 if the bridge failed.
func (a *App) BridgePort() int { return a.bridgePort }

// V2BridgePort is the local WS port the frontend uses to reach Graphwar II
// raw TCP rooms. It relays JSON events over the game's u16be framed protocol.
func (a *App) V2BridgePort() int { return a.v2BridgePort }

func (a *App) Graphwar2DLLInfo() backend.GW2DLLInfo {
	if !a.gw2DLLInfo.Found {
		a.gw2DLLInfo = backend.DetectGraphwar2DLL()
	}
	return a.gw2DLLInfo
}

func (a *App) CreateGraphwar2Room(name string, port int, official bool) map[string]interface{} {
	info := a.Graphwar2DLLInfo()
	target := "local"
	if official {
		target = "official"
	}
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, 20*time.Second)
	defer cancel()
	room, publisher, err := backend.StartGraphwar2CompatRoomAndPublish(ctx, name, port, official)
	if err != nil {
		log.Printf("CreateGraphwar2Room failed target=%s: %v", target, err)
		return map[string]interface{}{
			"ok":         false,
			"supported":  true,
			"target":     target,
			"requested":  name,
			"port":       port,
			"capability": info.Capability,
			"dllPath":    info.Path,
			"error":      err.Error(),
		}
	}
	local := backend.Graphwar2LocalRoom{
		Host:       "::1",
		Port:       room.Port(),
		Address:    net.JoinHostPort("::1", strconv.Itoa(room.Port())),
		ListenHost: "::",
		Process:    "GraphwarDesktop",
	}
	var hosted backend.Graphwar2HostedRoom
	var verifyErr error
	officialListed := false
	if official {
		hosted, verifyErr = backend.VerifyGraphwar2OfficialPublication(ctx, []backend.Graphwar2LocalRoom{local}, publisher, 5)
		officialListed = verifyErr == nil
		if verifyErr != nil {
			log.Printf("CreateGraphwar2Room official verification pending: %v", verifyErr)
		}
	}
	res := map[string]interface{}{
		"ok":             true,
		"supported":      true,
		"target":         target,
		"requested":      name,
		"port":           room.Port(),
		"capability":     info.Capability,
		"dllPath":        info.Path,
		"rooms":          []backend.Graphwar2LocalRoom{local},
		"publisher":      publisher,
		"hosted":         hosted,
		"officialListed": officialListed,
		"reason":         "Started embedded Graphwar II compatible RoomServer.",
		"nextStep":       "Join the room from this client or official Graphwar II. Official mode also publishes GameInfo/GameHeartbeat to the broker.",
	}
	if verifyErr != nil {
		res["verifyError"] = verifyErr.Error()
	}
	return res
}

func (a *App) Graphwar2LocalRooms() map[string]interface{} {
	rooms, err := backend.DetectGraphwar2LocalRooms()
	if err != nil {
		log.Printf("Graphwar II local room detect failed: %v", err)
		return map[string]interface{}{"ok": false, "error": err.Error(), "rooms": rooms}
	}
	return map[string]interface{}{"ok": true, "rooms": rooms}
}

func (a *App) Graphwar2HostedOfficialRoom() map[string]interface{} {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, 25*time.Second)
	defer cancel()
	room, err := backend.DetectGraphwar2HostedOfficialRoom(ctx)
	res := map[string]interface{}{"ok": err == nil, "hosted": room}
	if err != nil {
		log.Printf("Graphwar II official hosted room detect failed: %v", err)
		res["error"] = err.Error()
	}
	return res
}

func (a *App) PublishGraphwar2OfficialRoom(name, host string, port int) map[string]interface{} {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, 20*time.Second)
	defer cancel()
	st, err := backend.DefaultGraphwar2Publisher().Start(ctx, backend.Graphwar2PublishOptions{
		RoomName: name,
		Host:     host,
		Port:     port,
	})
	res := map[string]interface{}{"ok": err == nil, "status": st}
	if err != nil {
		log.Printf("Graphwar II official publish failed: %v", err)
		res["error"] = err.Error()
	}
	return res
}

func (a *App) StopGraphwar2OfficialPublisher() bool {
	backend.DefaultGraphwar2Publisher().Stop()
	return true
}

func (a *App) Graphwar2OfficialPublisherStatus() backend.Graphwar2PublisherStatus {
	return backend.DefaultGraphwar2Publisher().Status()
}

func (a *App) CpolarConfigPath() string { return backend.DefaultCpolarManager().ConfigPath() }

func (a *App) CpolarStatus() backend.CpolarStatus {
	return backend.DefaultCpolarManager().Status()
}

func (a *App) CpolarInitAccounts() map[string]interface{} {
	st, err := backend.DefaultCpolarManager().InitAccounts()
	res := map[string]interface{}{"ok": err == nil, "status": st}
	if err != nil {
		res["error"] = err.Error()
	}
	return res
}

func (a *App) CpolarStartTCP(label string, localPort int) map[string]interface{} {
	info, err := backend.DefaultCpolarManager().StartTCP(label, localPort)
	res := map[string]interface{}{"ok": err == nil, "tunnel": info}
	if err != nil {
		res["error"] = err.Error()
	}
	return res
}

func (a *App) CpolarStartTCPForTarget(label, localTarget string) map[string]interface{} {
	info, err := backend.DefaultCpolarManager().StartTCPForTarget(label, localTarget)
	res := map[string]interface{}{"ok": err == nil, "tunnel": info}
	if err != nil {
		res["error"] = err.Error()
	}
	return res
}

func (a *App) CpolarStop(id string) bool { return backend.DefaultCpolarManager().Stop(id) }

func (a *App) CpolarStopAll() bool { return backend.DefaultCpolarManager().StopAll() }

func (a *App) Graphwar2LobbyRooms() map[string]interface{} {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, 25*time.Second)
	defer cancel()
	rooms, err := backend.FetchGraphwar2Rooms(ctx)
	if err != nil {
		log.Printf("Graphwar II lobby fetch failed: %v", err)
		return map[string]interface{}{"ok": false, "error": err.Error(), "rooms": []backend.Graphwar2Room{}}
	}
	return map[string]interface{}{"ok": true, "rooms": rooms}
}

func (a *App) ProbeGraphwar2Room(host string, port int) backend.Graphwar2RoomProbeResult {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, 6*time.Second)
	defer cancel()
	return backend.ProbeGraphwar2Room(ctx, host, port)
}

func (a *App) V2DebugLogPath() string { return backend.V2DebugLogPath() }

func (a *App) V2DebugEnabled() bool { return backend.V2DebugEnabled() }

func (a *App) SetV2DebugEnabled(enabled bool) bool {
	backend.SetV2DebugEnabled(enabled)
	if enabled {
		backend.V2Debugf("[app] debug log enabled")
	}
	return backend.V2DebugEnabled()
}

func (a *App) ClearV2DebugLog() bool {
	if err := backend.ClearV2DebugLog(); err != nil {
		log.Printf("clear graphwar2 debug log failed: %v", err)
		return false
	}
	backend.V2Debugf("[app] debug log cleared")
	return true
}

func (a *App) AppendV2DebugLog(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	if len(line) > 12000 {
		line = line[:12000] + "...<truncated>"
	}
	backend.V2Debugf("[frontend] %s", line)
	return true
}

// CreateRoom hosts a new room relay locally and returns its bound port. Pass
// port 0 for an OS-assigned local port (LAN/loopback play), or a specific port
// you've port-forwarded so the official lobby probe + remote players reach it.
// The frontend registers the returned port with the lobby via CREATE_ROOM.
// Returns 0 on failure.
func (a *App) CreateRoom(name string, port int) int {
	p, err := backend.StartStandaloneRoomNamed(port, name)
	if err != nil {
		log.Printf("CreateRoom failed: %v", err)
		return 0
	}
	log.Printf("standalone room hosted on port %d (%q)", p, name)
	return p
}

// ========================= room management API =========================
// All operate on a room this app hosts, identified by its bound port.

func (a *App) AdminHostedPorts() []int { return backend.HostedPorts() }

func (a *App) AdminRoomStatus(port int) map[string]interface{} {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return map[string]interface{}{"ok": false}
	}
	st := gs.Status()
	return map[string]interface{}{"ok": true, "name": st.Name, "port": st.Port,
		"players": st.Players, "clients": st.Clients, "gameMode": st.GameMode,
		"inGame": st.InGame, "locked": st.Locked, "maxClients": st.MaxClients}
}

// AdminListPlayers returns connected players (with IP) for a hosted room.
func (a *App) AdminListPlayers(port int) []map[string]interface{} {
	gs := backend.HostedRoom(port)
	out := []map[string]interface{}{}
	if gs == nil {
		return out
	}
	for _, p := range gs.ListPlayers() {
		out = append(out, map[string]interface{}{"id": p.ID, "name": p.Name, "team": p.Team,
			"ip": p.IP, "leader": p.Leader, "ready": p.Ready, "soldiers": p.Soldiers,
			"computer": p.Computer, "level": p.Level})
	}
	return out
}

func (a *App) AdminAddComputerPlayer(port int, name string, level int) map[string]interface{} {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return map[string]interface{}{"ok": false, "error": "room not hosted by this app"}
	}
	if _, ok := gs.AddComputerPlayer(name, level); !ok {
		return map[string]interface{}{"ok": false, "error": "cannot add computer player"}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) AdminKick(port, playerID int) bool {
	gs := backend.HostedRoom(port)
	return gs != nil && gs.KickPlayer(playerID)
}
func (a *App) AdminBanPlayer(port, playerID int, alsoIP bool) bool {
	gs := backend.HostedRoom(port)
	return gs != nil && gs.BanPlayer(playerID, alsoIP)
}
func (a *App) AdminBanName(port int, name string) bool {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return false
	}
	gs.BanName(name)
	return true
}
func (a *App) AdminBanIP(port int, ip string) bool {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return false
	}
	gs.BanIP(ip)
	return true
}
func (a *App) AdminUnban(port int, nameOrIP string) bool {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return false
	}
	gs.Unban(nameOrIP)
	return true
}
func (a *App) AdminBans(port int) map[string]interface{} {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return map[string]interface{}{"names": []string{}, "ips": []string{}}
	}
	names, ips := gs.Bans()
	if names == nil {
		names = []string{}
	}
	if ips == nil {
		ips = []string{}
	}
	return map[string]interface{}{"names": names, "ips": ips}
}
func (a *App) AdminSetLocked(port int, locked bool) bool {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return false
	}
	gs.SetLocked(locked)
	return true
}
func (a *App) AdminSetMaxClients(port, n int) bool {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return false
	}
	gs.SetMaxClients(n)
	return true
}
func (a *App) AdminForceReset(port int) bool {
	gs := backend.HostedRoom(port)
	if gs == nil {
		return false
	}
	gs.ForceReset()
	return true
}

// Translate proxies a chat message through the YouDao translate service.
// `to` is the target language (e.g. "zh-CHS", "en"); source is auto-detected.
// Returns the translated text, or "ERR:<msg>" on failure (frontend shows it).
func (a *App) Translate(text, to string) string {
	if a.translator == nil {
		a.translator = backend.NewTranslator()
	}
	out, err := a.translator.Translate(text, to)
	if err != nil {
		return "ERR:" + err.Error()
	}
	return out
}

// StartBackend launches the embedded lobby + rooms on OS-assigned free ports.
// publicIP is what clients should dial to reach this machine ("" = 127.0.0.1).
// Returns the actual lobby port on success, or 0 + sets lastErr on failure.
// Idempotent: a second call returns the already-bound lobby port.
func (a *App) StartBackend(publicIP string) int {
	return a.StartBackendOn(publicIP, 0, 0)
}

// StartBackendOn is StartBackend with explicit lobby/room ports (0 = auto).
// roomBase 0 => rooms get OS-assigned ports; otherwise rooms use roomBase+i.
func (a *App) StartBackendOn(publicIP string, lobbyPort, roomBase int) int {
	if a.started {
		return a.backend.LobbyPort()
	}
	a.backend = backend.NewBackend(publicIP)
	a.backend.GlobalPort = lobbyPort
	a.backend.RoomBase = roomBase
	if err := a.backend.Start(); err != nil {
		a.lastErr = err.Error()
		log.Printf("embedded backend start failed: %v", err)
		return 0
	}
	a.started = true
	log.Printf("embedded backend started (lobby %d, rooms %v, ip %s)",
		a.backend.LobbyPort(), a.backend.RoomPorts(), a.backend.PublicIP)
	return a.backend.LobbyPort()
}

// LastError returns the last backend start error (empty if none).
func (a *App) LastError() string { return a.lastErr }

// BackendInfo reports the embedded backend's actual ports for the frontend.
func (a *App) BackendInfo() map[string]interface{} {
	info := map[string]interface{}{"started": a.started, "error": a.lastErr}
	if a.backend != nil {
		info["lobbyPort"] = a.backend.LobbyPort()
		info["roomPorts"] = a.backend.RoomPorts()
	}
	return info
}

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:  "Graphwar Desktop",
		Width:  1100,
		Height: 760,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 7, G: 13, B: 18, A: 1},
		OnStartup:        app.startup,
		OnShutdown: func(ctx context.Context) {
			backend.DefaultCpolarManager().StopAll()
		},
		Bind: []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
