// Package importer implements import resolution and source tree management.
// This mirrors C++ google::protobuf::compiler::Importer from compiler/importer.cc.
package importer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

	// FallbackFS is an optional embedded filesystem used as a fallback
	// when a file is not found on disk. This is used to bundle well-known
	// types (e.g. google/protobuf/timestamp.proto) into the binary,
	// mirroring how C++ protoc uses compiled-in descriptors.
	FallbackFS fs.FS
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
// Includes a round-trip check matching C++ DiskSourceTree::Open: after finding
// a disk file, verifies that the disk path maps back to the same virtual filename.
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
			// Round-trip check: verify disk file maps back to same virtual filename.
			// filepath.Join normalizes paths (strips leading "/" from remainder),
			// so "/dep.proto" resolves to the same disk file as "dep.proto" but
			// the reverse mapping gives "dep.proto", not "/dep.proto".
			rel, err := filepath.Rel(m.DiskPath, diskFile)
			if err != nil {
				continue
			}
			var roundTrip string
			if m.VirtualPath != "" {
				roundTrip = m.VirtualPath + "/" + rel
			} else {
				roundTrip = rel
			}
			if roundTrip != filename {
				continue
			}
			return diskFile, true
		}
	}
	return "", false
}

// IsVirtualPathInvalid checks if a virtual path contains disallowed components.
// Returns true if the path contains backslashes, consecutive slashes, ".", or "..".
func IsVirtualPathInvalid(path string) bool {
	if strings.ContainsRune(path, '\\') {
		return true
	}
	if strings.Contains(path, "//") {
		return true
	}
	for _, component := range strings.Split(path, "/") {
		if component == "." || component == ".." {
			return true
		}
	}
	return false
}

// Open finds and reads a .proto file from the source tree.
// If the file is not found on disk but a FallbackFS is configured,
// it tries reading from the embedded filesystem (used for well-known types).
func (st *SourceTree) Open(filename string) (string, error) {
	if IsVirtualPathInvalid(filename) {
		return "", &VirtualPathError{Filename: filename}
	}
	if diskPath, ok := st.findFile(filename); ok {
		data, err := os.ReadFile(diskPath)
		if err == nil {
			return string(data), nil
		}
	}
	// Fallback to embedded filesystem (e.g. bundled well-known types).
	if st.FallbackFS != nil {
		data, err := fs.ReadFile(st.FallbackFS, filename)
		if err == nil {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("file not found: %s", filename)
}

// VirtualPathError is returned when a filename contains disallowed path components.
type VirtualPathError struct {
	Filename string
}

func (e *VirtualPathError) Error() string {
	return fmt.Sprintf(`Backslashes, consecutive slashes, ".", or ".." are not allowed in the virtual path`)
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
