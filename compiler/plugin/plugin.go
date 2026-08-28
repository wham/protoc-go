// Package plugin implements plugin subprocess management for protoc.
// This mirrors C++ google::protobuf::compiler::Subprocess from compiler/subprocess.cc
// and the plugin protocol from compiler/plugin.cc.
package plugin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

// The C++ protoc release protoc-go reproduces. It is reported by --version and
// sent to plugins as CodeGeneratorRequest.compiler_version, so it is written
// out once here rather than kept in step by hand in two places. Bumping the
// target is this edit alone: CI reads --version back
// (.github/install-target-protoc.sh) to pick the C++ release to verify against.
const (
	upstreamMajor = 7
	upstreamMinor = 36
	upstreamPatch = 0
)

// UpstreamVersion is that release as protoc --version reports it, e.g. "36.0".
// The libprotoc major is not part of it; protoc prints only minor and patch.
var UpstreamVersion = fmt.Sprintf("%d.%d", upstreamMinor, upstreamPatch)

// PluginStartError indicates that a plugin could not be started.
type PluginStartError struct {
	Path string
}

func (e *PluginStartError) Error() string {
	return fmt.Sprintf("plugin %s failed to start", e.Path)
}

// PluginExitError indicates that a plugin exited with non-zero status.
type PluginExitError struct {
	Path     string
	ExitCode int
}

func (e *PluginExitError) Error() string {
	return fmt.Sprintf("plugin %s failed with exit code %d", e.Path, e.ExitCode)
}

// RunPlugin executes a protoc plugin with the given CodeGeneratorRequest.
func RunPlugin(pluginPath string, req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	return RunPluginCommand([]string{pluginPath}, req)
}

// RunPluginCommand is [RunPlugin] over a full command line, whose last token is
// the plugin. A --<lang>_prefix wrapper supplies the tokens before it, so the
// plugin runs as "COMMAND <plugin>" rather than being executed directly.
func RunPluginCommand(argv []string, req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("no plugin command given")
	}
	pluginPath := argv[len(argv)-1]

	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = nil
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		// Match C++ protoc error format from subprocess.cc:
		// The child process writes these two lines to stderr and exits with code 1.
		fmt.Fprintf(os.Stderr, "%s: program not found or is not executable\n", argv[0])
		fmt.Fprintf(os.Stderr, "Please specify a program using absolute path or make sure the program is available in your PATH system variable\n")
		return nil, &PluginStartError{Path: argv[0]}
	}

	if _, err := stdinPipe.Write(reqBytes); err != nil {
		return nil, fmt.Errorf("writing to plugin stdin: %w", err)
	}
	stdinPipe.Close()

	respBytes, err := io.ReadAll(stdoutPipe)
	if err != nil {
		return nil, fmt.Errorf("reading plugin stdout: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, &PluginExitError{Path: pluginPath, ExitCode: exitErr.ExitCode()}
		}
		return nil, fmt.Errorf("plugin %s failed: %w", pluginPath, err)
	}

	var resp pluginpb.CodeGeneratorResponse
	if err := proto.Unmarshal(respBytes, &resp); err != nil {
		return nil, &ResponseError{Message: "Plugin output is unparseable: " + cEscape(respBytes)}
	}

	return &resp, nil
}

// BuildCodeGeneratorRequest builds a CodeGeneratorRequest from parsed file descriptors.
func BuildCodeGeneratorRequest(
	filesToGenerate []string,
	parameter string,
	protoFiles []*descriptorpb.FileDescriptorProto,
	sourceFileDescriptors []*descriptorpb.FileDescriptorProto,
) *pluginpb.CodeGeneratorRequest {
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate:        filesToGenerate,
		ProtoFile:             protoFiles,
		SourceFileDescriptors: sourceFileDescriptors,
		CompilerVersion: &pluginpb.Version{
			Major:  proto.Int32(upstreamMajor),
			Minor:  proto.Int32(upstreamMinor),
			Patch:  proto.Int32(upstreamPatch),
			Suffix: proto.String(""),
		},
	}
	if parameter != "" {
		req.Parameter = proto.String(parameter)
	}
	return req
}

// ResponseError is a failure protoc blames on the plugin. The caller prefixes
// it with "--<lang>_out: protoc-gen-<lang>: ".
type ResponseError struct{ Message string }

func (e *ResponseError) Error() string { return e.Message }

// ErrOutputFailed reports that everything protoc has to say about a failed
// write has already gone to stderr, and the process only has to exit non-zero.
// Its message is empty for exactly that reason.
var ErrOutputFailed = errors.New("")

// outputDir accumulates a plugin response the way C++ protoc's in-memory
// generator context does: nothing reaches disk until the whole response has
// been processed, so a response that fails half way through writes nothing new.
type outputDir struct {
	files    map[string]string
	order    []string
	hadError bool
}

// chunk is one response entry being accumulated. C++ protoc applies an entry
// only when the next one starts, because an entry with no name appends to the
// one before it; the deferral is what makes that work.
type chunk struct {
	name           string
	insertionPoint string
	data           string
}

func (o *outputDir) fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	o.hadError = true
}

// flush applies one accumulated entry: a plain write, or an insertion into a
// file an earlier entry produced.
func (o *outputDir) flush(c *chunk) {
	if c == nil {
		return
	}
	_, present := o.files[c.name]
	if !present {
		o.files[c.name] = ""
		o.order = append(o.order, c.name)
	}

	if c.insertionPoint == "" {
		if present {
			o.fail("%s: Tried to write the same file twice.", c.name)
			return
		}
		o.files[c.name] = c.data
		return
	}

	if !present {
		// The empty entry above stays behind, exactly as in C++ protoc: a later
		// plain write of the same name is then reported as a second write.
		o.fail("%s: Tried to insert into file that doesn't exist.", c.name)
		return
	}

	target := o.files[c.name]
	magic := "@@protoc_insertion_point(" + c.insertionPoint + ")"
	pos := strings.Index(target, magic)
	if pos < 0 {
		o.fail("%s: insertion point %q not found.", c.name, c.insertionPoint)
		return
	}
	o.files[c.name] = insertAt(target, pos, c.data)
}

// insertAt splices data into target immediately before the line holding the
// insertion point, indenting every inserted line to match it. Pushing the
// marker down is deliberate: it keeps repeated insertions at one point in the
// order the plugin emitted them.
func insertAt(target string, pos int, data string) string {
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	lineStart := strings.LastIndexByte(target[:pos], '\n') + 1
	indentEnd := lineStart
	for indentEnd < len(target) && (target[indentEnd] == ' ' || target[indentEnd] == '\t') {
		indentEnd++
	}
	indent := target[lineStart:indentEnd]

	var b strings.Builder
	b.Grow(len(target) + len(data))
	b.WriteString(target[:lineStart])
	if indent == "" {
		b.WriteString(data)
	} else {
		for len(data) > 0 {
			end := strings.IndexByte(data, '\n') + 1
			b.WriteString(indent)
			b.WriteString(data[:end])
			data = data[end:]
		}
	}
	b.WriteString(target[lineStart:])
	return b.String()
}

// collectPluginOutput replays a response into the file set it describes.
func collectPluginOutput(resp *pluginpb.CodeGeneratorResponse) (*outputDir, error) {
	out := &outputDir{files: map[string]string{}}
	var current *chunk
	for _, f := range resp.GetFile() {
		switch {
		case f.GetInsertionPoint() != "":
			out.flush(current)
			current = &chunk{name: f.GetName(), insertionPoint: f.GetInsertionPoint()}
		case f.GetName() != "":
			out.flush(current)
			current = &chunk{name: f.GetName()}
		case current == nil:
			return nil, &ResponseError{Message: "First file chunk returned by plugin did not specify a file name."}
		}
		current.data += f.GetContent()
	}
	out.flush(current)
	return out, nil
}

// WritePluginOutput writes a plugin response to outputLocation — a .zip or .jar
// archive when the name ends in one, otherwise a directory.
func WritePluginOutput(resp *pluginpb.CodeGeneratorResponse, outputLocation string, allowOutDirEscape bool) error {
	out, err := collectPluginOutput(resp)
	if err != nil {
		return err
	}
	if out.hadError {
		return ErrOutputFailed
	}

	sorted := append([]string{}, out.order...)
	sort.Strings(sorted)

	if strings.HasSuffix(outputLocation, ".zip") || strings.HasSuffix(outputLocation, ".jar") {
		return writeArchive(out.files, sorted, outputLocation, allowOutDirEscape)
	}

	for _, name := range sorted {
		// A ".." anywhere in the name is refused, not just a leading one: protoc
		// looks for the substring rather than resolving the path.
		if strings.Contains(name, "..") && !allowOutDirEscape {
			fmt.Fprintf(os.Stderr, "Output file names must never have a relative path. (%s). Use --unsafe_allow_out_dir_escape to disable this error if intentional.\n", name)
			return ErrOutputFailed
		}
		if err := createParentDirs(outputLocation, name); err != nil {
			return err
		}
		// Joined without cleaning, so a name allowed through by
		// --unsafe_allow_out_dir_escape lands where protoc would put it.
		outPath := outputLocation + string(filepath.Separator) + name
		if err := os.WriteFile(outPath, []byte(out.files[name]), 0o644); err != nil {
			return fmt.Errorf("writing file %s: %w", outPath, err)
		}
	}
	return nil
}

// createParentDirs makes every directory component of name under prefix. The
// components are taken from the name as written, empty ones skipped — protoc
// does not resolve the path first, so an escaping name leaves the directories
// it names behind.
func createParentDirs(prefix, name string) error {
	parts := strings.Split(name, "/")
	path := prefix
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			continue
		}
		path += string(filepath.Separator) + part
		if err := os.Mkdir(path, 0o777); err != nil && !os.IsExist(err) {
			return fmt.Errorf("creating directory %s: %w", path, err)
		}
	}
	return nil
}

// jarManifest is the manifest C++ protoc puts at the head of a .jar.
const jarManifest = "Manifest-Version: 1.0\nCreated-By: 1.6.0 (protoc)\n\n"

// writeArchive writes the file set as a zip. Every entry is stored uncompressed
// with a zeroed timestamp, which is what C++ protoc emits — archive/zip cannot
// produce those bytes, so the container is written by hand.
func writeArchive(files map[string]string, sorted []string, path string, allowOutDirEscape bool) error {
	if strings.HasSuffix(path, ".jar") {
		const manifest = "META-INF/MANIFEST.MF"
		if _, ok := files[manifest]; !ok {
			files = maps.Clone(files)
			files[manifest] = jarManifest
			sorted = append(append([]string{}, sorted...), manifest)
			sort.Strings(sorted)
		}
	}

	var body, central bytes.Buffer
	for _, name := range sorted {
		if strings.Contains(name, "..") && !allowOutDirEscape {
			fmt.Fprintf(os.Stderr, "WARNING: Output file names must never have a relative path. (%s). This will become an error in a future breaking change release of Protobuf. Use --unsafe_allow_out_dir_escape to suppress this warning if intentional.\n", name)
		}
		content := files[name]
		crc := crc32.ChecksumIEEE([]byte(content))
		offset := uint32(body.Len())

		body.WriteString("PK\x03\x04")
		writeZipFields(&body, crc, uint32(len(content)), uint16(len(name)))
		body.WriteString(name)
		body.WriteString(content)

		central.WriteString("PK\x01\x02")
		binary.Write(&central, binary.LittleEndian, uint16(zipVersion)) // version made by
		writeZipFields(&central, crc, uint32(len(content)), uint16(len(name)))
		binary.Write(&central, binary.LittleEndian, [3]uint16{0, 0, 0}) // comment length, start disk, internal attrs
		binary.Write(&central, binary.LittleEndian, uint32(0))          // external attrs
		binary.Write(&central, binary.LittleEndian, offset)
		central.WriteString(name)
	}

	var buf bytes.Buffer
	buf.Write(body.Bytes())
	buf.Write(central.Bytes())
	buf.WriteString("PK\x05\x06")
	binary.Write(&buf, binary.LittleEndian, [4]uint16{0, 0, uint16(len(sorted)), uint16(len(sorted))})
	binary.Write(&buf, binary.LittleEndian, uint32(central.Len()))
	binary.Write(&buf, binary.LittleEndian, uint32(body.Len()))
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // comment length

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

const (
	zipVersion = 10 // 1.0: stored entries only
	zipDOSDate = 33 // 1980-01-01, the epoch protoc stamps on every entry
)

// writeZipFields writes the run of header fields the local and central headers
// share, from the version-needed word through the extra-field length.
func writeZipFields(w *bytes.Buffer, crc, size uint32, nameLen uint16) {
	binary.Write(w, binary.LittleEndian, [5]uint16{zipVersion, 0, 0, 0, zipDOSDate})
	binary.Write(w, binary.LittleEndian, [3]uint32{crc, size, size})
	binary.Write(w, binary.LittleEndian, [2]uint16{nameLen, 0})
}

// cEscape renders bytes the way absl::CEscape does, which is how C++ protoc
// quotes a plugin's unparseable output back at the user.
func cEscape(b []byte) string {
	var out strings.Builder
	for _, c := range b {
		switch c {
		case '\n':
			out.WriteString("\\n")
		case '\r':
			out.WriteString("\\r")
		case '\t':
			out.WriteString("\\t")
		case '"':
			out.WriteString("\\\"")
		case '\'':
			out.WriteString("\\'")
		case '\\':
			out.WriteString("\\\\")
		default:
			if c < 0x20 || c >= 0x7f {
				fmt.Fprintf(&out, "\\%03o", c)
			} else {
				out.WriteByte(c)
			}
		}
	}
	return out.String()
}
