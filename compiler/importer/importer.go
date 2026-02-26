// Package importer implements import resolution and source tree management.
// This mirrors C++ google::protobuf::compiler::Importer from compiler/importer.cc.
package importer

import (
	"fmt"
	"os"
	"path/filepath"
)

// SourceTree represents a set of directories to search for .proto files.
type SourceTree struct {
	Roots []string
}

// Open finds and reads a .proto file from the source tree.
func (st *SourceTree) Open(filename string) (string, error) {
	for _, root := range st.Roots {
		path := filepath.Join(root, filename)
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("file not found: %s", filename)
}

// Exists checks if a .proto file exists in the source tree.
func (st *SourceTree) Exists(filename string) bool {
	for _, root := range st.Roots {
		path := filepath.Join(root, filename)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// VirtualFileToDiskFile maps a virtual filename to the disk path.
func (st *SourceTree) VirtualFileToDiskFile(filename string) (string, bool) {
	for _, root := range st.Roots {
		path := filepath.Join(root, filename)
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

// ValidateRoots checks that all root directories exist and returns warnings.
func (st *SourceTree) ValidateRoots() []string {
	var warnings []string
	for _, root := range st.Roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("%s: warning: directory does not exist.", root))
		}
	}
	return warnings
}

// MakeRelative attempts to make a filename relative to one of the source tree roots.
func (st *SourceTree) MakeRelative(filename string) (string, error) {
	// If already relative and exists, return as-is
	if !filepath.IsAbs(filename) {
		if st.Exists(filename) {
			return filename, nil
		}
	}

	// Try to make relative to each root
	abs, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("Could not make proto path relative: %s: %s", filename, err)
	}

	for _, root := range st.Roots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err == nil && !filepath.IsAbs(rel) && rel != ".." && !startsWith(rel, ".."+string(filepath.Separator)) {
			return rel, nil
		}
	}

	// Check if file exists at all
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return "", fmt.Errorf("Could not make proto path relative: %s: No such file or directory", filename)
	}

	return "", fmt.Errorf("Could not make proto path relative: %s: not within any proto path", filename)
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
