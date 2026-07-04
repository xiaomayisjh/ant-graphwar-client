package backend

import (
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	GW2DLLCapabilityMissing         = "Missing"
	GW2DLLCapabilityGDExtensionOnly = "GDExtensionOnly"
	GW2DLLCapabilityNativeABI       = "NativeABI"

	gw2GDExtensionEntry = "gdext_rust_init"
)

// GW2DLLInfo describes whether graphwarrust.dll can be used directly by the
// Go client. Graphwar II ships it as a Godot GDExtension, not as a stable SDK.
type GW2DLLInfo struct {
	Found             bool       `json:"found"`
	Path              string     `json:"path,omitempty"`
	Capability        string     `json:"capability"`
	DirectCallable    bool       `json:"directCallable"`
	RequiresGodotHost bool       `json:"requiresGodotHost"`
	EntrySymbol       string     `json:"entrySymbol,omitempty"`
	Exports           []PEExport `json:"exports,omitempty"`
	Reason            string     `json:"reason"`
}

type PEExport struct {
	Name    string `json:"name"`
	Ordinal uint32 `json:"ordinal"`
	RVA     uint32 `json:"rva"`
}

func DetectGraphwar2DLL() GW2DLLInfo {
	path, err := FindGraphwar2DLL("")
	if err != nil {
		return GW2DLLInfo{
			Found:      false,
			Capability: GW2DLLCapabilityMissing,
			Reason:     err.Error(),
		}
	}
	info, err := InspectGraphwar2DLL(path)
	if err != nil {
		return GW2DLLInfo{
			Found:      true,
			Path:       path,
			Capability: GW2DLLCapabilityMissing,
			Reason:     err.Error(),
		}
	}
	return info
}

func FindGraphwar2DLL(explicit string) (string, error) {
	if explicit != "" {
		return existingFile(explicit)
	}
	if env := strings.TrimSpace(os.Getenv("GW2_DLL")); env != "" {
		return existingFile(env)
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
				filepath.Join(dir, "graphwarrust.dll"),
				filepath.Join(dir, "graphwar2", "graphwarrust.dll"),
				filepath.Join(filepath.Dir(dir), "graphwar2", "graphwarrust.dll"),
				filepath.Join(dir, "rust", "target", "x86_64-pc-windows-gnu", "release", "graphwarrust.dll"),
			}
			for _, candidate := range candidates {
				key := strings.ToLower(filepath.Clean(candidate))
				if seen[key] {
					continue
				}
				seen[key] = true
				if p, err := existingFile(candidate); err == nil {
					return p, nil
				}
			}
			dir = filepath.Dir(dir)
		}
	}
	return "", errors.New("graphwarrust.dll not found; set GW2_DLL or place it next to the graphwar2 assets")
}

func InspectGraphwar2DLL(path string) (GW2DLLInfo, error) {
	path, err := existingFile(path)
	if err != nil {
		return GW2DLLInfo{}, err
	}
	exports, err := ReadPEExports(path)
	if err != nil {
		return GW2DLLInfo{}, err
	}
	info := GW2DLLInfo{
		Found:   true,
		Path:    path,
		Exports: exports,
	}
	if len(exports) == 1 && exports[0].Name == gw2GDExtensionEntry {
		info.Capability = GW2DLLCapabilityGDExtensionOnly
		info.DirectCallable = false
		info.RequiresGodotHost = true
		info.EntrySymbol = gw2GDExtensionEntry
		info.Reason = "graphwarrust.dll only exports the Godot GDExtension initializer; game APIs must be reached through a Godot host or the JSON room protocol"
		return info, nil
	}
	for _, ex := range exports {
		if ex.Name == gw2GDExtensionEntry {
			info.EntrySymbol = gw2GDExtensionEntry
			break
		}
	}
	info.Capability = GW2DLLCapabilityNativeABI
	info.DirectCallable = len(exports) > 0
	info.RequiresGodotHost = false
	info.Reason = "graphwarrust.dll exposes additional exports; review their ABI before calling from Go"
	return info, nil
}

func ReadPEExports(path string) ([]PEExport, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	exportRVA, exportSize, err := exportDirectory(f)
	if err != nil {
		return nil, err
	}
	if exportRVA == 0 || exportSize == 0 {
		return nil, nil
	}
	dir, err := readAtRVA(f, exportRVA, 40)
	if err != nil {
		return nil, err
	}
	base := binary.LittleEndian.Uint32(dir[16:20])
	numberOfFunctions := binary.LittleEndian.Uint32(dir[20:24])
	numberOfNames := binary.LittleEndian.Uint32(dir[24:28])
	addressOfFunctions := binary.LittleEndian.Uint32(dir[28:32])
	addressOfNames := binary.LittleEndian.Uint32(dir[32:36])
	addressOfNameOrdinals := binary.LittleEndian.Uint32(dir[36:40])
	if numberOfNames == 0 {
		return nil, nil
	}
	if numberOfNames > 4096 || numberOfFunctions > 65536 {
		return nil, fmt.Errorf("suspicious export table size: names=%d funcs=%d", numberOfNames, numberOfFunctions)
	}

	nameRVAs, err := readAtRVA(f, addressOfNames, numberOfNames*4)
	if err != nil {
		return nil, err
	}
	ordinals, err := readAtRVA(f, addressOfNameOrdinals, numberOfNames*2)
	if err != nil {
		return nil, err
	}
	funcRVAs, err := readAtRVA(f, addressOfFunctions, numberOfFunctions*4)
	if err != nil {
		return nil, err
	}

	exports := make([]PEExport, 0, numberOfNames)
	for i := uint32(0); i < numberOfNames; i++ {
		nameRVA := binary.LittleEndian.Uint32(nameRVAs[i*4 : i*4+4])
		name, err := readCStringAtRVA(f, nameRVA, 512)
		if err != nil {
			return nil, err
		}
		ordinalIndex := uint32(binary.LittleEndian.Uint16(ordinals[i*2 : i*2+2]))
		if ordinalIndex >= numberOfFunctions {
			return nil, fmt.Errorf("export %q ordinal index %d outside function table", name, ordinalIndex)
		}
		funcRVA := binary.LittleEndian.Uint32(funcRVAs[ordinalIndex*4 : ordinalIndex*4+4])
		exports = append(exports, PEExport{
			Name:    name,
			Ordinal: base + ordinalIndex,
			RVA:     funcRVA,
		})
	}
	return exports, nil
}

func existingFile(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	return abs, nil
}

func exportDirectory(f *pe.File) (uint32, uint32, error) {
	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if len(h.DataDirectory) == 0 {
			return 0, 0, nil
		}
		return h.DataDirectory[0].VirtualAddress, h.DataDirectory[0].Size, nil
	case *pe.OptionalHeader64:
		if len(h.DataDirectory) == 0 {
			return 0, 0, nil
		}
		return h.DataDirectory[0].VirtualAddress, h.DataDirectory[0].Size, nil
	default:
		return 0, 0, errors.New("unsupported PE optional header")
	}
}

func readAtRVA(f *pe.File, rva uint32, size uint32) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	section := sectionForRVA(f, rva, size)
	if section == nil {
		return nil, fmt.Errorf("RVA 0x%x size 0x%x is outside PE sections", rva, size)
	}
	data, err := section.Data()
	if err != nil {
		return nil, err
	}
	off := rva - section.VirtualAddress
	if uint64(off)+uint64(size) > uint64(len(data)) {
		return nil, fmt.Errorf("RVA 0x%x size 0x%x exceeds section %s", rva, size, section.Name)
	}
	out := make([]byte, size)
	copy(out, data[off:off+size])
	return out, nil
}

func readCStringAtRVA(f *pe.File, rva uint32, limit uint32) (string, error) {
	section := sectionForRVA(f, rva, 1)
	if section == nil {
		return "", fmt.Errorf("string RVA 0x%x is outside PE sections", rva)
	}
	data, err := section.Data()
	if err != nil {
		return "", err
	}
	off := rva - section.VirtualAddress
	max := uint32(len(data)) - off
	if max > limit {
		max = limit
	}
	for i := uint32(0); i < max; i++ {
		if data[off+i] == 0 {
			return string(data[off : off+i]), nil
		}
	}
	return "", fmt.Errorf("unterminated export name at RVA 0x%x", rva)
}

func sectionForRVA(f *pe.File, rva uint32, size uint32) *pe.Section {
	for _, section := range f.Sections {
		start := section.VirtualAddress
		end := start + max32(section.VirtualSize, section.Size)
		if rva >= start && uint64(rva)+uint64(size) <= uint64(end) {
			return section
		}
	}
	return nil
}

func max32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
