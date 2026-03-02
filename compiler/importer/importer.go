// Package importer implements import resolution and source tree management.
// This mirrors C++ google::protobuf::compiler::Importer from compiler/importer.cc.
package importer

import (
	"fmt"
	"os"
	"path/filepath"
)

// Mapping represents a virtual-path to disk-path mapping.
// VirtualPath "" means root mapping (no prefix stripping).
type Mapping struct {
	VirtualPath string
	DiskPath    string
}

// SourceTree represents a set of directories to search for .proto files.
type SourceTree struct {
	Roots    []string // Simple root directories (VirtualPath="")
	Mappings []Mapping
}

// allMappings returns all mappings including those from Roots.
func (st *SourceTree) allMappings() []Mapping {
	var all []Mapping
	for _, root := range st.Roots {
		all = append(all, Mapping{VirtualPath: "", DiskPath: root})
	}
	all = append(all, st.Mappings...)
	return all
}

// findFile tries to find filename on disk using the configured mappings.
func (st *SourceTree) findFile(filename string) (string, bool) {
	for _, m := range st.allMappings() {
		prefix := m.VirtualPath
		if prefix != "" {
			prefix += "/"
		}
		if !startsWith(filename, prefix) {
			continue
		}
		remainder := filename[len(prefix):]
		diskFile := filepath.Join(m.DiskPath, remainder)
		if _, err := os.Stat(diskFile); err == nil {
			return diskFile, true
		}
	}
	return "", false
}

// Open finds and reads a .proto file from the source tree.
func (st *SourceTree) Open(filename string) (string, error) {
	if diskPath, ok := st.findFile(filename); ok {
		data, err := os.ReadFile(diskPath)
		if err == nil {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("file not found: %s", filename)
}

// Exists checks if a .proto file exists in the source tree.
func (st *SourceTree) Exists(filename string) bool {
	_, ok := st.findFile(filename)
	return ok
}

// VirtualFileToDiskFile maps a virtual filename to the disk path.
func (st *SourceTree) VirtualFileToDiskFile(filename string) (string, bool) {
	return st.findFile(filename)
}

// ValidateRoots checks that all root directories exist and returns warnings.
func (st *SourceTree) ValidateRoots() []string {
	var warnings []string
	for _, m := range st.allMappings() {
		if _, err := os.Stat(m.DiskPath); os.IsNotExist(err) {
			// C++ uses the original flag value for the warning
			label := m.DiskPath
			if m.VirtualPath != "" {
				label = m.VirtualPath + "=" + m.DiskPath
			}
			warnings = append(warnings, fmt.Sprintf("%s: warning: directory does not exist.", label))
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

	// Try to make relative to each mapping
	abs, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("Could not make proto path relative: %s: %s", filename, err)
	}

	for _, m := range st.allMappings() {
		rootAbs, err := filepath.Abs(m.DiskPath)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err == nil && !filepath.IsAbs(rel) && rel != ".." && !startsWith(rel, ".."+string(filepath.Separator)) {
			// Prepend virtual path prefix if any
			if m.VirtualPath != "" {
				rel = m.VirtualPath + "/" + rel
			}
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
