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

	// Resolve type references across all files (must happen after all files parsed)
	for _, name := range orderedFiles {
		parser.ResolveTypes(parsed[name], parsed)
	}

	// Validate duplicate symbol names
	if errs := validateDuplicateNames(orderedFiles, parsed); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Validate non-positive field numbers
	if errs := validatePositiveFieldNumbers(orderedFiles, parsed); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Validate max field numbers (> 536870911)
	if errs := validateMaxFieldNumbers(orderedFiles, parsed); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Validate reserved field numbers (applies to all syntaxes)
	if errs := validateReservedFieldNumbers(orderedFiles, parsed); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Validate duplicate field numbers
	if errs := validateDuplicateFieldNumbers(orderedFiles, parsed); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Validate duplicate enum values (without allow_alias)
	if errs := validateDuplicateEnumValues(orderedFiles, parsed); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Validate proto3 constraints
	if errs := validateProto3(orderedFiles, parsed); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
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
			relFileSet := make(map[string]bool)
			for _, name := range relFiles {
				relFileSet[name] = true
			}
			for _, name := range orderedFiles {
				if relFileSet[name] {
					fdCopy := proto.Clone(parsed[name]).(*descriptorpb.FileDescriptorProto)
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
		return fmt.Errorf("%s:%w", filename, err)
	}

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

// validatePositiveFieldNumbers checks that all field numbers are positive integers.
func validatePositiveFieldNumbers(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectPositiveFieldNumberErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
	}
	return errs
}

func collectPositiveFieldNumberErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.GetNumber() <= 0 {
			line, col := findFieldNumberLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field numbers must be positive integers.", filename, line, col))
			// Suggest next available field number
			next := suggestFieldNumber(msg)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Suggested field numbers for %s: %d", filename, line, col, fqn, next))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedFqn := fqn + "." + nested.GetName()
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectPositiveFieldNumberErrors(filename, nested, nestedFqn, nestedPath, sci, errs)
	}
}

func suggestFieldNumber(msg *descriptorpb.DescriptorProto) int32 {
	used := make(map[int32]bool)
	for _, field := range msg.GetField() {
		if field.GetNumber() > 0 {
			used[field.GetNumber()] = true
		}
	}
	var n int32 = 1
	for used[n] {
		n++
	}
	return n
}

const kMaxFieldNumber = 536870911 // 2^29 - 1

// validateMaxFieldNumbers checks that no field number exceeds 536870911.
func validateMaxFieldNumbers(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for i, msg := range fd.GetMessageType() {
			fqn := msg.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			collectMaxFieldNumberErrors(fd.GetName(), msg, fqn, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
		for _, ext := range fd.GetExtension() {
			if ext.GetNumber() > kMaxFieldNumber {
				errs = append(errs, fmt.Sprintf("%s: Field numbers cannot be greater than %d.", fd.GetName(), kMaxFieldNumber))
			}
		}
	}
	return errs
}

func collectMaxFieldNumberErrors(filename string, msg *descriptorpb.DescriptorProto, fqn string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.GetNumber() > kMaxFieldNumber {
			line, col := findFieldNumberLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field numbers cannot be greater than %d.", filename, line, col, kMaxFieldNumber))
			next := suggestFieldNumber(msg)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Suggested field numbers for %s: %d", filename, line, col, fqn, next))
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
		collectMaxFieldNumberErrors(filename, nested, nestedFqn, nestedPath, sci, errs)
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

// validateProto3 checks proto3-specific constraints and returns error strings.
func validateProto3(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "proto3" {
			continue
		}
		for i, e := range fd.GetEnumType() {
			collectProto3EnumZeroErrors(fd.GetName(), e, []int32{5, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
		for i, msg := range fd.GetMessageType() {
			collectProto3MessageErrors(fd.GetName(), msg, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
	}
	return errs
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

func collectProto3MessageErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
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
		check := func(fqn, shortName, scope string, line, col int) {
			if seen[fqn] {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: \"%s\" is already defined in \"%s\".",
					fd.GetName(), line, col, shortName, scope))
			} else {
				seen[fqn] = true
			}
		}

		for i, msg := range fd.GetMessageType() {
			msgFQN := msg.GetName()
			if pkg != "" {
				msgFQN = pkg + "." + msgFQN
			}
			line, col := findLocationByPath([]int32{4, int32(i), 1}, sci)
			check(msgFQN, msg.GetName(), pkg, line, col)
			collectDupNamesInMsg(msg, msgFQN, []int32{4, int32(i)}, sci, check)
		}

		for i, enum := range fd.GetEnumType() {
			enumFQN := enum.GetName()
			if pkg != "" {
				enumFQN = pkg + "." + enumFQN
			}
			line, col := findLocationByPath([]int32{5, int32(i), 1}, sci)
			check(enumFQN, enum.GetName(), pkg, line, col)
			for j, val := range enum.GetValue() {
				valFQN := val.GetName()
				if pkg != "" {
					valFQN = pkg + "." + valFQN
				}
				vl, vc := findLocationByPath([]int32{5, int32(i), 2, int32(j), 1}, sci)
				check(valFQN, val.GetName(), pkg, vl, vc)
			}
		}

		for i, svc := range fd.GetService() {
			svcFQN := svc.GetName()
			if pkg != "" {
				svcFQN = pkg + "." + svcFQN
			}
			line, col := findLocationByPath([]int32{6, int32(i), 1}, sci)
			check(svcFQN, svc.GetName(), pkg, line, col)
			for j, method := range svc.GetMethod() {
				mFQN := svcFQN + "." + method.GetName()
				ml, mc := findLocationByPath([]int32{6, int32(i), 2, int32(j), 1}, sci)
				check(mFQN, method.GetName(), svcFQN, ml, mc)
			}
		}
	}
	return errs
}

func collectDupNamesInMsg(msg *descriptorpb.DescriptorProto, msgFQN string, msgPath []int32, sci *descriptorpb.SourceCodeInfo, check func(fqn, shortName, scope string, line, col int)) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for i, field := range msg.GetField() {
		fqn := msgFQN + "." + field.GetName()
		p := append(append([]int32{}, msgPath...), 2, int32(i), 1)
		l, c := findLocationByPath(p, sci)
		check(fqn, field.GetName(), msgFQN, l, c)
	}
	for i, nested := range msg.GetNestedType() {
		nFQN := msgFQN + "." + nested.GetName()
		np := append(append([]int32{}, msgPath...), 3, int32(i))
		l, c := findLocationByPath(append(append([]int32{}, np...), 1), sci)
		check(nFQN, nested.GetName(), msgFQN, l, c)
		collectDupNamesInMsg(nested, nFQN, np, sci, check)
	}
	for i, enum := range msg.GetEnumType() {
		eFQN := msgFQN + "." + enum.GetName()
		l, c := findLocationByPath(append(append([]int32{}, msgPath...), 4, int32(i), 1), sci)
		check(eFQN, enum.GetName(), msgFQN, l, c)
		for j, val := range enum.GetValue() {
			vFQN := msgFQN + "." + val.GetName()
			vl, vc := findLocationByPath(append(append([]int32{}, msgPath...), 4, int32(i), 2, int32(j), 1), sci)
			check(vFQN, val.GetName(), msgFQN, vl, vc)
		}
	}
	for i, oneof := range msg.GetOneofDecl() {
		oFQN := msgFQN + "." + oneof.GetName()
		l, c := findLocationByPath(append(append([]int32{}, msgPath...), 8, int32(i), 1), sci)
		check(oFQN, oneof.GetName(), msgFQN, l, c)
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

