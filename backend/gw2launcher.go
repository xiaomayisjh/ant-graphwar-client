package backend

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Graphwar2LocalRoom struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Address    string `json:"address"`
	ListenHost string `json:"listenHost"`
	PID        int    `json:"pid"`
	Process    string `json:"process"`
}

type Graphwar2HostedRoom struct {
	LocalRoom Graphwar2LocalRoom       `json:"localRoom"`
	LobbyRoom Graphwar2Room            `json:"lobbyRoom"`
	Publisher Graphwar2PublisherStatus `json:"publisher"`
	Reason    string                   `json:"reason"`
}

func FindGraphwar2Executable() string {
	if env := strings.TrimSpace(os.Getenv("GW2_EXE")); env != "" {
		if p, err := existingFile(env); err == nil {
			return p
		}
	}
	starts := []string{}
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	seen := map[string]bool{}
	for _, start := range starts {
		dir, err := filepath.Abs(start)
		if err != nil {
			continue
		}
		for i := 0; i < 8 && dir != "" && dir != filepath.Dir(dir); i++ {
			candidates := []string{
				filepath.Join(dir, "Graphwar.exe"),
				filepath.Join(dir, "graphwar2", "Graphwar.exe"),
				filepath.Join(filepath.Dir(dir), "graphwar2", "Graphwar.exe"),
			}
			for _, candidate := range candidates {
				key := strings.ToLower(filepath.Clean(candidate))
				if seen[key] {
					continue
				}
				seen[key] = true
				if p, err := existingFile(candidate); err == nil {
					return p
				}
			}
			dir = filepath.Dir(dir)
		}
	}
	return ""
}

func StartGraphwar2Executable(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("Graphwar.exe not found")
	}
	path, err := existingFile(path)
	if err != nil {
		return err
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	return cmd.Start()
}
