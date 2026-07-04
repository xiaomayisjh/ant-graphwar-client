//go:build !windows

package backend

func DetectGraphwar2LocalRooms() ([]Graphwar2LocalRoom, error) {
	return graphwar2CompatLocalRooms(), nil
}
