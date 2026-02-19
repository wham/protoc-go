package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wham/protoc-go/compiler/importer"
	"github.com/wham/protoc-go/compiler/parser"
	"github.com/wham/protoc-go/compiler/plugin"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

const usageText = `Usage: protoc [OPTION] PROTO_FILES
Parse PROTO_FILES and generate output based on the options given:
  -IPATH, --proto_path=PATH   Specify the directory in which to search for
                              imports.  May be specified multiple times;
                              directories will be searched in order.  If not
                              given, the current working directory is used.
                              If not found in any of the these directories,
                              the --descriptor_set_in descriptors will be
                              checked for required proto file.
  --version                   Show version info and exit.
  -h, --help                  Show this text and exit.
  --encode=MESSAGE_TYPE       Read a text-format message of the given type
                              from standard input and write it in binary
                              to standard output.  The message type must
                              be defined in PROTO_FILES or their imports.
  --deterministic_output      When using --encode, ensure map fields are
                              deterministically ordered. Note that this order
                              is not canonical, and changes across builds or
                              releases of protoc.
  --decode=MESSAGE_TYPE       Read a binary message of the given type from
                              standard input and write it in text format
                              to standard output.  The message type must
                              be defined in PROTO_FILES or their imports.
  --decode_raw                Read an arbitrary protocol message from
                              standard input and write the raw tag/value
                              pairs in text format to standard output.  No
                              PROTO_FILES should be given when using this
                              flag.
  --descriptor_set_in=FILES   Specifies a delimited list of FILES
                              each containing a FileDescriptorSet (a
                              protocol buffer defined in descriptor.proto).
                              The FileDescriptor for each of the PROTO_FILES
                              provided will be loaded from these
                              FileDescriptorSets. If a FileDescriptor
                              appears multiple times, the first occurrence
                              will be used.
  -oFILE,                     Writes a FileDescriptorSet (a protocol buffer,
    --descriptor_set_out=FILE defined in descriptor.proto) containing all of
                              the input files to FILE.
  --include_imports           When using --descriptor_set_out, also include
                              all dependencies of the input files in the
                              set, so that the set is self-contained.
  --include_source_info       When using --descriptor_set_out, do not strip
                              SourceCodeInfo from the FileDescriptorProto.
                              This results in vastly larger descriptors that
                              include information about the original
                              location of each decl in the source file as
                              well as surrounding comments.
  --retain_options            When using --descriptor_set_out, do not strip
                              any options from the FileDescriptorProto.
                              This results in potentially larger descriptors
                              that include information about options that were
                              only meant to be useful during compilation.
  --dependency_out=FILE       Write a dependency output file in the format
                              expected by make. This writes the transitive
                              set of input file paths to FILE
  --error_format=FORMAT       Set the format in which to print errors.
                              FORMAT may be 'gcc' (the default) or 'msvs'
                              (Microsoft Visual Studio format).
  --fatal_warnings            Make warnings be fatal (similar to -Werr in
                              gcc). This flag will make protoc return
                              with a non-zero exit code if any warnings
                              are generated.
  --print_free_field_numbers  Print the free field numbers of the messages
                              defined in the given proto files. Extension ranges
                              are counted as occupied fields numbers.
  --enable_codegen_trace      Enables tracing which parts of protoc are
                              responsible for what codegen output. Not supported
                              by all backends or on all platforms.
  --plugin=EXECUTABLE         Specifies a plugin executable to use.
                              Normally, protoc searches the PATH for
                              plugins, but you may specify additional
                              executables not in the path using this flag.
                              Additionally, EXECUTABLE may be of the form
                              NAME=PATH, in which case the given plugin name
                              is mapped to the given executable even if
                              the executable's own name differs.
  --cpp_out=OUT_DIR           Generate C++ header and source.
  --csharp_out=OUT_DIR        Generate C# source file.
  --java_out=OUT_DIR          Generate Java source file.
  --kotlin_out=OUT_DIR        Generate Kotlin file.
  --objc_out=OUT_DIR          Generate Objective-C header and source.
  --php_out=OUT_DIR           Generate PHP source file.
  --pyi_out=OUT_DIR           Generate python pyi stub.
  --python_out=OUT_DIR        Generate Python source file.
  --ruby_out=OUT_DIR          Generate Ruby source file.
  --rust_out=OUT_DIR          Generate Rust sources.
  @<filename>                 Read options and filenames from file. If a
                              relative file path is specified, the file
                              will be searched in the working directory.
                              The --proto_path option will not affect how
                              this argument file is searched. Content of
                              the file will be expanded in the position of
                              @<filename> as in the argument list. Note
                              that shell expansion is not applied to the
                              content of the file (i.e., you cannot use
                              quotes, wildcards, escapes, commands, etc.).
                              Each line corresponds to a single argument,
                              even if it contains spaces.`

type pluginSpec struct {
	name      string
	path      string
	outputDir string
	parameter string
}

type config struct {
	protoPaths        []string
	plugins           map[string]*pluginSpec
	descriptorSetOut  string
	includeImports    bool
	includeSourceInfo bool
	protoFiles        []string
}

// Run executes the protocol buffer compiler with the given command-line arguments.
// This mirrors C++ CommandLineInterface::Run().
func Run(args []string) error {
	cfg, err := parseArgs(args[1:]) // skip program name
	if err != nil {
		return err
	}

	if cfg == nil {
		// No args — print usage and exit successfully
		fmt.Println(usageText)
		return nil
	}

	// Validate we have output directives
	if len(cfg.plugins) == 0 && cfg.descriptorSetOut == "" {
		return fmt.Errorf("Missing output directives.")
	}

	// Default proto path
	if len(cfg.protoPaths) == 0 {
		cfg.protoPaths = []string{"."}
	}

	// Build source tree
	srcTree := &importer.SourceTree{Roots: cfg.protoPaths}

	// Validate proto paths
	warnings := srcTree.ValidateRoots()
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w)
	}

	// Make proto files relative to source tree
	relFiles := make([]string, len(cfg.protoFiles))
	for i, f := range cfg.protoFiles {
		rel, err := srcTree.MakeRelative(f)
		if err != nil {
			return err
		}
		relFiles[i] = rel
	}

	// Parse all proto files
	parsed := make(map[string]*descriptorpb.FileDescriptorProto)
	var orderedFiles []string

	for _, f := range relFiles {
		if err := parseRecursive(f, srcTree, parsed, &orderedFiles); err != nil {
			return err
		}
	}

	// Build ordered list of FileDescriptorProtos
	var protoFiles []*descriptorpb.FileDescriptorProto
	for _, name := range orderedFiles {
		protoFiles = append(protoFiles, parsed[name])
	}

	// Handle descriptor set output
	if cfg.descriptorSetOut != "" {
		fds := &descriptorpb.FileDescriptorSet{}
		if cfg.includeImports {
			for _, fd := range protoFiles {
				fdCopy := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
				if !cfg.includeSourceInfo {
					fdCopy.SourceCodeInfo = nil
				}
				fds.File = append(fds.File, fdCopy)
			}
		} else {
			for _, name := range relFiles {
				fdCopy := proto.Clone(parsed[name]).(*descriptorpb.FileDescriptorProto)
				if !cfg.includeSourceInfo {
					fdCopy.SourceCodeInfo = nil
				}
				fds.File = append(fds.File, fdCopy)
			}
		}

		data, err := proto.Marshal(fds)
		if err != nil {
			return fmt.Errorf("marshaling descriptor set: %w", err)
		}
		dir := filepath.Dir(cfg.descriptorSetOut)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating output directory: %w", err)
			}
		}
		if err := os.WriteFile(cfg.descriptorSetOut, data, 0o644); err != nil {
			return fmt.Errorf("writing descriptor set: %w", err)
		}
	}

	// Handle plugin outputs
	for _, plug := range cfg.plugins {
		// Build source file descriptors (same as files to generate, with source info)
		var sourceFileDescriptors []*descriptorpb.FileDescriptorProto
		for _, name := range relFiles {
			sourceFileDescriptors = append(sourceFileDescriptors, parsed[name])
		}

		req := plugin.BuildCodeGeneratorRequest(relFiles, plug.parameter, protoFiles, sourceFileDescriptors)

		resp, err := plugin.RunPlugin(plug.path, req)
		if err != nil {
			return err
		}

		if resp.GetError() != "" {
			return fmt.Errorf("plugin %s: %s", plug.name, resp.GetError())
		}

		if err := plugin.WritePluginOutput(resp, plug.outputDir); err != nil {
			return err
		}
	}

	return nil
}

func parseRecursive(filename string, srcTree *importer.SourceTree, parsed map[string]*descriptorpb.FileDescriptorProto, orderedFiles *[]string) error {
	if _, ok := parsed[filename]; ok {
		return nil
	}

	content, err := srcTree.Open(filename)
	if err != nil {
		return err
	}

	fd, err := parser.ParseFile(filename, content)
	if err != nil {
		return fmt.Errorf("%s: %w", filename, err)
	}

	// Resolve type references
	parser.ResolveTypes(fd)

	parsed[filename] = fd

	// Parse dependencies
	for _, dep := range fd.GetDependency() {
		if err := parseRecursive(dep, srcTree, parsed, orderedFiles); err != nil {
			return err
		}
	}

	*orderedFiles = append(*orderedFiles, filename)
	return nil
}

func parseArgs(args []string) (*config, error) {
	if len(args) == 0 {
		return nil, nil
	}

	cfg := &config{
		plugins: make(map[string]*pluginSpec),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--help" || arg == "-h" {
			fmt.Println(usageText)
			os.Exit(0)
		}

		if arg == "--version" {
			fmt.Println("libprotoc 29.3")
			os.Exit(0)
		}

		if strings.HasPrefix(arg, "--proto_path=") {
			cfg.protoPaths = append(cfg.protoPaths, arg[len("--proto_path="):])
			continue
		}

		if strings.HasPrefix(arg, "-I") {
			path := arg[2:]
			if path == "" && i+1 < len(args) {
				i++
				path = args[i]
			}
			cfg.protoPaths = append(cfg.protoPaths, path)
			continue
		}

		if strings.HasPrefix(arg, "--plugin=") {
			val := arg[len("--plugin="):]
			parts := strings.SplitN(val, "=", 2)
			if len(parts) == 2 {
				name := parts[0]
				// Extract plugin short name: protoc-gen-X → X
				shortName := name
				if strings.HasPrefix(name, "protoc-gen-") {
					shortName = name[len("protoc-gen-"):]
				}
				if _, ok := cfg.plugins[shortName]; !ok {
					cfg.plugins[shortName] = &pluginSpec{name: shortName}
				}
				cfg.plugins[shortName].path = parts[1]
			}
			continue
		}

		if strings.HasPrefix(arg, "--descriptor_set_out=") {
			cfg.descriptorSetOut = arg[len("--descriptor_set_out="):]
			continue
		}

		if arg == "--include_imports" {
			cfg.includeImports = true
			continue
		}

		if arg == "--include_source_info" {
			cfg.includeSourceInfo = true
			continue
		}

		// --X_out=DIR
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "_out=") {
			withoutDashes := arg[2:]
			eqIdx := strings.Index(withoutDashes, "_out=")
			pluginName := withoutDashes[:eqIdx]
			outputDir := withoutDashes[eqIdx+5:]
			if _, ok := cfg.plugins[pluginName]; !ok {
				cfg.plugins[pluginName] = &pluginSpec{name: pluginName}
			}
			cfg.plugins[pluginName].outputDir = outputDir
			// If no explicit plugin path, assume protoc-gen-X is on PATH
			if cfg.plugins[pluginName].path == "" {
				cfg.plugins[pluginName].path = "protoc-gen-" + pluginName
			}
			continue
		}

		// --X_opt=PARAM
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "_opt=") {
			withoutDashes := arg[2:]
			eqIdx := strings.Index(withoutDashes, "_opt=")
			pluginName := withoutDashes[:eqIdx]
			param := withoutDashes[eqIdx+5:]
			if _, ok := cfg.plugins[pluginName]; !ok {
				cfg.plugins[pluginName] = &pluginSpec{name: pluginName}
			}
			if cfg.plugins[pluginName].parameter != "" {
				cfg.plugins[pluginName].parameter += "," + param
			} else {
				cfg.plugins[pluginName].parameter = param
			}
			continue
		}

		if strings.HasPrefix(arg, "-") {
			// Unknown flag — ignore for now
			continue
		}

		// Proto file
		cfg.protoFiles = append(cfg.protoFiles, arg)
	}

	return cfg, nil
}

