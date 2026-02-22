package archive

import (
	"encoding/json"
	"fmt"
	"os"
)

// Read loads an Archive from a JSON file at the given path.
func Read(path string) (*Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading archive file: %w", err)
	}

	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("unmarshalling archive: %w", err)
	}

	return &a, nil
}

// ReadBatch loads a BatchArchive from a JSON file at the given path.
func ReadBatch(path string) (*BatchArchive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading batch archive file: %w", err)
	}

	var b BatchArchive
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("unmarshalling batch archive: %w", err)
	}

	return &b, nil
}
