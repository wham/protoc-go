// Package importer implements import resolution and source tree management.
// This mirrors C++ google::protobuf::compiler::Importer from compiler/importer.cc.
package importer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
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

	// Overlay holds in-memory file contents keyed by virtual path. They take
	// precedence over the disk and are never written to it.
	Overlay map[string]string

	// found remembers where a virtual file was located, so a file checked
	// with Exists is not searched for again when it is opened.
	foundMu sync.Mutex
	found   map[string]string
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
	st.foundMu.Lock()
	diskFile, ok := st.found[filename]
	st.foundMu.Unlock()
	if ok {
		return diskFile, true
	}
	found := false
	st.candidates(filename, func(candidate string) bool {
		if _, err := os.Stat(candidate); err != nil {
			return true
		}
		diskFile, found = candidate, true
		return false
	})
	if found {
		st.remember(filename, diskFile)
	}
	return diskFile, found
}

func (st *SourceTree) remember(filename, diskFile string) {
	st.foundMu.Lock()
	if st.found == nil {
		st.found = map[string]string{}
	}
	st.found[filename] = diskFile
	st.foundMu.Unlock()
}

// candidates yields, in mapping order, each disk path filename could live at,
// until yield returns false. A candidate must map back to the same virtual
// name, matching C++ DiskSourceTree::Open: filepath.Join normalizes paths
// (strips a leading "/" from the remainder), so "/dep.proto" reaches the same
// disk file as "dep.proto" but maps back to "dep.proto", not "/dep.proto".
func (st *SourceTree) candidates(filename string, yield func(diskFile string) bool) {
	try := func(virtualPath, diskPath string) bool {
		prefix := virtualPath
		if prefix != "" {
			prefix += "/"
		}
		if !startsWith(filename, prefix) {
			return true
		}
		diskFile := filepath.Join(diskPath, filename[len(prefix):])
		rel, err := filepath.Rel(diskPath, diskFile)
		if err != nil {
			return true
		}
		if virtualPath != "" {
			rel = virtualPath + "/" + rel
		}
		if rel != filename {
			return true
		}
		return yield(diskFile)
	}
	for _, root := range st.Roots {
		if !try("", root) {
			return
		}
	}
	for _, m := range st.Mappings {
		if !try(m.VirtualPath, m.DiskPath) {
			return
		}
	}
}

// readDisk finds and reads filename in one pass: each candidate is opened
// outright rather than checked first, so a file costs no stat of its own.
func (st *SourceTree) readDisk(filename string) (data []byte, ok bool) {
	st.foundMu.Lock()
	known, isKnown := st.found[filename]
	st.foundMu.Unlock()
	if isKnown {
		data, err := os.ReadFile(known)
		return data, err == nil
	}
	st.candidates(filename, func(candidate string) bool {
		d, err := os.ReadFile(candidate)
		if err == nil {
			data, ok = d, true
			st.remember(filename, candidate)
			return false
		}
		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
		// Something is there but cannot be read (a directory, no permission):
		// it still shadows later mappings, as long as it can be stat'ed.
		_, statErr := os.Stat(candidate)
		return statErr != nil
	})
	return data, ok
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
	if content, ok := st.Overlay[filename]; ok {
		return content, nil
	}
	if data, ok := st.readDisk(filename); ok {
		return ownedString(data), nil
	}
	// Fallback to embedded filesystem (e.g. bundled well-known types).
	if st.FallbackFS != nil {
		data, err := fs.ReadFile(st.FallbackFS, filename)
		if err == nil {
			return ownedString(data), nil
		}
	}
	return "", fmt.Errorf("file not found: %s", filename)
}

// ownedString views a freshly read buffer as a string without copying it. The
// buffer is this function's caller's alone, and nothing writes to it again.
func ownedString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
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
	if _, ok := st.Overlay[filename]; ok {
		return true
	}
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
