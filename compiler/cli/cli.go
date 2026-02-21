package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wham/protoc-go/compiler/importer"
	"github.com/wham/protoc-go/compiler/parser"
	"github.com/wham/protoc-go/compiler/plugin"
	"github.com/wham/protoc-go/io/tokenizer"
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
	protoPaths            []string
	plugins               map[string]*pluginSpec
	descriptorSetOut      string
	includeImports        bool
	includeSourceInfo     bool
	printFreeFieldNumbers bool
	decodeRaw             bool
	protoFiles            []string
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

	// --decode_raw reads binary proto from stdin; no files needed
	if cfg.decodeRaw {
		return nil
	}

	// Validate we have input files
	if len(cfg.protoFiles) == 0 {
		return fmt.Errorf("Missing input file.")
	}

	// Validate we have output directives
	if len(cfg.plugins) == 0 && cfg.descriptorSetOut == "" && !cfg.printFreeFieldNumbers {
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
	explicitJsonNames := make(map[*descriptorpb.FieldDescriptorProto]bool)
	var orderedFiles []string
	var collectErrors []string

	for _, f := range relFiles {
		ok, err := parseRecursive(f, srcTree, parsed, explicitJsonNames, &orderedFiles, nil, &collectErrors)
		if err != nil {
			return err
		}
		_ = ok
	}

	if len(collectErrors) > 0 {
		return fmt.Errorf("%s", strings.Join(collectErrors, "\n"))
	}

	// Resolve type references across all files (must happen after all files parsed)
	var resolveErrors []string
	for _, name := range orderedFiles {
		resolveErrors = append(resolveErrors, parser.ResolveTypes(parsed[name], parsed)...)
	}
	if len(resolveErrors) > 0 {
		return fmt.Errorf("%s", strings.Join(resolveErrors, "\n"))
	}

	// Phase 1: Build/cross-link validation — accumulate errors (C++ protoc collects all)
	var buildErrors []string
	fieldHints := make(map[*descriptorpb.DescriptorProto]*messageHint)
	buildErrors = append(buildErrors, validateMapKeyTypes(orderedFiles, parsed)...)
	dupNameErrs := validateDuplicateNames(orderedFiles, parsed)
	buildErrors = append(buildErrors, dupNameErrs...)
	if len(dupNameErrs) > 0 {
		buildErrors = append(buildErrors, validateMapEntryConflicts(orderedFiles, parsed)...)
	}
	buildErrors = append(buildErrors, validateExtRangePositive(orderedFiles, parsed, fieldHints)...)
	buildErrors = append(buildErrors, validatePositiveFieldNumbers(orderedFiles, parsed, fieldHints)...)
	buildErrors = append(buildErrors, validateMaxFieldNumbers(orderedFiles, parsed, fieldHints)...)
	buildErrors = append(buildErrors, validateReservedFieldNumbers(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateDuplicateFieldNumbers(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateReservedNumberConflicts(orderedFiles, parsed, fieldHints)...)
	buildErrors = append(buildErrors, validateDuplicateReservedNames(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateReservedNameConflicts(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateEmptyEnums(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateDuplicateEnumValues(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateExtensionFieldConflicts(orderedFiles, parsed, fieldHints)...)
	appendSuggestions(fieldHints, &buildErrors)
	buildErrors = append(buildErrors, validateReservedRangeOverlaps(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateEnumReservedRangeOverlaps(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateEnumReservedValueConflicts(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateEnumReservedNameConflicts(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateExtensionRangeOverlaps(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateExtensionReservedOverlaps(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateExtensionRanges(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateDuplicateExtensionNumbers(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateRequiredExtensions(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateExtensionJsonName(orderedFiles, parsed, explicitJsonNames)...)
	buildErrors = append(buildErrors, validateMessageSetFields(orderedFiles, parsed)...)
	if len(buildErrors) > 0 {
		return fmt.Errorf("%s", strings.Join(buildErrors, "\n"))
	}

	// Phase 2: Descriptor validation — only runs when no build errors (C++ had_errors_ gate)
	var valErrors []string
	if len(dupNameErrs) == 0 {
		valErrors = append(valErrors, validateJsonNameConflicts(orderedFiles, parsed, explicitJsonNames)...)
	}
	valErrors = append(valErrors, validatePackedNonRepeated(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateLazyNonMessage(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateJstypeNonInt64(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateRepeatedDefault(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateMessageDefault(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateEnumDefaultValues(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateProto3(orderedFiles, parsed)...)
	featEditionErrs := validateFeaturesEditions(orderedFiles, parsed)
	valErrors = append(valErrors, featEditionErrs...)
	if len(featEditionErrs) == 0 {
		valErrors = append(valErrors, validateFeatureTargets(orderedFiles, parsed)...)
	}
	if len(valErrors) > 0 {
		return fmt.Errorf("%s", strings.Join(valErrors, "\n"))
	}

	// Handle --print_free_field_numbers mode
	if cfg.printFreeFieldNumbers {
		for _, f := range relFiles {
			fd := parsed[f]
			pkg := fd.GetPackage()
			for _, msg := range fd.GetMessageType() {
				fqn := msg.GetName()
				if pkg != "" {
					fqn = pkg + "." + msg.GetName()
				}
				printFreeFieldNumbers(fqn, msg)
			}
		}
		return nil
	}

	// Build ordered list of FileDescriptorProtos (stripped of source-retention options)
	var protoFiles []*descriptorpb.FileDescriptorProto
	strippedMap := make(map[string]*descriptorpb.FileDescriptorProto)
	for _, name := range orderedFiles {
		stripped := stripSourceRetention(parsed[name])
		protoFiles = append(protoFiles, stripped)
		strippedMap[name] = stripped
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
			relFileSet := make(map[string]bool)
			for _, name := range relFiles {
				relFileSet[name] = true
			}
			for _, name := range orderedFiles {
				if relFileSet[name] {
					fdCopy := proto.Clone(strippedMap[name]).(*descriptorpb.FileDescriptorProto)
					if !cfg.includeSourceInfo {
						fdCopy.SourceCodeInfo = nil
					}
					fds.File = append(fds.File, fdCopy)
				}
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
		// Build source file descriptors in dependency order (matching protoFile order)
		relFileSet := make(map[string]bool)
		for _, name := range relFiles {
			relFileSet[name] = true
		}
		var sourceFileDescriptors []*descriptorpb.FileDescriptorProto
		for _, name := range orderedFiles {
			if relFileSet[name] {
				sourceFileDescriptors = append(sourceFileDescriptors, parsed[name])
			}
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

func parseRecursive(filename string, srcTree *importer.SourceTree, parsed map[string]*descriptorpb.FileDescriptorProto, explicitJsonNames map[*descriptorpb.FieldDescriptorProto]bool, orderedFiles *[]string, importStack []string, collectErrors *[]string) (bool, error) {
	// Check for import cycles
	for idx, f := range importStack {
		if f == filename {
			// Build chain starting from the cycle point
			chain := ""
			started := false
			for _, s := range importStack {
				if s == filename {
					started = true
				}
				if started {
					if chain != "" {
						chain += " -> "
					}
					chain += s
				}
			}
			chain += " -> " + filename

			// Report at cycle-starting file's import of the next file in chain
			cycleStart := filename
			cycleStartFd := parsed[cycleStart]
			var nextFile string
			if idx+1 < len(importStack) {
				nextFile = importStack[idx+1]
			} else {
				nextFile = filename // self-import
			}
			line, col := findImportLocation(cycleStartFd, nextFile)
			if collectErrors != nil {
				*collectErrors = append(*collectErrors, fmt.Sprintf("%s:%d:%d: File recursively imports itself: %s", cycleStart, line, col, chain))
				return false, nil
			}
			return false, fmt.Errorf("%s:%d:%d: File recursively imports itself: %s", cycleStart, line, col, chain)
		}
	}

	if _, ok := parsed[filename]; ok {
		return true, nil
	}

	content, err := srcTree.Open(filename)
	if err != nil {
		if collectErrors != nil {
			*collectErrors = append(*collectErrors, fmt.Sprintf("%s: File not found.", filename))
			return false, nil
		}
		return false, err
	}

	result, err := parser.ParseFile(filename, content)
	if err != nil {
		if me, ok := err.(*parser.MultiError); ok {
			return false, me
		}
		return false, fmt.Errorf("%s:%w", filename, err)
	}

	fd := result.FD
	for f := range result.ExplicitJsonNames {
		explicitJsonNames[f] = true
	}

	parsed[filename] = fd

	// Parse dependencies
	newStack := append(importStack, filename)
	failedDeps := map[string]bool{}
	for _, dep := range fd.GetDependency() {
		ok, err := parseRecursive(dep, srcTree, parsed, explicitJsonNames, orderedFiles, newStack, collectErrors)
		if err != nil {
			return false, err
		}
		if !ok {
			failedDeps[dep] = true
		}
	}

	if len(failedDeps) > 0 && collectErrors != nil {
		// Self-import: if the only failed dep is this file itself, just return
		if len(failedDeps) == 1 && failedDeps[filename] {
			return false, nil
		}

		// Add "Import X was not found or had errors." for each failed dep (that isn't self)
		for _, dep := range fd.GetDependency() {
			if failedDeps[dep] && dep != filename {
				line, col := findImportLocation(fd, dep)
				*collectErrors = append(*collectErrors, fmt.Sprintf("%s:%d:%d: Import \"%s\" was not found or had errors.", filename, line, col, dep))
			}
		}

		// Build available files (excluding failed deps)
		availableFiles := map[string]*descriptorpb.FileDescriptorProto{}
		for _, dep := range fd.GetDependency() {
			if !failedDeps[dep] {
				if depFd, ok := parsed[dep]; ok {
					availableFiles[dep] = depFd
				}
			}
		}

		// Check for unresolved type errors
		typeErrors := parser.CheckUnresolvedTypes(fd, availableFiles)
		*collectErrors = append(*collectErrors, typeErrors...)

		return false, nil
	} else if len(failedDeps) > 0 {
		// No error collector — return first failed dep as error
		return false, fmt.Errorf("import failed")
	}

	*orderedFiles = append(*orderedFiles, filename)
	return true, nil
}

// findImportLocation finds the line:col of an import statement in a file descriptor's SCI.
func findImportLocation(fd *descriptorpb.FileDescriptorProto, importedFile string) (int, int) {
	if fd == nil {
		return 0, 0
	}
	// Find the dependency index
	depIdx := int32(-1)
	for i, dep := range fd.GetDependency() {
		if dep == importedFile {
			depIdx = int32(i)
			break
		}
	}
	if depIdx < 0 {
		return 0, 0
	}
	// Look for SCI path [3, depIdx]
	for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
		path := loc.GetPath()
		if len(path) == 2 && path[0] == 3 && path[1] == depIdx {
			span := loc.GetSpan()
			if len(span) >= 2 {
				return int(span[0]) + 1, int(span[1]) + 1 // 1-indexed
			}
		}
	}
	return 0, 0
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

		if strings.HasPrefix(arg, "--error_format=") {
			continue
		}

		if arg == "--fatal_warnings" {
			continue
		}

		if arg == "--deterministic_output" {
			continue
		}

		if arg == "--retain_options" {
			continue
		}

		if strings.HasPrefix(arg, "--enable_codegen_trace") {
			continue
		}

		if strings.HasPrefix(arg, "--descriptor_set_in=") {
			continue
		}

		if strings.HasPrefix(arg, "--encode=") || strings.HasPrefix(arg, "--decode=") {
			continue
		}

		if arg == "--decode_raw" {
			cfg.decodeRaw = true
			continue
		}

		if arg == "--print_free_field_numbers" {
			cfg.printFreeFieldNumbers = true
			continue
		}

		if strings.HasPrefix(arg, "--dependency_out=") {
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
			if idx := strings.Index(arg, "="); idx >= 0 {
				flagName := arg[:idx]
				return nil, fmt.Errorf("Unknown flag: %s", flagName)
			}
			return nil, fmt.Errorf("Missing value for flag: %s", arg)
		}

		// Proto file
		cfg.protoFiles = append(cfg.protoFiles, arg)
	}

	return cfg, nil
}

// validateMapKeyTypes checks that map fields don't use float/double/bytes/enum/message as key types.
func validateMapKeyTypes(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectMapKeyTypeErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectMapKeyTypeErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			keyField := nested.GetField()[0]
			kt := keyField.GetType()
			if kt == descriptorpb.FieldDescriptorProto_TYPE_FLOAT ||
				kt == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE ||
				kt == descriptorpb.FieldDescriptorProto_TYPE_BYTES ||
				kt == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE ||
				kt == descriptorpb.FieldDescriptorProto_TYPE_GROUP ||
				kt == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
				// Find the parent field that uses this map entry
				var errMsg string
				if kt == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
					errMsg = "Key in map fields cannot be enum types."
				} else {
					errMsg = "Key in map fields cannot be float/double, bytes or message types."
				}
				for j := range msg.GetField() {
					field := msg.GetField()[j]
					if field.GetTypeName() == nested.GetName() || strings.HasSuffix(field.GetTypeName(), "."+nested.GetName()) {
						fieldPath := append(append([]int32{}, msgPath...), 2, int32(j))
						line, col := findLocationByPath(fieldPath, sci)
						*errs = append(*errs, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, errMsg))
						break
					}
				}
			}
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectMapKeyTypeErrors(filename, nested, nestedPath, sci, errs)
	}
}

// isPackableType returns true if the field type can be packed (numeric, bool, enum).
func isPackableType(t descriptorpb.FieldDescriptorProto_Type) bool {
	switch t {
	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT,
		descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_BOOL,
		descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		return true
	}
	return false
}

// validatePackedNonRepeated checks that [packed = true] is only on repeated primitive fields.
func validatePackedNonRepeated(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectPackedErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		// Check file-level extensions
		for i, ext := range fd.GetExtension() {
			if ext.GetOptions().GetPacked() {
				if ext.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_REPEATED || !isPackableType(ext.GetType()) {
					path := []int32{7, int32(i), 5}
					line, col := findLocationByPath(path, sci)
					errs = append(errs, fmt.Sprintf("%s:%d:%d: [packed = true] can only be specified for repeated primitive fields.", fd.GetName(), line, col))
				}
			}
		}
	}
	return errs
}

func collectPackedErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.GetOptions().GetPacked() {
			if field.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_REPEATED || !isPackableType(field.GetType()) {
				fieldPath := append(append([]int32{}, msgPath...), 2, int32(i), 5)
				line, col := findLocationByPath(fieldPath, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: [packed = true] can only be specified for repeated primitive fields.", filename, line, col))
			}
		}
	}
	// Check message-level extensions
	for i, ext := range msg.GetExtension() {
		if ext.GetOptions().GetPacked() {
			if ext.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_REPEATED || !isPackableType(ext.GetType()) {
				extPath := append(append([]int32{}, msgPath...), 6, int32(i), 5)
				line, col := findLocationByPath(extPath, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: [packed = true] can only be specified for repeated primitive fields.", filename, line, col))
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectPackedErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateLazyNonMessage checks that [lazy = true] is only on submessage fields.
func validateLazyNonMessage(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectLazyErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		// Check file-level extensions
		for i, ext := range fd.GetExtension() {
			if ext.GetOptions().GetLazy() || ext.GetOptions().GetUnverifiedLazy() {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					path := []int32{7, int32(i), 5}
					line, col := findLocationByPath(path, sci)
					errs = append(errs, fmt.Sprintf("%s:%d:%d: [lazy = true] can only be specified for submessage fields.", fd.GetName(), line, col))
				}
			}
		}
	}
	return errs
}

func collectLazyErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.GetOptions().GetLazy() || field.GetOptions().GetUnverifiedLazy() {
			if field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
				fieldPath := append(append([]int32{}, msgPath...), 2, int32(i), 5)
				line, col := findLocationByPath(fieldPath, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: [lazy = true] can only be specified for submessage fields.", filename, line, col))
			}
		}
	}
	// Check message-level extensions
	for i, ext := range msg.GetExtension() {
		if ext.GetOptions().GetLazy() || ext.GetOptions().GetUnverifiedLazy() {
			if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
				extPath := append(append([]int32{}, msgPath...), 6, int32(i), 5)
				line, col := findLocationByPath(extPath, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: [lazy = true] can only be specified for submessage fields.", filename, line, col))
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectLazyErrors(filename, nested, nestedPath, sci, errs)
	}
}

// isJstypeAllowedType returns true if the field type supports the jstype option.
func isJstypeAllowedType(t descriptorpb.FieldDescriptorProto_Type) bool {
	switch t {
	case descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return true
	}
	return false
}

// validateJstypeNonInt64 checks that jstype is only used on int64-family fields.
func validateJstypeNonInt64(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectJstypeErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		// Check file-level extensions
		for i, ext := range fd.GetExtension() {
			if ext.GetOptions().GetJstype() != descriptorpb.FieldOptions_JS_NORMAL && ext.Options != nil && ext.Options.Jstype != nil {
				if !isJstypeAllowedType(ext.GetType()) {
					path := []int32{7, int32(i), 5}
					line, col := findLocationByPath(path, sci)
					errs = append(errs, fmt.Sprintf("%s:%d:%d: jstype is only allowed on int64, uint64, sint64, fixed64 or sfixed64 fields.", fd.GetName(), line, col))
				}
			}
		}
	}
	return errs
}

func collectJstypeErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.Options != nil && field.Options.Jstype != nil {
			if !isJstypeAllowedType(field.GetType()) {
				fieldPath := append(append([]int32{}, msgPath...), 2, int32(i), 5)
				line, col := findLocationByPath(fieldPath, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: jstype is only allowed on int64, uint64, sint64, fixed64 or sfixed64 fields.", filename, line, col))
			}
		}
	}
	// Check message-level extensions
	for i, ext := range msg.GetExtension() {
		if ext.Options != nil && ext.Options.Jstype != nil {
			if !isJstypeAllowedType(ext.GetType()) {
				extPath := append(append([]int32{}, msgPath...), 6, int32(i), 5)
				line, col := findLocationByPath(extPath, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: jstype is only allowed on int64, uint64, sint64, fixed64 or sfixed64 fields.", filename, line, col))
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectJstypeErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateExtRangePositive checks that extension range start numbers are positive.
func validateExtRangePositive(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, hints map[*descriptorpb.DescriptorProto]*messageHint) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectExtRangePositiveErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, sci, &errs, hints)
		}
	}
	return errs
}

func collectExtRangePositiveErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string, hints map[*descriptorpb.DescriptorProto]*messageHint) {
	for j, er := range msg.GetExtensionRange() {
		if er.GetStart() <= 0 {
			path := append(append([]int32{}, msgPath...), 5, int32(j), 1)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extension numbers must be positive integers.", filename, line, col))
			requestHint(hints, msg, filename, fqn, line, col, int(er.GetStart()), int(er.GetEnd()))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := fqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectExtRangePositiveErrors(filename, nested, nestedFqn, nestedPath, sci, errs, hints)
	}
}

// validatePositiveFieldNumbers checks that all field numbers are positive integers.
func validatePositiveFieldNumbers(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, hints map[*descriptorpb.DescriptorProto]*messageHint) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectPositiveFieldNumberErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs, hints)
		}
	}
	return errs
}

func collectPositiveFieldNumberErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string, hints map[*descriptorpb.DescriptorProto]*messageHint) {
	for i, field := range msg.GetField() {
		if field.GetNumber() <= 0 {
			line, col := findFieldNumberLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field numbers must be positive integers.", filename, line, col))
			requestHint(hints, msg, filename, fqn, line, col, 0, 1)
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := fqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectPositiveFieldNumberErrors(filename, nested, nestedFqn, nestedPath, sci, errs, hints)
	}
}

// messageHint tracks per-message suggestion info, matching C++ MessageHints.
type messageHint struct {
	fieldsToSuggest int
	firstLine       int
	firstCol        int
	filename        string
	fqn             string
	msg             *descriptorpb.DescriptorProto
}

// requestHint updates the hint for a message, accumulating fields_to_suggest.
func requestHint(hints map[*descriptorpb.DescriptorProto]*messageHint, msg *descriptorpb.DescriptorProto, filename, fqn string, line, col int, rangeStart, rangeEnd int) {
	h, ok := hints[msg]
	if !ok {
		h = &messageHint{filename: filename, fqn: fqn, msg: msg, firstLine: line, firstCol: col}
		hints[msg] = h
	}
	add := rangeEnd - rangeStart
	if add < 0 {
		add = 0
	}
	if add > kMaxFieldNumber {
		add = kMaxFieldNumber
	}
	h.fieldsToSuggest += add
	if h.fieldsToSuggest > kMaxFieldNumber {
		h.fieldsToSuggest = kMaxFieldNumber
	}
}

// suggestFieldNumbers returns a comma-separated list of up to count available
// field numbers for the message, excluding used fields, extension ranges,
// reserved ranges, and the protobuf-reserved 19000-19999 range.
func suggestFieldNumbers(msg *descriptorpb.DescriptorProto, count int) string {
	const kFirstReserved = 19000
	const kLastReserved = 19999

	type rng struct{ from, to int32 }
	var used []rng

	addOrdinal := func(n int32) {
		if n <= 0 || n > kMaxFieldNumber {
			return
		}
		used = append(used, rng{n, n + 1})
	}
	addRange := func(from, to int32) {
		if from < 0 {
			from = 0
		}
		if from > kMaxFieldNumber+1 {
			from = kMaxFieldNumber + 1
		}
		if to < 0 {
			to = 0
		}
		if to > kMaxFieldNumber+1 {
			to = kMaxFieldNumber + 1
		}
		if from >= to {
			return
		}
		used = append(used, rng{from, to})
	}

	for _, field := range msg.GetField() {
		addOrdinal(field.GetNumber())
	}
	for _, ext := range msg.GetExtension() {
		addOrdinal(ext.GetNumber())
	}
	for _, rr := range msg.GetReservedRange() {
		addRange(rr.GetStart(), rr.GetEnd())
	}
	for _, er := range msg.GetExtensionRange() {
		addRange(er.GetStart(), er.GetEnd())
	}
	used = append(used, rng{kMaxFieldNumber, kMaxFieldNumber + 1})
	used = append(used, rng{kFirstReserved, kLastReserved + 1})

	sort.Slice(used, func(i, j int) bool {
		if used[i].from != used[j].from {
			return used[i].from < used[j].from
		}
		return used[i].to < used[j].to
	})

	var parts []string
	current := int32(1)
	remaining := count
	for _, r := range used {
		for current < r.from && remaining > 0 {
			parts = append(parts, fmt.Sprintf("%d", current))
			current++
			remaining--
		}
		if remaining == 0 {
			break
		}
		if current < r.to {
			current = r.to
		}
	}
	return strings.Join(parts, ", ")
}

// appendSuggestions appends "Suggested field numbers" lines from collected hints.
func appendSuggestions(hints map[*descriptorpb.DescriptorProto]*messageHint, errs *[]string) {
	const kMaxSuggestions = 3
	for _, h := range hints {
		count := h.fieldsToSuggest
		if count > kMaxSuggestions {
			count = kMaxSuggestions
		}
		if count <= 0 {
			continue
		}
		suggestion := suggestFieldNumbers(h.msg, count)
		if suggestion == "" {
			continue
		}
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Suggested field numbers for %s: %s",
			h.filename, h.firstLine, h.firstCol, h.fqn, suggestion))
	}
}

const kMaxFieldNumber = 536870911 // 2^29 - 1

// validateMaxFieldNumbers checks that no field number exceeds 536870911.
func validateMaxFieldNumbers(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, hints map[*descriptorpb.DescriptorProto]*messageHint) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectMaxFieldNumberErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs, hints)
		}
		for _, ext := range fd.GetExtension() {
			if ext.GetNumber() > kMaxFieldNumber {
				errs = append(errs, fmt.Sprintf("%s: Field numbers cannot be greater than %d.", fd.GetName(), kMaxFieldNumber))
			}
		}
	}
	return errs
}

func collectMaxFieldNumberErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string, hints map[*descriptorpb.DescriptorProto]*messageHint) {
	for i, field := range msg.GetField() {
		if field.GetNumber() > kMaxFieldNumber {
			line, col := findFieldNumberLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field numbers cannot be greater than %d.", filename, line, col, kMaxFieldNumber))
			requestHint(hints, msg, filename, fqn, line, col, 0, 1)
		}
	}
	for _, ext := range msg.GetExtension() {
		if ext.GetNumber() > kMaxFieldNumber {
			*errs = append(*errs, fmt.Sprintf("%s: Field numbers cannot be greater than %d.", filename, kMaxFieldNumber))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := fqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectMaxFieldNumberErrors(filename, nested, nestedFqn, nestedPath, sci, errs, hints)
	}
}

// validateReservedFieldNumbers checks that no field uses numbers 19000-19999.
func validateReservedFieldNumbers(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	const kFirstReserved = 19000
	const kLastReserved = 19999
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		collectReservedFieldNumberErrors(fd.GetName(), fd.GetMessageType(), &errs, kFirstReserved, kLastReserved)
		for _, ext := range fd.GetExtension() {
			if n := ext.GetNumber(); n >= kFirstReserved && n <= kLastReserved {
				errs = append(errs, fmt.Sprintf("%s: Field numbers %d through %d are reserved for the protocol buffer library implementation.", fd.GetName(), kFirstReserved, kLastReserved))
			}
		}
	}
	return errs
}

func collectReservedFieldNumberErrors(filename string, msgs []*descriptorpb.DescriptorProto, errs *[]string, first, last int32) {
	for _, msg := range msgs {
		for _, field := range msg.GetField() {
			if n := field.GetNumber(); n >= first && n <= last {
				*errs = append(*errs, fmt.Sprintf("%s: Field numbers %d through %d are reserved for the protocol buffer library implementation.", filename, first, last))
			}
		}
		for _, ext := range msg.GetExtension() {
			if n := ext.GetNumber(); n >= first && n <= last {
				*errs = append(*errs, fmt.Sprintf("%s: Field numbers %d through %d are reserved for the protocol buffer library implementation.", filename, first, last))
			}
		}
		collectReservedFieldNumberErrors(filename, msg.GetNestedType(), errs, first, last)
	}
}

// validateDuplicateFieldNumbers checks that no two fields in a message share the same field number.
func validateDuplicateFieldNumbers(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectDuplicateFieldNumberErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
	}
	return errs
}

func collectDuplicateFieldNumberErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	// Map field number -> first field name
	seen := make(map[int32]string)
	for i, field := range msg.GetField() {
		num := field.GetNumber()
		if firstName, ok := seen[num]; ok {
			line, col := findFieldNumberLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field number %d has already been used in \"%s\" by field \"%s\".",
				filename, line, col, num, fqn, firstName))
		} else {
			seen[num] = field.GetName()
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := fqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectDuplicateFieldNumberErrors(filename, nested, nestedFqn, nestedPath, sci, errs)
	}
}

func findFieldNumberLocation(msgPath []int32, fieldIdx int, sci *descriptorpb.SourceCodeInfo) (int, int) {
	if sci == nil {
		return 0, 0
	}
	// Path: msgPath + [2, fieldIdx, 3] where 2=field list, 3=number field of FieldDescriptorProto
	target := append(append([]int32{}, msgPath...), 2, int32(fieldIdx), 3)
	for _, loc := range sci.GetLocation() {
		path := loc.GetPath()
		if len(path) == len(target) {
			match := true
			for i := range path {
				if path[i] != target[i] {
					match = false
					break
				}
			}
			if match {
				span := loc.GetSpan()
				if len(span) >= 2 {
					return int(span[0]) + 1, int(span[1]) + 1
				}
			}
		}
	}
	return 0, 0
}

// validateReservedNumberConflicts checks that no field uses a number in the message's reserved ranges.
func validateReservedNumberConflicts(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, hints map[*descriptorpb.DescriptorProto]*messageHint) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectReservedNumberConflictErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, sci, &errs, hints)
		}
	}
	return errs
}

func collectReservedNumberConflictErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string, hints map[*descriptorpb.DescriptorProto]*messageHint) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for _, field := range msg.GetField() {
		num := field.GetNumber()
		for j, rr := range msg.GetReservedRange() {
			// ReservedRange uses exclusive end in DescriptorProto
			if num >= rr.GetStart() && num < rr.GetEnd() {
				// C++ protoc points at the reserved range start location
				line, col := findReservedRangeStartLocation(msgPath, j, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field \"%s\" uses reserved number %d.",
					filename, line, col, field.GetName(), num))
				requestHint(hints, msg, filename, fqn, line, col, 0, 1)
				break
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := fqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectReservedNumberConflictErrors(filename, nested, nestedFqn, nestedPath, sci, errs, hints)
	}
}

func findReservedRangeStartLocation(msgPath []int32, rangeIdx int, sci *descriptorpb.SourceCodeInfo) (int, int) {
	if sci == nil {
		return 0, 0
	}
	// Path: msgPath + [9, rangeIdx, 1] where 9=reserved_range, 1=start field
	target := append(append([]int32{}, msgPath...), 9, int32(rangeIdx), 1)
	for _, loc := range sci.GetLocation() {
		path := loc.GetPath()
		if len(path) == len(target) {
			match := true
			for i := range path {
				if path[i] != target[i] {
					match = false
					break
				}
			}
			if match {
				span := loc.GetSpan()
				if len(span) >= 2 {
					return int(span[0]) + 1, int(span[1]) + 1
				}
			}
		}
	}
	return 0, 0
}

// validateDuplicateReservedNames checks that no reserved name appears more than once in a message or enum.
func validateDuplicateReservedNames(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectDuplicateReservedNameErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		for i, enum := range fd.GetEnumType() {
			collectDuplicateEnumReservedNameErrors(fd.GetName(), enum, []int32{5, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectDuplicateReservedNameErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	seen := make(map[string]bool)
	for _, rn := range msg.GetReservedName() {
		if seen[rn] {
			path := append(append([]int32{}, msgPath...), 1)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field name \"%s\" is reserved multiple times.",
				filename, line, col, rn))
		}
		seen[rn] = true
	}
	for i, nested := range msg.GetNestedType() {
		np := append(append([]int32{}, msgPath...), 3, int32(i))
		collectDuplicateReservedNameErrors(filename, nested, np, sci, errs)
	}
	for i, enum := range msg.GetEnumType() {
		ep := append(append([]int32{}, msgPath...), 4, int32(i))
		collectDuplicateEnumReservedNameErrors(filename, enum, ep, sci, errs)
	}
}

func collectDuplicateEnumReservedNameErrors(filename string, enum *descriptorpb.EnumDescriptorProto, enumPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	seen := make(map[string]bool)
	for _, rn := range enum.GetReservedName() {
		if seen[rn] {
			path := append(append([]int32{}, enumPath...), 1)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Enum value \"%s\" is reserved multiple times.",
				filename, line, col, rn))
		}
		seen[rn] = true
	}
}

// validateReservedNameConflicts checks that no field uses a reserved name in its message.
func validateReservedNameConflicts(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectReservedNameErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectReservedNameErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	reserved := make(map[string]bool)
	for _, rn := range msg.GetReservedName() {
		reserved[rn] = true
	}
	for i, field := range msg.GetField() {
		if reserved[field.GetName()] {
			path := append(append([]int32{}, msgPath...), 2, int32(i), 1)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field name \"%s\" is reserved.",
				filename, line, col, field.GetName()))
		}
	}
	for i, nested := range msg.GetNestedType() {
		np := append(append([]int32{}, msgPath...), 3, int32(i))
		collectReservedNameErrors(filename, nested, np, sci, errs)
	}
}

// validateEmptyEnums checks that all enums contain at least one value.
func validateEmptyEnums(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		for i, e := range fd.GetEnumType() {
			if len(e.GetValue()) == 0 {
				line, col := findLocationByPath([]int32{5, int32(i), 1}, fd.GetSourceCodeInfo())
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Enums must contain at least one value.", fd.GetName(), line, col))
			}
		}
		for i, msg := range fd.GetMessageType() {
			collectEmptyEnumErrors(fd.GetName(), msg, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
	}
	return errs
}

func collectEmptyEnumErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, e := range msg.GetEnumType() {
		if len(e.GetValue()) == 0 {
			path := append(append([]int32{}, msgPath...), 4, int32(i), 1)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Enums must contain at least one value.", filename, line, col))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectEmptyEnumErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateDuplicateEnumValues checks that no two enum values share the same number
// unless allow_alias is set to true.
func validateDuplicateEnumValues(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for i, e := range fd.GetEnumType() {
			parentScope := pkg
			collectDuplicateEnumValueErrors(fd.GetName(), e, parentScope, []int32{5, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
		for i, msg := range fd.GetMessageType() {
			msgFqn := msg.GetName()
			if pkg != "" {
				msgFqn = pkg + "." + msgFqn
			}
			collectDuplicateEnumValuesInMessage(fd.GetName(), msg, msgFqn, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
	}
	return errs
}

func collectDuplicateEnumValuesInMessage(filename string, msg *descriptorpb.DescriptorProto, msgFqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, e := range msg.GetEnumType() {
		enumPath := append(append([]int32{}, msgPath...), 4, int32(i))
		collectDuplicateEnumValueErrors(filename, e, msgFqn, enumPath, sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := msgFqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectDuplicateEnumValuesInMessage(filename, nested, nestedFqn, nestedPath, sci, errs)
	}
}

func collectDuplicateEnumValueErrors(filename string, e *descriptorpb.EnumDescriptorProto, parentScope string, enumPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if e.GetOptions().GetAllowAlias() {
		return
	}
	// Map enum number -> first value name
	seen := make(map[int32]string)
	for i, val := range e.GetValue() {
		num := val.GetNumber()
		if firstName, ok := seen[num]; ok {
			line, col := findEnumValueNumberLocation(enumPath, i, sci)
			// Enum values are scoped to the parent (package or message), not the enum
			valFqn := parentScope + "." + val.GetName()
			firstFqn := parentScope + "." + firstName
			// Find next available enum value
			nextAvail := nextAvailableEnumValue(e)
			*errs = append(*errs, fmt.Sprintf(
				"%s:%d:%d: \"%s\" uses the same enum value as \"%s\". If this is intended, set 'option allow_alias = true;' to the enum definition. The next available enum value is %d.",
				filename, line, col, valFqn, firstFqn, nextAvail))
		} else {
			seen[num] = val.GetName()
		}
	}
}

func nextAvailableEnumValue(e *descriptorpb.EnumDescriptorProto) int32 {
	used := make(map[int32]bool)
	for _, val := range e.GetValue() {
		used[val.GetNumber()] = true
	}
	// Find smallest non-negative integer not in use
	for i := int32(0); ; i++ {
		if !used[i] {
			return i
		}
	}
}

// validateJsonNameConflicts checks that no two fields in a message have conflicting JSON names.
// C++ protoc runs two passes: one for default-only names, one considering custom json_names.
func validateJsonNameConflicts(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, explicitJsonNames map[*descriptorpb.FieldDescriptorProto]bool) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		// C++ protoc only treats JSON name conflicts as errors in proto3/editions, not proto2
		if fd.GetSyntax() != "proto3" && fd.GetSyntax() != "editions" {
			continue
		}
		for i, msg := range fd.GetMessageType() {
			collectJsonNameConflictErrors(fd.GetName(), msg, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), explicitJsonNames, false, &errs)
			collectJsonNameConflictErrors(fd.GetName(), msg, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), explicitJsonNames, true, &errs)
		}
	}
	return errs
}

type jsonNameEntry struct {
	fieldName string
	jsonName  string
	isCustom  bool
}

func collectJsonNameConflictErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, explicitJsonNames map[*descriptorpb.FieldDescriptorProto]bool, useCustom bool, errs *[]string) {
	seen := make(map[string]jsonNameEntry)
	for i, field := range msg.GetField() {
		defaultJsonName := tokenizer.ToJSONName(field.GetName())
		isCustom := false
		jsonName := defaultJsonName
		if useCustom && explicitJsonNames[field] && field.GetJsonName() != defaultJsonName {
			jsonName = field.GetJsonName()
			isCustom = true
		}
		if match, ok := seen[jsonName]; ok {
			if useCustom && !isCustom && !match.isCustom {
				// Both are default names — already reported by the non-custom pass
				continue
			}
			namePath := append(append([]int32{}, msgPath...), 2, int32(i), 1)
			line, col := findLocationByPath(namePath, sci)
			thisType := "default"
			if isCustom {
				thisType = "custom"
			}
			existingType := "default"
			if match.isCustom {
				existingType = "custom"
			}
			nameSuffix := ""
			if jsonName != match.jsonName {
				nameSuffix = fmt.Sprintf(" (\"%s\")", match.jsonName)
			}
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: The %s JSON name of field \"%s\" (\"%s\") conflicts with the %s JSON name of field \"%s\"%s.",
				filename, line, col, thisType, field.GetName(), jsonName, existingType, match.fieldName, nameSuffix))
		} else {
			seen[jsonName] = jsonNameEntry{fieldName: field.GetName(), jsonName: jsonName, isCustom: isCustom}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectJsonNameConflictErrors(filename, nested, nestedPath, sci, explicitJsonNames, useCustom, errs)
	}
}

// featureTargets maps FeatureSet field names to the entity types where they can be used.
var featureTargets = map[string]map[string]bool{
	"field_presence":         {"file": true, "field": true},
	"enum_type":              {"file": true, "enum": true},
	"repeated_field_encoding": {"file": true, "field": true},
	"utf8_validation":        {"file": true, "field": true},
	"message_encoding":       {"file": true, "field": true},
	"json_format":            {"file": true, "message": true, "enum": true},
}

// featureProtoNames maps FeatureSet field names to their full proto path names.
var featureProtoNames = map[string]string{
	"field_presence":         "google.protobuf.FeatureSet.field_presence",
	"enum_type":              "google.protobuf.FeatureSet.enum_type",
	"repeated_field_encoding": "google.protobuf.FeatureSet.repeated_field_encoding",
	"utf8_validation":        "google.protobuf.FeatureSet.utf8_validation",
	"message_encoding":       "google.protobuf.FeatureSet.message_encoding",
	"json_format":            "google.protobuf.FeatureSet.json_format",
}

func validateFeaturesEditions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() == "editions" {
			continue
		}
		if hasAnyFeatures(fd) {
			errs = append(errs, fmt.Sprintf("%s: Features are only valid under editions.", name))
		}
	}
	return errs
}

func hasAnyFeatures(fd *descriptorpb.FileDescriptorProto) bool {
	if fd.GetOptions() != nil && fd.GetOptions().GetFeatures() != nil {
		return true
	}
	for _, msg := range fd.GetMessageType() {
		if hasAnyFeaturesInMsg(msg) {
			return true
		}
	}
	for _, e := range fd.GetEnumType() {
		if hasAnyFeaturesInEnum(e) {
			return true
		}
	}
	for _, svc := range fd.GetService() {
		if svc.GetOptions() != nil && svc.GetOptions().GetFeatures() != nil {
			return true
		}
		for _, m := range svc.GetMethod() {
			if m.GetOptions() != nil && m.GetOptions().GetFeatures() != nil {
				return true
			}
		}
	}
	for _, ext := range fd.GetExtension() {
		if ext.GetOptions() != nil && ext.GetOptions().GetFeatures() != nil {
			return true
		}
	}
	return false
}

func hasAnyFeaturesInMsg(msg *descriptorpb.DescriptorProto) bool {
	if msg.GetOptions() != nil && msg.GetOptions().GetFeatures() != nil {
		return true
	}
	for _, f := range msg.GetField() {
		if f.GetOptions() != nil && f.GetOptions().GetFeatures() != nil {
			return true
		}
	}
	for _, o := range msg.GetOneofDecl() {
		if o.GetOptions() != nil && o.GetOptions().GetFeatures() != nil {
			return true
		}
	}
	for _, e := range msg.GetEnumType() {
		if hasAnyFeaturesInEnum(e) {
			return true
		}
	}
	for _, ext := range msg.GetExtension() {
		if ext.GetOptions() != nil && ext.GetOptions().GetFeatures() != nil {
			return true
		}
	}
	for _, nested := range msg.GetNestedType() {
		if hasAnyFeaturesInMsg(nested) {
			return true
		}
	}
	return false
}

func hasAnyFeaturesInEnum(e *descriptorpb.EnumDescriptorProto) bool {
	if e.GetOptions() != nil && e.GetOptions().GetFeatures() != nil {
		return true
	}
	for _, v := range e.GetValue() {
		if v.GetOptions() != nil && v.GetOptions().GetFeatures() != nil {
			return true
		}
	}
	return false
}

func validateFeatureTargets(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, svc := range fd.GetService() {
			if svc.GetOptions() != nil && svc.GetOptions().GetFeatures() != nil {
				feat := svc.GetOptions().GetFeatures()
				if feat.FieldPresence != nil {
					errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `service`.", name, featureProtoNames["field_presence"]))
				}
				if feat.EnumType != nil {
					errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `service`.", name, featureProtoNames["enum_type"]))
				}
				if feat.RepeatedFieldEncoding != nil {
					errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `service`.", name, featureProtoNames["repeated_field_encoding"]))
				}
				if feat.Utf8Validation != nil {
					errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `service`.", name, featureProtoNames["utf8_validation"]))
				}
				if feat.MessageEncoding != nil {
					errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `service`.", name, featureProtoNames["message_encoding"]))
				}
				if feat.JsonFormat != nil {
					errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `service`.", name, featureProtoNames["json_format"]))
				}
			}
			for _, method := range svc.GetMethod() {
				if method.GetOptions() != nil && method.GetOptions().GetFeatures() != nil {
					feat := method.GetOptions().GetFeatures()
					if feat.FieldPresence != nil {
						errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `method`.", name, featureProtoNames["field_presence"]))
					}
					if feat.EnumType != nil {
						errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `method`.", name, featureProtoNames["enum_type"]))
					}
					if feat.RepeatedFieldEncoding != nil {
						errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `method`.", name, featureProtoNames["repeated_field_encoding"]))
					}
					if feat.Utf8Validation != nil {
						errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `method`.", name, featureProtoNames["utf8_validation"]))
					}
					if feat.MessageEncoding != nil {
						errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `method`.", name, featureProtoNames["message_encoding"]))
					}
					if feat.JsonFormat != nil {
						errs = append(errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `method`.", name, featureProtoNames["json_format"]))
					}
				}
			}
		}
		for _, e := range fd.GetEnumType() {
			collectEnumFeatureErrors(name, e, &errs)
			collectEnumEntryFeatureErrors(name, e, &errs)
		}
		for _, msg := range fd.GetMessageType() {
			collectMessageFeatureErrors(name, msg, &errs)
			collectFieldFeatureErrorsInMsg(name, msg, &errs)
			collectEnumFeatureErrorsInMsg(name, msg, &errs)
			collectEnumEntryFeatureErrorsInMsg(name, msg, &errs)
			collectOneofFeatureErrorsInMsg(name, msg, &errs)
		}
		collectFieldFeatureErrorsForExtensions(name, fd.GetExtension(), &errs)
	}
	return errs
}

func collectMessageFeatureErrors(filename string, msg *descriptorpb.DescriptorProto, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	if msg.GetOptions() != nil && msg.GetOptions().GetFeatures() != nil {
		feat := msg.GetOptions().GetFeatures()
		if feat.FieldPresence != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `message`.", filename, featureProtoNames["field_presence"]))
		}
		if feat.EnumType != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `message`.", filename, featureProtoNames["enum_type"]))
		}
		if feat.RepeatedFieldEncoding != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `message`.", filename, featureProtoNames["repeated_field_encoding"]))
		}
		if feat.Utf8Validation != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `message`.", filename, featureProtoNames["utf8_validation"]))
		}
		if feat.MessageEncoding != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `message`.", filename, featureProtoNames["message_encoding"]))
		}
		// json_format targets MESSAGE, so it's allowed — skip it
	}
	for _, nested := range msg.GetNestedType() {
		collectMessageFeatureErrors(filename, nested, errs)
	}
}

func collectOneofFeatureErrors(filename string, msg *descriptorpb.DescriptorProto, errs *[]string) {
	for _, oneof := range msg.GetOneofDecl() {
		if oneof.GetOptions() != nil && oneof.GetOptions().GetFeatures() != nil {
			feat := oneof.GetOptions().GetFeatures()
			if feat.FieldPresence != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `oneof`.", filename, featureProtoNames["field_presence"]))
			}
			if feat.EnumType != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `oneof`.", filename, featureProtoNames["enum_type"]))
			}
			if feat.RepeatedFieldEncoding != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `oneof`.", filename, featureProtoNames["repeated_field_encoding"]))
			}
			if feat.Utf8Validation != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `oneof`.", filename, featureProtoNames["utf8_validation"]))
			}
			if feat.MessageEncoding != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `oneof`.", filename, featureProtoNames["message_encoding"]))
			}
			if feat.JsonFormat != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `oneof`.", filename, featureProtoNames["json_format"]))
			}
		}
	}
}

func collectOneofFeatureErrorsInMsg(filename string, msg *descriptorpb.DescriptorProto, errs *[]string) {
	collectOneofFeatureErrors(filename, msg, errs)
	for _, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		collectOneofFeatureErrorsInMsg(filename, nested, errs)
	}
}

func collectEnumFeatureErrors(filename string, e *descriptorpb.EnumDescriptorProto, errs *[]string) {
	if e.GetOptions() != nil && e.GetOptions().GetFeatures() != nil {
		feat := e.GetOptions().GetFeatures()
		if feat.FieldPresence != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum`.", filename, featureProtoNames["field_presence"]))
		}
		if feat.RepeatedFieldEncoding != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum`.", filename, featureProtoNames["repeated_field_encoding"]))
		}
		if feat.Utf8Validation != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum`.", filename, featureProtoNames["utf8_validation"]))
		}
		if feat.MessageEncoding != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum`.", filename, featureProtoNames["message_encoding"]))
		}
		// json_format targets ENUM, so it's allowed — skip it
	}
}

func collectEnumFeatureErrorsInMsg(filename string, msg *descriptorpb.DescriptorProto, errs *[]string) {
	for _, e := range msg.GetEnumType() {
		collectEnumFeatureErrors(filename, e, errs)
	}
	for _, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		collectEnumFeatureErrorsInMsg(filename, nested, errs)
	}
}

func collectEnumEntryFeatureErrors(filename string, e *descriptorpb.EnumDescriptorProto, errs *[]string) {
	for _, val := range e.GetValue() {
		if val.GetOptions() != nil && val.GetOptions().GetFeatures() != nil {
			feat := val.GetOptions().GetFeatures()
			if feat.FieldPresence != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum entry`.", filename, featureProtoNames["field_presence"]))
			}
			if feat.EnumType != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum entry`.", filename, featureProtoNames["enum_type"]))
			}
			if feat.RepeatedFieldEncoding != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum entry`.", filename, featureProtoNames["repeated_field_encoding"]))
			}
			if feat.Utf8Validation != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum entry`.", filename, featureProtoNames["utf8_validation"]))
			}
			if feat.MessageEncoding != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum entry`.", filename, featureProtoNames["message_encoding"]))
			}
			if feat.JsonFormat != nil {
				*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `enum entry`.", filename, featureProtoNames["json_format"]))
			}
		}
	}
}

func collectEnumEntryFeatureErrorsInMsg(filename string, msg *descriptorpb.DescriptorProto, errs *[]string) {
	for _, e := range msg.GetEnumType() {
		collectEnumEntryFeatureErrors(filename, e, errs)
	}
	for _, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		collectEnumEntryFeatureErrorsInMsg(filename, nested, errs)
	}
}
func collectFieldFeatureErrors(filename string, field *descriptorpb.FieldDescriptorProto, errs *[]string) {
	if field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil {
		feat := field.GetOptions().GetFeatures()
		// field_presence, repeated_field_encoding, utf8_validation, message_encoding target FIELD — allowed
		// enum_type targets ENUM — not allowed on field
		if feat.EnumType != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `field`.", filename, featureProtoNames["enum_type"]))
		}
		// json_format targets MESSAGE — not allowed on field
		if feat.JsonFormat != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Option %s cannot be set on an entity of type `field`.", filename, featureProtoNames["json_format"]))
		}
	}
}

func collectFieldFeatureErrorsInMsg(filename string, msg *descriptorpb.DescriptorProto, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for _, field := range msg.GetField() {
		collectFieldFeatureErrors(filename, field, errs)
	}
	for _, ext := range msg.GetExtension() {
		collectFieldFeatureErrors(filename, ext, errs)
	}
	for _, nested := range msg.GetNestedType() {
		collectFieldFeatureErrorsInMsg(filename, nested, errs)
	}
}

func collectFieldFeatureErrorsForExtensions(filename string, exts []*descriptorpb.FieldDescriptorProto, errs *[]string) {
	for _, ext := range exts {
		collectFieldFeatureErrors(filename, ext, errs)
	}
}

func validateProto3(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "proto3" {
			continue
		}
		collectProto3ExtendErrors(fd.GetName(), fd.GetExtension(), []int32{7}, fd.GetSourceCodeInfo(), &errs)
		for i, e := range fd.GetEnumType() {
			collectProto3EnumZeroErrors(fd.GetName(), e, []int32{5, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
		for i, msg := range fd.GetMessageType() {
			collectProto3MessageErrors(fd.GetName(), msg, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
	}
	return errs
}

func allowedProto3Extendee(name string) bool {
	// Strip leading dot from fully-qualified name
	if len(name) > 0 && name[0] == '.' {
		name = name[1:]
	}
	if len(name) <= 16 || name[:16] != "google.protobuf." {
		return false
	}
	switch name[16:] {
	case "FileOptions", "MessageOptions", "FieldOptions", "OneofOptions",
		"ExtensionRangeOptions", "EnumOptions", "EnumValueOptions",
		"ServiceOptions", "MethodOptions", "StreamOptions",
		"SourceCodeInfo", "GeneratedCodeInfo", "FeatureSet":
		return true
	}
	return false
}

func collectProto3ExtendErrors(filename string, exts []*descriptorpb.FieldDescriptorProto, basePath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, ext := range exts {
		if !allowedProto3Extendee(ext.GetExtendee()) {
			// Location from extendee SCI path: basePath + [extIdx, 2]
			path := append(append([]int32{}, basePath...), int32(i), 2)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extensions in proto3 are only allowed for defining options.", filename, line, col))
		}
	}
}

func collectProto3EnumZeroErrors(filename string, e *descriptorpb.EnumDescriptorProto, enumPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	vals := e.GetValue()
	if len(vals) > 0 && vals[0].GetNumber() != 0 {
		line, col := findEnumValueNumberLocation(enumPath, 0, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: The first enum value must be zero for open enums.", filename, line, col))
	}
}

func findEnumValueNumberLocation(enumPath []int32, valueIdx int, sci *descriptorpb.SourceCodeInfo) (int, int) {
	if sci == nil {
		return 0, 0
	}
	// Path: enumPath + [2, valueIdx, 2] where 2=value field, 2=number field
	target := append(append([]int32{}, enumPath...), 2, int32(valueIdx), 2)
	for _, loc := range sci.GetLocation() {
		path := loc.GetPath()
		if len(path) == len(target) {
			match := true
			for i := range path {
				if path[i] != target[i] {
					match = false
					break
				}
			}
			if match {
				span := loc.GetSpan()
				if len(span) >= 2 {
					return int(span[0]) + 1, int(span[1]) + 1
				}
			}
		}
	}
	return 0, 0
}

func collectProto3DefaultErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for _, field := range msg.GetField() {
		if field.DefaultValue != nil {
			line, col := findDefaultValueLocation(field, msg, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Explicit default values are not allowed in proto3.", filename, line, col))
		}
	}
}

func collectProto3RequiredErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REQUIRED {
			line, col := findFieldTypeLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Required fields are not allowed in proto3.", filename, line, col))
		}
	}
}

func findFieldTypeLocation(msgPath []int32, fieldIdx int, sci *descriptorpb.SourceCodeInfo) (int, int) {
	if sci == nil {
		return 0, 0
	}
	// Path: msgPath + [2, fieldIdx, 5] where 2=field, 5=type
	target := append(append([]int32{}, msgPath...), 2, int32(fieldIdx), 5)
	for _, loc := range sci.GetLocation() {
		path := loc.GetPath()
		if len(path) == len(target) {
			match := true
			for i := range path {
				if path[i] != target[i] {
					match = false
					break
				}
			}
			if match {
				span := loc.GetSpan()
				if len(span) >= 2 {
					return int(span[0]) + 1, int(span[1]) + 1
				}
			}
		}
	}
	return 0, 0
}

func collectProto3MessageSetErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMessageSetWireFormat() {
		namePath := append(append([]int32{}, msgPath...), 1)
		line, col := findLocationByPath(namePath, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: MessageSet is not supported in proto3.", filename, line, col))
	}
}

func collectProto3ExtensionRangeErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i := range msg.GetExtensionRange() {
		path := append(append([]int32{}, msgPath...), 5, int32(i), 1)
		line, col := findLocationByPath(path, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extension ranges are not allowed in proto3.", filename, line, col))
	}
}

func collectProto3GroupErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP {
			line, col := findFieldTypeLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Groups are not supported in proto3 syntax.", filename, line, col))
		}
	}
}

func collectProto3MessageErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	collectProto3MessageSetErrors(filename, msg, msgPath, sci, errs)
	collectProto3ExtensionRangeErrors(filename, msg, msgPath, sci, errs)
	collectProto3ExtendErrors(filename, msg.GetExtension(), append(append([]int32{}, msgPath...), 6), sci, errs)
	collectProto3GroupErrors(filename, msg, msgPath, sci, errs)
	collectProto3RequiredErrors(filename, msg, msgPath, sci, errs)
	collectProto3DefaultErrors(filename, msg, msgPath, sci, errs)
	for i, e := range msg.GetEnumType() {
		collectProto3EnumZeroErrors(filename, e, append(append([]int32{}, msgPath...), 4, int32(i)), sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		collectProto3MessageErrors(filename, nested, append(append([]int32{}, msgPath...), 3, int32(i)), sci, errs)
	}
}

func findDefaultValueLocation(field *descriptorpb.FieldDescriptorProto, msg *descriptorpb.DescriptorProto, sci *descriptorpb.SourceCodeInfo) (int, int) {
	if sci == nil {
		return 0, 0
	}
	// Find field index in message
	fieldIdx := -1
	for i, f := range msg.GetField() {
		if f == field {
			fieldIdx = i
			break
		}
	}
	if fieldIdx < 0 {
		return 0, 0
	}
	// Search for source code info location with path ending in [2, fieldIdx, 7]
	// where 7 = default_value field number of FieldDescriptorProto
	for _, loc := range sci.GetLocation() {
		path := loc.GetPath()
		if len(path) >= 3 &&
			path[len(path)-1] == 7 &&
			path[len(path)-2] == int32(fieldIdx) &&
			path[len(path)-3] == 2 {
			span := loc.GetSpan()
			if len(span) >= 2 {
				return int(span[0]) + 1, int(span[1]) + 1
			}
		}
	}
	return 0, 0
}

// validateDuplicateNames checks that no two symbols share the same fully-qualified name.
func validateDuplicateNames(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		sci := fd.GetSourceCodeInfo()

		seen := make(map[string]bool)
		// Track which FQNs are enum values and their parent enum name
		enumValParent := make(map[string]string) // fqn -> enum short name
		check := func(fqn, shortName, scope string, line, col int, enumName string) {
			if seen[fqn] {
				var errMsg string
				if line > 0 && col > 0 {
					errMsg = fmt.Sprintf("%s:%d:%d: \"%s\" is already defined in \"%s\".",
						fd.GetName(), line, col, shortName, scope)
				} else {
					errMsg = fmt.Sprintf("%s: \"%s\" is already defined in \"%s\".",
						fd.GetName(), shortName, scope)
				}
				errs = append(errs, errMsg)
				// If the new symbol is an enum value, add the scoping note
				if enumName != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Note that enum values use C++ scoping rules, meaning that enum values are siblings of their type, not children of it.  Therefore, \"%s\" must be unique within \"%s\", not just within \"%s\".",
						fd.GetName(), line, col, shortName, scope, enumName))
				}
			} else {
				seen[fqn] = true
				if enumName != "" {
					enumValParent[fqn] = enumName
				}
			}
		}

		// C++ protoc registers children before parent (AddSymbol is called after
		// building nested types/fields/etc. in BuildMessage/BuildEnum/BuildService).
		for i, msg := range fd.GetMessageType() {
			msgFQN := msg.GetName()
			if pkg != "" {
				msgFQN = pkg + "." + msgFQN
			}
			collectDupNamesInMsg(msg, msgFQN, []int32{4, int32(i)}, sci, check)
			line, col := findLocationByPath([]int32{4, int32(i), 1}, sci)
			check(msgFQN, msg.GetName(), pkg, line, col, "")
		}

		for i, enum := range fd.GetEnumType() {
			enumFQN := enum.GetName()
			if pkg != "" {
				enumFQN = pkg + "." + enumFQN
			}
			for j, val := range enum.GetValue() {
				valFQN := val.GetName()
				if pkg != "" {
					valFQN = pkg + "." + valFQN
				}
				vl, vc := findLocationByPath([]int32{5, int32(i), 2, int32(j), 1}, sci)
				check(valFQN, val.GetName(), pkg, vl, vc, enum.GetName())
			}
			line, col := findLocationByPath([]int32{5, int32(i), 1}, sci)
			check(enumFQN, enum.GetName(), pkg, line, col, "")
		}

		for i, svc := range fd.GetService() {
			svcFQN := svc.GetName()
			if pkg != "" {
				svcFQN = pkg + "." + svcFQN
			}
			for j, method := range svc.GetMethod() {
				mFQN := svcFQN + "." + method.GetName()
				ml, mc := findLocationByPath([]int32{6, int32(i), 2, int32(j), 1}, sci)
				check(mFQN, method.GetName(), svcFQN, ml, mc, "")
			}
			line, col := findLocationByPath([]int32{6, int32(i), 1}, sci)
			check(svcFQN, svc.GetName(), pkg, line, col, "")
		}

		for i, ext := range fd.GetExtension() {
			extFQN := ext.GetName()
			if pkg != "" {
				extFQN = pkg + "." + extFQN
			}
			line, col := findLocationByPath([]int32{7, int32(i), 1}, sci)
			check(extFQN, ext.GetName(), pkg, line, col, "")
		}
	}
	return errs
}

func collectDupNamesInMsg(msg *descriptorpb.DescriptorProto, msgFQN string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, check func(fqn, shortName, scope string, line, col int, enumName string)) {
	// C++ protoc BuildMessage order: oneofs, fields, enums, nested_types, then AddSymbol for self.
	for _, oneof := range msg.GetOneofDecl() {
		oFQN := msgFQN + "." + oneof.GetName()
		// C++ protoc omits line:col for duplicate oneof names
		check(oFQN, oneof.GetName(), msgFQN, 0, 0, "")
	}
	for i, field := range msg.GetField() {
		fqn := msgFQN + "." + field.GetName()
		p := append(append([]int32{}, msgPath...), 2, int32(i), 1)
		l, c := findLocationByPath(p, sci)
		check(fqn, field.GetName(), msgFQN, l, c, "")
	}
	for i, ext := range msg.GetExtension() {
		eFQN := msgFQN + "." + ext.GetName()
		p := append(append([]int32{}, msgPath...), 6, int32(i), 1)
		l, c := findLocationByPath(p, sci)
		check(eFQN, ext.GetName(), msgFQN, l, c, "")
	}
	for i, enum := range msg.GetEnumType() {
		eFQN := msgFQN + "." + enum.GetName()
		for j, val := range enum.GetValue() {
			vFQN := msgFQN + "." + val.GetName()
			vl, vc := findLocationByPath(append(append([]int32{}, msgPath...), 4, int32(i), 2, int32(j), 1), sci)
			check(vFQN, val.GetName(), msgFQN, vl, vc, enum.GetName())
		}
		l, c := findLocationByPath(append(append([]int32{}, msgPath...), 4, int32(i), 1), sci)
		check(eFQN, enum.GetName(), msgFQN, l, c, "")
	}
	for i, nested := range msg.GetNestedType() {
		nFQN := msgFQN + "." + nested.GetName()
		np := append(append([]int32{}, msgPath...), 3, int32(i))
		collectDupNamesInMsg(nested, nFQN, np, sci, check)
		l, c := findLocationByPath(append(append([]int32{}, np...), 1), sci)
		check(nFQN, nested.GetName(), msgFQN, l, c, "")
	}
}

// validateMapEntryConflicts detects naming conflicts between expanded map entry
// types and existing nested types (C++ DetectMapConflicts).
func validateMapEntryConflicts(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			detectMapConflicts(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func detectMapConflicts(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	seenNames := make(map[string]bool)
	for _, nested := range msg.GetNestedType() {
		if seenNames[nested.GetName()] {
			// Duplicate found — if either is a map entry, report conflict
			isMapEntry := nested.GetOptions().GetMapEntry()
			if !isMapEntry {
				// Check if the previously-seen one is a map entry
				for _, other := range msg.GetNestedType() {
					if other.GetName() == nested.GetName() && other != nested && other.GetOptions().GetMapEntry() {
						isMapEntry = true
						break
					}
				}
			}
			if isMapEntry {
				l, c := findLocationByPath(append(append([]int32{}, msgPath...), 1), sci)
				if l > 0 && c > 0 {
					*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Expanded map entry type %s conflicts with an existing nested message type.",
						filename, l, c, nested.GetName()))
				} else {
					*errs = append(*errs, fmt.Sprintf("%s: Expanded map entry type %s conflicts with an existing nested message type.",
						filename, nested.GetName()))
				}
				break
			}
		}
		seenNames[nested.GetName()] = true
	}
	// Recurse into non-map-entry nested types
	for i, nested := range msg.GetNestedType() {
		if !nested.GetOptions().GetMapEntry() {
			np := append(append([]int32{}, msgPath...), 3, int32(i))
			detectMapConflicts(filename, nested, np, sci, errs)
		}
	}
}

// validateExtensionFieldConflicts checks that no field number in a message
// falls within the message's declared extension ranges.
func validateExtensionFieldConflicts(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, hints map[*descriptorpb.DescriptorProto]*messageHint) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectExtensionFieldConflictErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, sci, &errs, hints)
		}
	}
	return errs
}

func collectExtensionFieldConflictErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string, hints map[*descriptorpb.DescriptorProto]*messageHint) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for _, field := range msg.GetField() {
		num := field.GetNumber()
		for j, er := range msg.GetExtensionRange() {
			if num >= er.GetStart() && num < er.GetEnd() {
				path := append(append([]int32{}, msgPath...), 5, int32(j), 1)
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extension range %d to %d includes field \"%s\" (%d).",
					filename, line, col,
					er.GetStart(), er.GetEnd()-1,
					field.GetName(), num))
				requestHint(hints, msg, filename, fqn, line, col, 0, 1)
				break
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := fqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectExtensionFieldConflictErrors(filename, nested, nestedFqn, nestedPath, sci, errs, hints)
	}
}

// validateReservedRangeOverlaps checks that reserved ranges within a message don't overlap.
func validateReservedRangeOverlaps(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectReservedRangeOverlapErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectReservedRangeOverlapErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	ranges := msg.GetReservedRange()
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			if ranges[i].GetEnd() > ranges[j].GetStart() && ranges[j].GetEnd() > ranges[i].GetStart() {
				path := append(append([]int32{}, msgPath...), 9, int32(i), 1)
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Reserved range %d to %d overlaps with already-defined range %d to %d.",
					filename, line, col,
					ranges[j].GetStart(), ranges[j].GetEnd()-1,
					ranges[i].GetStart(), ranges[i].GetEnd()-1))
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectReservedRangeOverlapErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateEnumReservedRangeOverlaps checks that reserved ranges within an enum don't overlap.
func validateEnumReservedRangeOverlaps(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, enum := range fd.GetEnumType() {
			collectEnumReservedRangeOverlapErrors(fd.GetName(), enum, []int32{5, int32(i)}, sci, &errs)
		}
		for i, msg := range fd.GetMessageType() {
			collectEnumReservedRangeOverlapInMsg(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectEnumReservedRangeOverlapErrors(filename string, enum *descriptorpb.EnumDescriptorProto, enumPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	ranges := enum.GetReservedRange()
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			// Enum reserved ranges have inclusive end
			if ranges[i].GetStart() <= ranges[j].GetEnd() && ranges[j].GetStart() <= ranges[i].GetEnd() {
				path := append(append([]int32{}, enumPath...), 4, int32(i), 1)
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Reserved range %d to %d overlaps with already-defined range %d to %d.",
					filename, line, col,
					ranges[j].GetStart(), ranges[j].GetEnd(),
					ranges[i].GetStart(), ranges[i].GetEnd()))
			}
		}
	}
}

func collectEnumReservedRangeOverlapInMsg(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, enum := range msg.GetEnumType() {
		enumPath := append(append([]int32{}, msgPath...), 4, int32(i))
		collectEnumReservedRangeOverlapErrors(filename, enum, enumPath, sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectEnumReservedRangeOverlapInMsg(filename, nested, nestedPath, sci, errs)
	}
}

// validateEnumReservedValueConflicts checks that no enum value uses a reserved number.
func validateEnumReservedValueConflicts(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, enum := range fd.GetEnumType() {
			collectEnumReservedValueConflictErrors(fd.GetName(), enum, []int32{5, int32(i)}, sci, &errs)
		}
		for i, msg := range fd.GetMessageType() {
			collectEnumReservedValueConflictInMsg(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectEnumReservedValueConflictErrors(filename string, enum *descriptorpb.EnumDescriptorProto, enumPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for _, val := range enum.GetValue() {
		num := val.GetNumber()
		for j, rr := range enum.GetReservedRange() {
			// Enum reserved ranges have inclusive end
			if num >= rr.GetStart() && num <= rr.GetEnd() {
				// Location at the reserved range start number
				path := append(append([]int32{}, enumPath...), 4, int32(j), 1)
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Enum value \"%s\" uses reserved number %d.",
					filename, line, col, val.GetName(), num))
				break
			}
		}
	}
}

func collectEnumReservedValueConflictInMsg(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, enum := range msg.GetEnumType() {
		enumPath := append(append([]int32{}, msgPath...), 4, int32(i))
		collectEnumReservedValueConflictErrors(filename, enum, enumPath, sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectEnumReservedValueConflictInMsg(filename, nested, nestedPath, sci, errs)
	}
}

// validateEnumReservedNameConflicts checks that no enum value uses a reserved name.
func validateEnumReservedNameConflicts(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, enum := range fd.GetEnumType() {
			collectEnumReservedNameConflictErrors(fd.GetName(), enum, []int32{5, int32(i)}, sci, &errs)
		}
		for i, msg := range fd.GetMessageType() {
			collectEnumReservedNameConflictInMsg(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectEnumReservedNameConflictErrors(filename string, enum *descriptorpb.EnumDescriptorProto, enumPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	reserved := make(map[string]bool)
	for _, rn := range enum.GetReservedName() {
		reserved[rn] = true
	}
	for i, val := range enum.GetValue() {
		if reserved[val.GetName()] {
			// Location at the enum value name (field 1 of EnumValueDescriptorProto)
			path := append(append([]int32{}, enumPath...), 2, int32(i), 1)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Enum value \"%s\" is reserved.",
				filename, line, col, val.GetName()))
		}
	}
}

func collectEnumReservedNameConflictInMsg(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, enum := range msg.GetEnumType() {
		enumPath := append(append([]int32{}, msgPath...), 4, int32(i))
		collectEnumReservedNameConflictErrors(filename, enum, enumPath, sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectEnumReservedNameConflictInMsg(filename, nested, nestedPath, sci, errs)
	}
}

// validateExtensionRangeOverlaps checks that extension ranges within a message don't overlap.
func validateExtensionRangeOverlaps(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectExtensionRangeOverlapErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectExtensionRangeOverlapErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	ranges := msg.GetExtensionRange()
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			if ranges[i].GetEnd() > ranges[j].GetStart() && ranges[j].GetEnd() > ranges[i].GetStart() {
				path := append(append([]int32{}, msgPath...), 5, int32(i), 1)
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extension range %d to %d overlaps with already-defined range %d to %d.",
					filename, line, col,
					ranges[j].GetStart(), ranges[j].GetEnd()-1,
					ranges[i].GetStart(), ranges[i].GetEnd()-1))
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectExtensionRangeOverlapErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateExtensionReservedOverlaps checks that extension ranges don't overlap with reserved ranges.
func validateExtensionReservedOverlaps(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectExtensionReservedOverlapErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectExtensionReservedOverlapErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	extRanges := msg.GetExtensionRange()
	resRanges := msg.GetReservedRange()
	for i, ext := range extRanges {
		for _, res := range resRanges {
			if ext.GetEnd() > res.GetStart() && res.GetEnd() > ext.GetStart() {
				path := append(append([]int32{}, msgPath...), 5, int32(i), 1)
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extension range %d to %d overlaps with reserved range %d to %d.",
					filename, line, col,
					ext.GetStart(), ext.GetEnd()-1,
					res.GetStart(), res.GetEnd()-1))
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectExtensionReservedOverlapErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateRequiredExtensions checks that no extension field uses the required label.
func validateRequiredExtensions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		pkg := fd.GetPackage()
		// File-level extensions
		for i, ext := range fd.GetExtension() {
			if ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REQUIRED {
				fqn := ext.GetName()
				if pkg != "" {
					fqn = pkg + "." + fqn
				}
				line, col := findLocationByPath([]int32{7, int32(i), 5}, sci)
				errs = append(errs, fmt.Sprintf("%s:%d:%d: The extension %s cannot be required.",
					fd.GetName(), line, col, fqn))
			}
		}
		// Message-level extensions
		for i, msg := range fd.GetMessageType() {
			collectRequiredExtensionErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, pkg, &errs)
		}
	}
	return errs
}

func collectRequiredExtensionErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, prefix string, errs *[]string) {
	fqn := msg.GetName()
	if prefix != "" {
		fqn = prefix + "." + fqn
	}
	for i, ext := range msg.GetExtension() {
		if ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REQUIRED {
			extFQN := fqn + "." + ext.GetName()
			path := append(append([]int32{}, msgPath...), 6, int32(i), 5)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: The extension %s cannot be required.",
				filename, line, col, extFQN))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectRequiredExtensionErrors(filename, nested, nestedPath, sci, fqn, errs)
	}
}

// validateExtensionJsonName checks that no extension field has an explicit json_name option.
func validateExtensionJsonName(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, explicitJsonNames map[*descriptorpb.FieldDescriptorProto]bool) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		// File-level extensions
		for i, ext := range fd.GetExtension() {
			if explicitJsonNames[ext] {
				path := []int32{7, int32(i), 10}
				line, col := findLocationByPath(path, sci)
				errs = append(errs, fmt.Sprintf("%s:%d:%d: option json_name is not allowed on extension fields.",
					fd.GetName(), line, col))
			}
		}
		// Message-level extensions
		for i, msg := range fd.GetMessageType() {
			collectExtensionJsonNameErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, explicitJsonNames, &errs)
		}
	}
	return errs
}

func collectExtensionJsonNameErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, explicitJsonNames map[*descriptorpb.FieldDescriptorProto]bool, errs *[]string) {
	for i, ext := range msg.GetExtension() {
		if explicitJsonNames[ext] {
			path := append(append([]int32{}, msgPath...), 6, int32(i), 10)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: option json_name is not allowed on extension fields.",
				filename, line, col))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectExtensionJsonNameErrors(filename, nested, nestedPath, sci, explicitJsonNames, errs)
	}
}

// validateMessageSetFields checks that messages with message_set_wire_format don't have regular fields.
func validateMessageSetFields(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectMessageSetFieldErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectMessageSetFieldErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMessageSetWireFormat() {
		for i := range msg.GetField() {
			namePath := append(append([]int32{}, msgPath...), 2, int32(i), 1)
			line, col := findLocationByPath(namePath, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: MessageSets cannot have fields, only extensions.",
				filename, line, col))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectMessageSetFieldErrors(filename, nested, nestedPath, sci, errs)
	}
}

// extInfo records where an extension field was defined so we can report duplicates.
type extInfo struct {
	fqn      string // fully-qualified name of the extension field
	filename string
	number   int32
	sciPath  []int32
	sci      *descriptorpb.SourceCodeInfo
}

// validateDuplicateExtensionNumbers checks that no two extensions for the same
// message use the same field number.
func validateDuplicateExtensionNumbers(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	// Collect all extensions keyed by extendee FQN (without leading dot).
	extsByMsg := map[string][]extInfo{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		sci := fd.GetSourceCodeInfo()
		// File-level extensions
		for i, ext := range fd.GetExtension() {
			extendee := strings.TrimPrefix(ext.GetExtendee(), ".")
			fqn := ext.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			extsByMsg[extendee] = append(extsByMsg[extendee], extInfo{
				fqn:      fqn,
				filename: fd.GetName(),
				number:   ext.GetNumber(),
				sciPath:  []int32{7, int32(i), 3},
				sci:      sci,
			})
		}
		// Message-level extensions
		for i, msg := range fd.GetMessageType() {
			msgFQN := msg.GetName()
			if pkg != "" {
				msgFQN = pkg + "." + msgFQN
			}
			collectExtInfoInMsg(fd.GetName(), msg, msgFQN, []int32{4, int32(i)}, sci, extsByMsg)
		}
	}
	var errs []string
	for extendee, exts := range extsByMsg {
		seen := map[int32]string{} // number -> first extension FQN
		for _, ei := range exts {
			if first, ok := seen[ei.number]; ok {
				line, col := findLocationByPath(ei.sciPath, ei.sci)
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Extension number %d has already been used in \"%s\" by extension \"%s\".",
					ei.filename, line, col, ei.number, extendee, first))
			} else {
				seen[ei.number] = ei.fqn
			}
		}
	}
	return errs
}

func collectExtInfoInMsg(filename string, msg *descriptorpb.DescriptorProto, msgFQN string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, out map[string][]extInfo) {
	for i, ext := range msg.GetExtension() {
		extendee := strings.TrimPrefix(ext.GetExtendee(), ".")
		fqn := msgFQN + "." + ext.GetName()
		path := append(append([]int32{}, msgPath...), 6, int32(i), 3)
		out[extendee] = append(out[extendee], extInfo{
			fqn:      fqn,
			filename: filename,
			number:   ext.GetNumber(),
			sciPath:  path,
			sci:      sci,
		})
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		nestedFQN := msgFQN + "." + nested.GetName()
		collectExtInfoInMsg(filename, nested, nestedFQN, nestedPath, sci, out)
	}
}

// validateExtensionRanges checks that extension field numbers fall within
// the extendee message's declared extension ranges.
func validateExtensionRanges(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	// Build map of message FQN -> extension ranges
	msgRanges := map[string][]*descriptorpb.DescriptorProto_ExtensionRange{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for _, msg := range fd.GetMessageType() {
			collectExtensionRanges(msg, pkg, msgRanges)
		}
	}

	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		// File-level extensions
		for i, ext := range fd.GetExtension() {
			extendee := strings.TrimPrefix(ext.GetExtendee(), ".")
			ranges := msgRanges[extendee]
			if !isInExtensionRange(ext.GetNumber(), ranges) {
				line, col := findLocationByPath([]int32{7, int32(i), 3}, sci)
				errs = append(errs, fmt.Sprintf("%s:%d:%d: \"%s\" does not declare %d as an extension number.",
					fd.GetName(), line, col, extendee, ext.GetNumber()))
			}
		}
		// Message-level extensions
		for i, msg := range fd.GetMessageType() {
			collectExtensionRangeErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, msgRanges, &errs)
		}
	}
	return errs
}

func collectExtensionRanges(msg *descriptorpb.DescriptorProto, prefix string, out map[string][]*descriptorpb.DescriptorProto_ExtensionRange) {
	fqn := msg.GetName()
	if prefix != "" {
		fqn = prefix + "." + fqn
	}
	if len(msg.GetExtensionRange()) > 0 {
		out[fqn] = msg.GetExtensionRange()
	}
	for _, nested := range msg.GetNestedType() {
		collectExtensionRanges(nested, fqn, out)
	}
}

func isInExtensionRange(number int32, ranges []*descriptorpb.DescriptorProto_ExtensionRange) bool {
	for _, r := range ranges {
		if number >= r.GetStart() && number < r.GetEnd() {
			return true
		}
	}
	return false
}

func isProtoIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
				return false
			}
		} else {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
				return false
			}
		}
	}
	return true
}

func collectExtensionRangeErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, msgRanges map[string][]*descriptorpb.DescriptorProto_ExtensionRange, errs *[]string) {
	for i, ext := range msg.GetExtension() {
		extendee := strings.TrimPrefix(ext.GetExtendee(), ".")
		ranges := msgRanges[extendee]
		if !isInExtensionRange(ext.GetNumber(), ranges) {
			path := append(append([]int32{}, msgPath...), 6, int32(i), 3)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: \"%s\" does not declare %d as an extension number.",
				filename, line, col, extendee, ext.GetNumber()))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectExtensionRangeErrors(filename, nested, nestedPath, sci, msgRanges, errs)
	}
}

func findLocationByPath(target []int32, sci *descriptorpb.SourceCodeInfo) (int, int) {
	if sci == nil {
		return 0, 0
	}
	for _, loc := range sci.GetLocation() {
		path := loc.GetPath()
		if len(path) != len(target) {
			continue
		}
		match := true
		for i := range path {
			if path[i] != target[i] {
				match = false
				break
			}
		}
		if match {
			span := loc.GetSpan()
			if len(span) >= 2 {
				return int(span[0]) + 1, int(span[1]) + 1
			}
		}
	}
	return 0, 0
}

// stripSourceRetention returns a copy of fd with source-retention-only options removed.
// If the descriptor has no such options, returns the original to avoid cloning.
func stripSourceRetention(fd *descriptorpb.FileDescriptorProto) *descriptorpb.FileDescriptorProto {
	if !hasExtRangeOpts(fd.GetMessageType()) {
		return fd
	}
	fdCopy := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	clearExtRangeOpts(fdCopy.GetMessageType())
	if fdCopy.SourceCodeInfo != nil {
		var filtered []*descriptorpb.SourceCodeInfo_Location
		for _, loc := range fdCopy.SourceCodeInfo.Location {
			if !isExtRangeOptsPath(loc.Path) {
				filtered = append(filtered, loc)
			}
		}
		fdCopy.SourceCodeInfo.Location = filtered
	}
	return fdCopy
}

func hasExtRangeOpts(msgs []*descriptorpb.DescriptorProto) bool {
	for _, msg := range msgs {
		for _, er := range msg.GetExtensionRange() {
			if er.Options != nil {
				return true
			}
		}
		if hasExtRangeOpts(msg.GetNestedType()) {
			return true
		}
	}
	return false
}

func clearExtRangeOpts(msgs []*descriptorpb.DescriptorProto) {
	for _, msg := range msgs {
		for _, er := range msg.GetExtensionRange() {
			if er.Options != nil {
				er.Options.Verification = nil
				er.Options.Declaration = nil
				if proto.Size(er.Options) == 0 {
					er.Options = nil
				}
			}
		}
		clearExtRangeOpts(msg.GetNestedType())
	}
}

// isExtRangeOptsPath checks if a SCI path points to an extension range options field.
// Pattern: 4, N, [3, N, ]*, 5, N, 3, [...]
func isExtRangeOptsPath(path []int32) bool {
	if len(path) < 5 || path[0] != 4 {
		return false
	}
	i := 2
	for i+2 < len(path) {
		if path[i] == 5 && i+2 < len(path) && path[i+2] == 3 {
			return true
		}
		if path[i] == 3 {
			i += 2 // nested_type
		} else {
			return false
		}
	}
	return false
}

// validateRepeatedDefault checks that repeated fields don't have default values.
func validateRepeatedDefault(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectRepeatedDefaultErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectRepeatedDefaultErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for i, field := range msg.GetField() {
		if field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED && field.DefaultValue != nil {
			path := append(append([]int32{}, msgPath...), 2, int32(i), 7)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Repeated fields can't have default values.", filename, line, col))
		}
	}
	for i, nested := range msg.GetNestedType() {
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectRepeatedDefaultErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateMessageDefault checks that message/group-typed fields don't have default values.
func validateMessageDefault(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectMessageDefaultErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectMessageDefaultErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for i, field := range msg.GetField() {
		if (field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE ||
			field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP) && field.DefaultValue != nil {
			path := append(append([]int32{}, msgPath...), 2, int32(i), 7)
			line, col := findLocationByPath(path, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Messages can't have default values.", filename, line, col))
		}
	}
	for i, nested := range msg.GetNestedType() {
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectMessageDefaultErrors(filename, nested, nestedPath, sci, errs)
	}
}

// validateEnumDefaultValues checks that enum fields with default values reference valid enum value names.
func validateEnumDefaultValues(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	enumValues := map[string]map[string]bool{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		prefix := ""
		if pkg != "" {
			prefix = "." + pkg
		}
		collectEnumValueNames(fd.GetEnumType(), prefix, enumValues)
		collectEnumValueNamesInMsgs(fd.GetMessageType(), prefix, enumValues)
	}

	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		prefix := ""
		if pkg != "" {
			prefix = "." + pkg
		}
		sci := fd.GetSourceCodeInfo()
		collectEnumDefaultErrors(fd.GetMessageType(), prefix, []int32{4}, sci, name, enumValues, &errs)
	}
	return errs
}

func collectEnumValueNames(enums []*descriptorpb.EnumDescriptorProto, prefix string, out map[string]map[string]bool) {
	for _, e := range enums {
		fqn := prefix + "." + e.GetName()
		vals := map[string]bool{}
		for _, v := range e.GetValue() {
			vals[v.GetName()] = true
		}
		out[fqn] = vals
	}
}

func collectEnumValueNamesInMsgs(msgs []*descriptorpb.DescriptorProto, prefix string, out map[string]map[string]bool) {
	for _, msg := range msgs {
		msgFQN := prefix + "." + msg.GetName()
		collectEnumValueNames(msg.GetEnumType(), msgFQN, out)
		collectEnumValueNamesInMsgs(msg.GetNestedType(), msgFQN, out)
	}
}

func collectEnumDefaultErrors(msgs []*descriptorpb.DescriptorProto, prefix string, basePath []int32, sci *descriptorpb.SourceCodeInfo, filename string, enumValues map[string]map[string]bool, errs *[]string) {
	for msgIdx, msg := range msgs {
		if msg.GetOptions().GetMapEntry() {
			continue
		}
		msgFQN := prefix + "." + msg.GetName()
		msgPath := append(append([]int32{}, basePath...), int32(msgIdx))
		for fieldIdx, field := range msg.GetField() {
			if field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_ENUM {
				continue
			}
			if field.DefaultValue == nil {
				continue
			}
			enumFQN := field.GetTypeName()
			vals, ok := enumValues[enumFQN]
			if !ok {
				continue
			}
			defVal := field.GetDefaultValue()
			enumName := enumFQN
			if len(enumName) > 0 && enumName[0] == '.' {
				enumName = enumName[1:]
			}
			path := append(append([]int32{}, msgPath...), 2, int32(fieldIdx), 7)
			if !isProtoIdentifier(defVal) {
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Default value for an enum field must be an identifier.", filename, line, col))
			} else if !vals[defVal] {
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Enum type \"%s\" has no value named \"%s\".", filename, line, col, enumName, defVal))
			}
		}
		nestedBase := append(append([]int32{}, msgPath...), 3)
		collectEnumDefaultErrors(msg.GetNestedType(), msgFQN, nestedBase, sci, filename, enumValues, errs)
	}
}

// printFreeFieldNumbers recursively prints free field numbers for a message and its nested messages.
func printFreeFieldNumbers(fullName string, msg *descriptorpb.DescriptorProto) {
	type fieldRange struct{ start, end int }
	var ranges []fieldRange

	// Collect used field numbers
	for _, f := range msg.GetField() {
		n := int(f.GetNumber())
		ranges = append(ranges, fieldRange{n, n + 1})
	}
	// Collect extension ranges (already [start, end) in proto)
	for _, er := range msg.GetExtensionRange() {
		ranges = append(ranges, fieldRange{int(er.GetStart()), int(er.GetEnd())})
	}
	// Collect reserved ranges (already [start, end) in proto)
	for _, rr := range msg.GetReservedRange() {
		ranges = append(ranges, fieldRange{int(rr.GetStart()), int(rr.GetEnd())})
	}

	// Sort ranges
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start != ranges[j].start {
			return ranges[i].start < ranges[j].start
		}
		return ranges[i].end < ranges[j].end
	})

	// Print nested messages first (post-order: children before parent)
	for _, nested := range msg.GetNestedType() {
		printFreeFieldNumbers(fullName+"."+nested.GetName(), nested)
	}

	// Format free field numbers
	const kMaxNumber = 536870911 // 2^29 - 1
	output := fmt.Sprintf("%-35s free:", fullName)
	nextFree := 1
	for _, r := range ranges {
		if nextFree >= r.end {
			continue
		}
		if nextFree < r.start {
			if nextFree+1 == r.start {
				output += fmt.Sprintf(" %d", nextFree)
			} else {
				output += fmt.Sprintf(" %d-%d", nextFree, r.start-1)
			}
		}
		nextFree = r.end
	}
	if nextFree <= kMaxNumber {
		output += fmt.Sprintf(" %d-INF", nextFree)
	}
	fmt.Println(output)
}

