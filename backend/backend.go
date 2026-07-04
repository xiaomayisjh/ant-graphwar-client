package backend

import (
	"net/url"
	"strings"
	"sync"
)

// hostedRooms tracks every GraphServer this app is hosting (standalone rooms +
// pool rooms), keyed by bound port, so the admin UI can manage them by port.
var hostedRooms = struct {
	mu sync.Mutex
	m  map[int]*GraphServer
}{m: map[int]*GraphServer{}}

func registerHosted(gs *GraphServer) {
	hostedRooms.mu.Lock()
	hostedRooms.m[gs.Port()] = gs
	hostedRooms.mu.Unlock()
}

// HostedRoom returns the GraphServer hosted on `port`, or nil.
func HostedRoom(port int) *GraphServer {
	hostedRooms.mu.Lock()
	defer hostedRooms.mu.Unlock()
	return hostedRooms.m[port]
}

// HostedPorts lists all ports this app is currently hosting rooms on.
func HostedPorts() []int {
	hostedRooms.mu.Lock()
	defer hostedRooms.mu.Unlock()
	var out []int
	for p := range hostedRooms.m {
		out = append(out, p)
	}
	return out
}

// Backend bundles the lobby + a pool of room relays, mirroring server/index.js.
type Backend struct {
	GlobalPort int
	RoomBase   int
	NumRooms   int
	PublicIP   string
	lobby      *GlobalServer
	rooms      []*managedRoom
}

type managedRoom struct {
	name      string
	port      int
	server    *GraphServer
	lobbyRoom *lobbyRoom
	lobby     *GlobalServer
}

// NewBackend creates a backend. Ports default to 0 = OS-assigned free ports
// (avoids "address already in use" when 23761/6112 are taken). Use
// LobbyPort()/RoomPorts() after Start() to learn the actual ports.
func NewBackend(publicIP string) *Backend {
	if publicIP == "" {
		publicIP = "127.0.0.1"
	}
	return &Backend{GlobalPort: 0, RoomBase: 0, NumRooms: 3, PublicIP: publicIP}
}

// Start launches the lobby and room pool. Returns the first error.
func (b *Backend) Start() error {
	b.lobby = NewGlobalServer(b.GlobalPort, b.PublicIP)
	if err := b.lobby.Listen(); err != nil {
		return err
	}
	for i := 0; i < b.NumRooms; i++ {
		// roomPort 0 (or RoomBase+i) -> OS-assigned when 0.
		rp := 0
		if b.RoomBase != 0 {
			rp = b.RoomBase + i
		}
		mr := b.newManagedRoom("Public Room "+itoa(i), rp)
		if err := mr.start(); err != nil {
			return err
		}
		b.rooms = append(b.rooms, mr)
	}
	return nil
}

// StartStandaloneRoom starts a single room relay (GraphServer). Pass port 0 for
// an OS-assigned port (local/LAN play), or a specific port you've port-forwarded
// so the OFFICIAL lobby's reachability probe + remote players can reach it.
// Returns the actual bound port. The room is NOT auto-registered with any lobby.
func StartStandaloneRoom() (int, error) {
	return StartStandaloneRoomOn(0)
}

func StartStandaloneRoomOn(port int) (int, error) {
	return StartStandaloneRoomNamed(port, "")
}

// StartStandaloneRoomNamed starts a room with a display name and registers it
// in the hosted-room registry so it can be managed by port.
func StartStandaloneRoomNamed(port int, name string) (int, error) {
	gs := NewGraphServer(port, Hooks{})
	gs.name = name
	if err := gs.Listen(); err != nil {
		return 0, err
	}
	registerHosted(gs)
	return gs.Port(), nil
}

// RoomPorts lists the actual bound room ports (after Start).
func (b *Backend) RoomPorts() []int {
	ports := make([]int, 0, len(b.rooms))
	for _, mr := range b.rooms {
		ports = append(ports, mr.port)
	}
	return ports
}

// LobbyPort is the actual bound lobby port (after Start).
func (b *Backend) LobbyPort() int {
	if b.lobby == nil {
		return 0
	}
	return b.lobby.Port()
}

func (b *Backend) newManagedRoom(name string, port int) *managedRoom {
	mr := &managedRoom{name: name, port: port, lobby: b.lobby}
	mr.server = NewGraphServer(port, Hooks{
		OnStatus: func(mode, num int) { mr.onStatus(mode, num) },
		OnStart:  func() { mr.onHide() },
		OnFinish: func() { mr.onRecreate() },
		OnEmpty:  func() { mr.onRecreate() },
	})
	mr.server.name = name
	return mr
}

func (mr *managedRoom) start() error {
	if err := mr.server.Listen(); err != nil {
		return err
	}
	mr.port = mr.server.Port() // capture OS-assigned port
	registerHosted(mr.server)
	mr.register()
	return nil
}
func (mr *managedRoom) register() {
	// URL-encode the name like the Java/JS client (spaces -> +).
	enc := strings.ReplaceAll(url.QueryEscape(mr.name), "%20", "+")
	mr.lobbyRoom = mr.lobby.RegisterLocalRoom(enc, mr.lobby.publicIP, mr.port)
}
func (mr *managedRoom) onStatus(mode, num int) {
	if mr.lobbyRoom != nil {
		mr.lobby.UpdateLocalRoom(mr.lobbyRoom, mode, num)
	}
}
func (mr *managedRoom) onHide() {
	if mr.lobbyRoom != nil {
		mr.lobby.RemoveLocalRoom(mr.lobbyRoom)
		mr.lobbyRoom = nil
	}
}
func (mr *managedRoom) onRecreate() {
	if mr.lobbyRoom == nil {
		mr.register()
	}
}
