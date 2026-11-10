package loader

import (
	"fmt"
	"io"
	"os"
)

// MaxMeshSetSize bounds legacy mesh-set files. Streaming production artifacts
// should load individually validated tiles rather than increasing this limit.
const MaxMeshSetSize = 256 << 20

func readBoundedFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxMeshSetSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMeshSetSize {
		return nil, fmt.Errorf("loader: file exceeds %d bytes", MaxMeshSetSize)
	}
	return data, nil
}
