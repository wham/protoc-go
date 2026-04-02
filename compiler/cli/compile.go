// This file exports the compilation pipeline as a library API, separate from
// the CLI entry point in cli.go.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/wham/protoc-go/compiler/importer"
	"github.com/wham/protoc-go/compiler/parser"
	"github.com/wham/protoc-go/compiler/plugin"
	"github.com/wham/protoc-go/compiler/wellknown"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

// CompileRequest specifies what to compile and how.
type CompileRequest struct {
	// ProtoFiles is the list of .proto files to compile (relative to proto paths).
	ProtoFiles []string

	// ProtoPaths is the list of directories to search for imports.
	// If empty, defaults to ["."].
	ProtoPaths []string

	// ProtoPathMappings provides virtual-path to disk-path mappings for import resolution.
	ProtoPathMappings []importer.Mapping

	// IncludeImports includes all transitive dependencies in the result,
	// not just the directly requested files.
	IncludeImports bool

	// IncludeSourceInfo preserves SourceCodeInfo in the output descriptors.
	// When false (default), source info is stripped.
	IncludeSourceInfo bool

	// RetainOptions keeps all options in the output descriptors, including
	// those only useful during compilation (source-retention options).
	RetainOptions bool

	// DescriptorSetIn is a list of files containing pre-compiled FileDescriptorSets
	// to use for resolving imports.
	DescriptorSetIn []string

	// DirectDependencies, if non-nil, restricts which imports are allowed.
	// Only the listed files may appear in import statements.
	DirectDependencies []string

	// DirectDependenciesViolationMsg is a custom error message for direct dependency
	// violations. Use %s as a placeholder for the import path.
	DirectDependenciesViolationMsg string
}

// CompileResult contains the output of a successful compilation.
type CompileResult struct {
	// Files contains the compiled FileDescriptorProtos for the requested files,
	// in dependency order. If IncludeImports was set, transitive dependencies
	// are included as well.
	Files []*descriptorpb.FileDescriptorProto

	// Warnings contains non-fatal warnings produced during compilation
	// (e.g., unused imports, duplicate extension numbers).
	Warnings []string

	// co holds internal compilation state for RunPlugin.
	co *compileOutput
}

// AsFileDescriptorSet returns the result as a FileDescriptorSet proto,
// ready for serialization.
func (r *CompileResult) AsFileDescriptorSet() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{
		File: r.Files,
	}
}

// GeneratedFile represents a single file produced by a protoc plugin.
type GeneratedFile struct {
	// Name is the output file path (relative to the output directory).
	Name string

	// Content is the file content.
	Content string

	// InsertionPoint, if non-empty, indicates this content should be merged
	// into another generated file at the named insertion point.
	InsertionPoint string
}

// RunPlugin executes a protoc code generation plugin against the compiled
// descriptors and returns the generated files in memory.
//
// pluginPath is the path to the plugin executable (e.g., "protoc-gen-go"
// or "/usr/local/bin/protoc-gen-go").
// parameter is passed to the plugin as the code generation parameter
// (e.g., "paths=source_relative").
func (r *CompileResult) RunPlugin(pluginPath string, parameter string) ([]GeneratedFile, error) {
	co := r.co
	protoFiles := co.buildProtoFiles()
	sourceFileDescriptors := co.buildSourceFileDescriptors()

	req := plugin.BuildCodeGeneratorRequest(co.relFiles, parameter, protoFiles, sourceFileDescriptors)
	resp, err := plugin.RunPlugin(pluginPath, req)
	if err != nil {
		return nil, err
	}

	if resp.GetError() != "" {
		return nil, fmt.Errorf("plugin error: %s", resp.GetError())
	}

	var files []GeneratedFile
	for _, f := range resp.GetFile() {
		files = append(files, GeneratedFile{
			Name:           f.GetName(),
			Content:        f.GetContent(),
			InsertionPoint: f.GetInsertionPoint(),
		})
	}
	return files, nil
}

// LibraryPlugin is a protoc code generation plugin that runs in-process.
//
// This is the in-process equivalent of a protoc-gen-* binary. The plugin
// receives the same CodeGeneratorRequest it would receive over stdin and
// returns the same CodeGeneratorResponse it would write to stdout.
type LibraryPlugin interface {
	Generate(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error)
}

// RunLibraryPlugin executes an in-process [LibraryPlugin] against the compiled
// descriptors. It builds the same CodeGeneratorRequest that [RunPlugin] would
// send to a subprocess, but calls the plugin directly in the current process.
//
// parameter is passed to the plugin as the code generation parameter
// (e.g., "paths=source_relative").
func (r *CompileResult) RunLibraryPlugin(p LibraryPlugin, parameter string) ([]GeneratedFile, error) {
	co := r.co
	protoFiles := co.buildProtoFiles()
	sourceFileDescriptors := co.buildSourceFileDescriptors()

	req := plugin.BuildCodeGeneratorRequest(co.relFiles, parameter, protoFiles, sourceFileDescriptors)

	resp, err := p.Generate(req)
	if err != nil {
		return nil, err
	}

	if resp.GetError() != "" {
		return nil, fmt.Errorf("plugin error: %s", resp.GetError())
	}

	var files []GeneratedFile
	for _, f := range resp.GetFile() {
		files = append(files, GeneratedFile{
			Name:           f.GetName(),
			Content:        f.GetContent(),
			InsertionPoint: f.GetInsertionPoint(),
		})
	}
	return files, nil
}

// CompileErrors is returned when the compilation pipeline encounters
// validation or parse errors. It holds individual error strings that can
// be reformatted (e.g., for MSVS error format in the CLI).
type CompileErrors struct {
	Errors []string
}

func (e *CompileErrors) Error() string {
	return strings.Join(e.Errors, "\n")
}

// ---------------------------------------------------------------------------
// Internal types
// ---------------------------------------------------------------------------

// subFieldOptNums groups the per-file sub-field option numbers produced by
// the custom option resolution passes.
type subFieldOptNums struct {
	file     map[string]map[int32]bool
	field    map[string]map[int32]bool
	msg      map[string]map[int32]bool
	enum     map[string]map[int32]bool
	enumVal  map[string]map[int32]bool
	svc      map[string]map[int32]bool
	method   map[string]map[int32]bool
	oneof    map[string]map[int32]bool
	extRange map[string]map[int32]bool
}

// compileOutput holds all intermediate state produced by the compilation
// pipeline. It is always returned (even on error) so that callers can
// access warnings collected before the error occurred.
type compileOutput struct {
	relFiles          []string
	orderedFiles      []string
	parsed            map[string]*descriptorpb.FileDescriptorProto
	explicitJsonNames map[*descriptorpb.FieldDescriptorProto]bool
	parseResults      map[string]*parser.ParseResult
	warnings          []string
	srcRetFields      sourceRetentionFields
	subOpts           subFieldOptNums
}

// compileInput holds the parameters for compileInternal, after the caller
// has set up the source tree and relativized the proto file paths.
type compileInput struct {
	relFiles               []string
	srcTree                *importer.SourceTree
	descriptorSetIn        []string
	directDeps             map[string]bool
	directDepsSet          bool
	directDepsViolationMsg string
}

// ---------------------------------------------------------------------------
// Core compilation pipeline
// ---------------------------------------------------------------------------

// compileInternal runs the parse → resolve → validate pipeline.
// It always returns a non-nil *compileOutput (which may contain warnings
// collected before the error point). On success error is nil.
func compileInternal(in *compileInput) (*compileOutput, error) {
	co := &compileOutput{
		relFiles: in.relFiles,
	}

	parsed := make(map[string]*descriptorpb.FileDescriptorProto)
	explicitJsonNames := make(map[*descriptorpb.FieldDescriptorProto]bool)
	parseResults := make(map[string]*parser.ParseResult)
	var orderedFiles []string
	var collectErrors []string

	// Load pre-compiled descriptor sets
	for _, dsFile := range in.descriptorSetIn {
		data, err := os.ReadFile(dsFile)
		if err != nil {
			return co, fmt.Errorf("%s: %s", dsFile, err.Error())
		}
		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(data, &fds); err != nil {
			return co, fmt.Errorf("%s: Unable to parse.", dsFile)
		}
		for _, fd := range fds.GetFile() {
			if _, exists := parsed[fd.GetName()]; exists {
				continue
			}
			parsed[fd.GetName()] = fd
			orderedFiles = append(orderedFiles, fd.GetName())
		}
	}

	// Parse all proto files recursively
	for _, f := range in.relFiles {
		ok, err := parseRecursive(f, in.srcTree, parsed, explicitJsonNames, parseResults, &orderedFiles, nil, &collectErrors)
		if err != nil {
			return co, err
		}
		_ = ok
	}
	if len(collectErrors) > 0 {
		return co, &CompileErrors{Errors: collectErrors}
	}

	// Enforce direct dependencies
	if in.directDepsSet {
		violationMsg := in.directDepsViolationMsg
		if violationMsg == "" {
			violationMsg = "File is imported but not declared in --direct_dependencies: %s"
		}
		var depErrors []string
		for _, f := range in.relFiles {
			fd := parsed[f]
			if fd == nil {
				continue
			}
			for _, dep := range fd.GetDependency() {
				if !in.directDeps[dep] {
					msg := strings.ReplaceAll(violationMsg, "%s", dep)
					depErrors = append(depErrors, fmt.Sprintf("%s: %s", f, msg))
				}
			}
		}
		if len(depErrors) > 0 {
			return co, &CompileErrors{Errors: depErrors}
		}
	}

	// Resolve type references across all files
	var resolveErrors []string
	for _, name := range orderedFiles {
		resolveErrors = append(resolveErrors, parser.ResolveTypes(parsed[name], parsed)...)
	}
	if len(resolveErrors) > 0 {
		return co, &CompileErrors{Errors: resolveErrors}
	}

	// Resolve custom options (9 kinds)
	var subOpts subFieldOptNums

	errs, nums := resolveCustomFileOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.file = nums

	errs, nums = resolveCustomFieldOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.field = nums

	errs, nums = resolveCustomMessageOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.msg = nums

	errs, nums = resolveCustomServiceOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.svc = nums

	errs, nums = resolveCustomMethodOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.method = nums

	errs, nums = resolveCustomEnumOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.enum = nums

	errs, nums = resolveCustomEnumValueOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.enumVal = nums

	errs, nums = resolveCustomOneofOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.oneof = nums

	errs, nums = resolveCustomExtRangeOptions(orderedFiles, parsed, parseResults)
	if len(errs) > 0 {
		return co, &CompileErrors{Errors: errs}
	}
	subOpts.extRange = nums

	// Phase 1: Build/cross-link validation
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
	buildErrors = append(buildErrors, validateExtRangeMax(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateReservedFieldNumbers(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateReservedNumberConflicts(orderedFiles, parsed, fieldHints)...)
	buildErrors = append(buildErrors, validateDuplicateReservedNames(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateReservedNameConflicts(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateDuplicateFieldNumbers(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateEmptyOneofs(orderedFiles, parsed)...)
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
	dupExtErrors, dupExtWarnings := validateDuplicateExtensionNumbers(orderedFiles, parsed)
	buildErrors = append(buildErrors, dupExtErrors...)
	co.warnings = append(co.warnings, dupExtWarnings...)
	buildErrors = append(buildErrors, validateRequiredExtensions(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateExtensionJsonName(orderedFiles, parsed, explicitJsonNames)...)
	buildErrors = append(buildErrors, validateMessageSetFields(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateMessageSetExtensions(orderedFiles, parsed)...)
	if len(buildErrors) > 0 {
		return co, &CompileErrors{Errors: buildErrors}
	}

	// Phase 2: Descriptor validation
	var valErrors []string
	if len(dupNameErrs) == 0 {
		jsonErrs, jsonWarns := validateJsonNameConflicts(orderedFiles, parsed, explicitJsonNames)
		valErrors = append(valErrors, jsonErrs...)
		co.warnings = append(co.warnings, jsonWarns...)
	}
	valErrors = append(valErrors, validatePackedNonRepeated(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateLazyNonMessage(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateJstypeNonInt64(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateRepeatedDefault(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateMessageDefault(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateEnumDefaultValues(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateProto3(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateEditionGroups(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateEnumPrefixConflict(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateEditionOpenEnumZero(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateFileLevelLegacyRequired(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateRepeatedFieldEncoding(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateFieldPresenceRepeated(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateFieldPresenceOneof(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateMessageEncodingScalar(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateFieldPresenceExtension(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateRequiredExtensionEditions(orderedFiles, parsed)...)
	valErrors = append(valErrors, validateUtf8ValidationNonString(orderedFiles, parsed)...)
	featEditionErrs := validateFeaturesEditions(orderedFiles, parsed)
	valErrors = append(valErrors, featEditionErrs...)
	if len(featEditionErrs) == 0 {
		valErrors = append(valErrors, validateFeatureTargets(orderedFiles, parsed)...)
	}
	if len(valErrors) > 0 {
		return co, &CompileErrors{Errors: valErrors}
	}

	// Unused import warnings
	co.warnings = append(co.warnings, detectUnusedImports(in.relFiles, orderedFiles, parsed, parseResults)...)

	// Collect source retention fields for output processing
	co.srcRetFields = collectSourceRetentionFields(orderedFiles, parsed)

	// Store all intermediate state
	co.orderedFiles = orderedFiles
	co.parsed = parsed
	co.explicitJsonNames = explicitJsonNames
	co.parseResults = parseResults
	co.subOpts = subOpts

	return co, nil
}

// ---------------------------------------------------------------------------
// Output building helpers
// ---------------------------------------------------------------------------

// buildProtoFiles builds the ordered list of FileDescriptorProtos suitable for
// plugin CodeGeneratorRequests (proto_file field). Source files have
// source-retention options stripped; dependency files do not. All files have
// custom options merged and unknown fields sorted.
func (co *compileOutput) buildProtoFiles() []*descriptorpb.FileDescriptorProto {
	relFileSet := make(map[string]bool)
	for _, name := range co.relFiles {
		relFileSet[name] = true
	}

	var protoFiles []*descriptorpb.FileDescriptorProto
	for _, name := range co.orderedFiles {
		fd := co.parsed[name]
		if relFileSet[name] {
			fd = stripSourceRetention(fd, co.srcRetFields)
		}
		if pr := co.parseResults[name]; pr != nil && hasSubFieldCustomOpts(pr) {
			fd = cloneWithMergedExtUnknowns(fd,
				co.subOpts.file[name], co.subOpts.field[name], co.subOpts.msg[name],
				co.subOpts.enum[name], co.subOpts.enumVal[name], co.subOpts.svc[name],
				co.subOpts.method[name], co.subOpts.oneof[name], co.subOpts.extRange[name])
		}
		if hasOptionsUnknowns(fd) {
			if fd == co.parsed[name] {
				fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
			}
			sortFDOptionsUnknownFields(fd)
		}
		protoFiles = append(protoFiles, fd)
	}
	return protoFiles
}

// buildSourceFileDescriptors returns the original (unstripped) descriptors
// for the files being compiled, in dependency order. These are used for the
// source_file_descriptors field in CodeGeneratorRequests.
func (co *compileOutput) buildSourceFileDescriptors() []*descriptorpb.FileDescriptorProto {
	relFileSet := make(map[string]bool)
	for _, name := range co.relFiles {
		relFileSet[name] = true
	}
	var result []*descriptorpb.FileDescriptorProto
	for _, name := range co.orderedFiles {
		if relFileSet[name] {
			result = append(result, co.parsed[name])
		}
	}
	return result
}

// buildDescriptorSetFiles builds descriptor set output files matching the
// behavior of protoc's --descriptor_set_out flag.
//
// When includeImports is true, all transitive dependencies are included and
// (unless retainOptions) have source-retention options stripped. When false,
// only the requested source files are included.
func (co *compileOutput) buildDescriptorSetFiles(includeImports, retainOptions, includeSourceInfo bool) []*descriptorpb.FileDescriptorProto {
	relFileSet := make(map[string]bool)
	for _, name := range co.relFiles {
		relFileSet[name] = true
	}

	// Build the plugin-ready proto files (source files stripped, deps not stripped)
	protoFiles := co.buildProtoFiles()
	protoFileMap := make(map[string]*descriptorpb.FileDescriptorProto)
	for i, name := range co.orderedFiles {
		protoFileMap[name] = protoFiles[i]
	}

	var result []*descriptorpb.FileDescriptorProto

	for _, name := range co.orderedFiles {
		if !includeImports && !relFileSet[name] {
			continue
		}

		var fd *descriptorpb.FileDescriptorProto
		if retainOptions {
			fd = co.parsed[name]
		} else {
			fd = protoFileMap[name]
			// For includeImports, dependency files also need source-retention stripped
			if includeImports && !relFileSet[name] {
				fd = stripSourceRetention(co.parsed[name], co.srcRetFields)
			}
		}

		fdCopy := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
		if !includeSourceInfo {
			fdCopy.SourceCodeInfo = nil
		}
		result = append(result, fdCopy)
	}

	return result
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// Compile runs the protocol buffer compilation pipeline on the given request.
// It parses .proto files, resolves types and custom options, validates the
// schema, and returns compiled FileDescriptorProtos.
//
// Compile is safe for concurrent use: each call operates on independent
// internal state. The returned [CompileResult] supports running code
// generation plugins via [CompileResult.RunPlugin].
func Compile(req *CompileRequest) (*CompileResult, error) {
	if len(req.ProtoFiles) == 0 {
		return nil, fmt.Errorf("no proto files specified")
	}

	protoPaths := req.ProtoPaths
	if len(protoPaths) == 0 && len(req.ProtoPathMappings) == 0 {
		protoPaths = []string{"."}
	}

	srcTree := &importer.SourceTree{Roots: protoPaths, Mappings: req.ProtoPathMappings, FallbackFS: wellknown.ProtoFiles}

	var rootWarnings []string
	rootWarnings = append(rootWarnings, srcTree.ValidateRoots()...)

	// Make proto files relative to source tree
	relFiles := make([]string, len(req.ProtoFiles))
	for i, f := range req.ProtoFiles {
		rel, err := srcTree.MakeRelative(f)
		if err != nil {
			return nil, err
		}
		relFiles[i] = rel
	}

	in := &compileInput{
		relFiles:        relFiles,
		srcTree:         srcTree,
		descriptorSetIn: req.DescriptorSetIn,
	}
	if req.DirectDependencies != nil {
		in.directDepsSet = true
		in.directDeps = make(map[string]bool)
		for _, d := range req.DirectDependencies {
			in.directDeps[d] = true
		}
		in.directDepsViolationMsg = req.DirectDependenciesViolationMsg
	}

	co, err := compileInternal(in)
	if err != nil {
		return nil, err
	}

	files := co.buildDescriptorSetFiles(req.IncludeImports, req.RetainOptions, req.IncludeSourceInfo)

	return &CompileResult{
		Files:    files,
		Warnings: append(rootWarnings, co.warnings...),
		co:       co,
	}, nil
}
