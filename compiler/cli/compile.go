// Package cli provides the core protocol buffer compilation pipeline.
//
// This file exports a Compile function that can be used as a library API,
// separate from the CLI entry point in cli.go.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/wham/protoc-go/compiler/importer"
	"github.com/wham/protoc-go/compiler/parser"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
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
	// (e.g., unused imports).
	Warnings []string
}

// AsFileDescriptorSet returns the result as a FileDescriptorSet proto,
// ready for serialization.
func (r *CompileResult) AsFileDescriptorSet() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{
		File: r.Files,
	}
}

// Compile runs the protocol buffer compilation pipeline on the given request.
// It parses .proto files, resolves types and custom options, validates the
// schema, and returns compiled FileDescriptorProtos.
//
// This is the library equivalent of running the protoc CLI with
// --descriptor_set_out. It does not invoke any code generation plugins.
func Compile(req *CompileRequest) (*CompileResult, error) {
	if len(req.ProtoFiles) == 0 {
		return nil, fmt.Errorf("no proto files specified")
	}

	protoPaths := req.ProtoPaths
	if len(protoPaths) == 0 && len(req.ProtoPathMappings) == 0 {
		protoPaths = []string{"."}
	}

	srcTree := &importer.SourceTree{Roots: protoPaths, Mappings: req.ProtoPathMappings}

	var warnings []string
	for _, w := range srcTree.ValidateRoots() {
		warnings = append(warnings, w)
	}

	// Make proto files relative to source tree
	relFiles := make([]string, len(req.ProtoFiles))
	for i, f := range req.ProtoFiles {
		rel, err := srcTree.MakeRelative(f)
		if err != nil {
			return nil, err
		}
		relFiles[i] = rel
	}

	// Parse all proto files
	parsed := make(map[string]*descriptorpb.FileDescriptorProto)
	explicitJsonNames := make(map[*descriptorpb.FieldDescriptorProto]bool)
	parseResults := make(map[string]*parser.ParseResult)
	var orderedFiles []string
	var collectErrors []string

	// Load pre-compiled descriptor sets
	for _, dsFile := range req.DescriptorSetIn {
		data, err := readFileBytes(dsFile)
		if err != nil {
			return nil, fmt.Errorf("%s: %s", dsFile, err.Error())
		}
		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(data, &fds); err != nil {
			return nil, fmt.Errorf("%s: Unable to parse.", dsFile)
		}
		for _, fd := range fds.GetFile() {
			if _, exists := parsed[fd.GetName()]; exists {
				continue
			}
			parsed[fd.GetName()] = fd
			orderedFiles = append(orderedFiles, fd.GetName())
		}
	}

	for _, f := range relFiles {
		ok, err := parseRecursive(f, srcTree, parsed, explicitJsonNames, parseResults, &orderedFiles, nil, &collectErrors)
		if err != nil {
			return nil, err
		}
		_ = ok
	}

	if len(collectErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(collectErrors, "\n"))
	}

	// Enforce direct dependencies
	if req.DirectDependencies != nil {
		allowed := make(map[string]bool)
		for _, d := range req.DirectDependencies {
			allowed[d] = true
		}
		violationMsg := req.DirectDependenciesViolationMsg
		if violationMsg == "" {
			violationMsg = "File is imported but not declared in --direct_dependencies: %s"
		}
		var depErrors []string
		for _, f := range relFiles {
			fd := parsed[f]
			if fd == nil {
				continue
			}
			for _, dep := range fd.GetDependency() {
				if !allowed[dep] {
					msg := strings.ReplaceAll(violationMsg, "%s", dep)
					depErrors = append(depErrors, fmt.Sprintf("%s: %s", f, msg))
				}
			}
		}
		if len(depErrors) > 0 {
			return nil, fmt.Errorf("%s", strings.Join(depErrors, "\n"))
		}
	}

	// Resolve type references
	var resolveErrors []string
	for _, name := range orderedFiles {
		resolveErrors = append(resolveErrors, parser.ResolveTypes(parsed[name], parsed)...)
	}
	if len(resolveErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(resolveErrors, "\n"))
	}

	// Resolve custom options
	customOptErrors, subFieldFileOptNums := resolveCustomFileOptions(orderedFiles, parsed, parseResults)
	if len(customOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customOptErrors, "\n"))
	}
	customFieldOptErrors, subFieldFieldOptNums := resolveCustomFieldOptions(orderedFiles, parsed, parseResults)
	if len(customFieldOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customFieldOptErrors, "\n"))
	}
	customMsgOptErrors, subFieldMsgOptNums := resolveCustomMessageOptions(orderedFiles, parsed, parseResults)
	if len(customMsgOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customMsgOptErrors, "\n"))
	}
	customSvcOptErrors, subFieldSvcOptNums := resolveCustomServiceOptions(orderedFiles, parsed, parseResults)
	if len(customSvcOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customSvcOptErrors, "\n"))
	}
	customMethodOptErrors, subFieldMethodOptNums := resolveCustomMethodOptions(orderedFiles, parsed, parseResults)
	if len(customMethodOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customMethodOptErrors, "\n"))
	}
	customEnumOptErrors, subFieldEnumOptNums := resolveCustomEnumOptions(orderedFiles, parsed, parseResults)
	if len(customEnumOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customEnumOptErrors, "\n"))
	}
	customEnumValOptErrors, subFieldEnumValOptNums := resolveCustomEnumValueOptions(orderedFiles, parsed, parseResults)
	if len(customEnumValOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customEnumValOptErrors, "\n"))
	}
	customOneofOptErrors, subFieldOneofOptNums := resolveCustomOneofOptions(orderedFiles, parsed, parseResults)
	if len(customOneofOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customOneofOptErrors, "\n"))
	}
	customExtRangeOptErrors, subFieldExtRangeOptNums := resolveCustomExtRangeOptions(orderedFiles, parsed, parseResults)
	if len(customExtRangeOptErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(customExtRangeOptErrors, "\n"))
	}

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
	warnings = append(warnings, dupExtWarnings...)
	buildErrors = append(buildErrors, validateRequiredExtensions(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateExtensionJsonName(orderedFiles, parsed, explicitJsonNames)...)
	buildErrors = append(buildErrors, validateMessageSetFields(orderedFiles, parsed)...)
	buildErrors = append(buildErrors, validateMessageSetExtensions(orderedFiles, parsed)...)
	if len(buildErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(buildErrors, "\n"))
	}

	// Phase 2: Descriptor validation
	var valErrors []string
	if len(dupNameErrs) == 0 {
		jsonErrs, jsonWarns := validateJsonNameConflicts(orderedFiles, parsed, explicitJsonNames)
		valErrors = append(valErrors, jsonErrs...)
		warnings = append(warnings, jsonWarns...)
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
		return nil, fmt.Errorf("%s", strings.Join(valErrors, "\n"))
	}

	// Unused import warnings
	unusedImportWarnings := detectUnusedImports(relFiles, orderedFiles, parsed, parseResults)
	warnings = append(warnings, unusedImportWarnings...)

	// Build output descriptors
	srcRetentionFields := collectSourceRetentionFields(orderedFiles, parsed)
	relFileSet := make(map[string]bool)
	for _, name := range relFiles {
		relFileSet[name] = true
	}

	var resultFiles []*descriptorpb.FileDescriptorProto

	if req.IncludeImports {
		for _, name := range orderedFiles {
			fd := buildOutputDescriptor(name, parsed, relFileSet, parseResults, srcRetentionFields,
				req.RetainOptions, req.IncludeSourceInfo,
				subFieldFileOptNums, subFieldFieldOptNums, subFieldMsgOptNums,
				subFieldEnumOptNums, subFieldEnumValOptNums, subFieldSvcOptNums,
				subFieldMethodOptNums, subFieldOneofOptNums, subFieldExtRangeOptNums)
			resultFiles = append(resultFiles, fd)
		}
	} else {
		for _, name := range orderedFiles {
			if relFileSet[name] {
				fd := buildOutputDescriptor(name, parsed, relFileSet, parseResults, srcRetentionFields,
					req.RetainOptions, req.IncludeSourceInfo,
					subFieldFileOptNums, subFieldFieldOptNums, subFieldMsgOptNums,
					subFieldEnumOptNums, subFieldEnumValOptNums, subFieldSvcOptNums,
					subFieldMethodOptNums, subFieldOneofOptNums, subFieldExtRangeOptNums)
				resultFiles = append(resultFiles, fd)
			}
		}
	}

	return &CompileResult{
		Files:    resultFiles,
		Warnings: warnings,
	}, nil
}

// buildOutputDescriptor prepares a single FileDescriptorProto for output,
// applying source-retention stripping, custom option merging, and unknown
// field sorting as needed.
func buildOutputDescriptor(
	name string,
	parsed map[string]*descriptorpb.FileDescriptorProto,
	relFileSet map[string]bool,
	parseResults map[string]*parser.ParseResult,
	srcRetFields sourceRetentionFields,
	retainOptions bool,
	includeSourceInfo bool,
	subFieldFileOptNums map[string]map[int32]bool,
	subFieldFieldOptNums map[string]map[int32]bool,
	subFieldMsgOptNums map[string]map[int32]bool,
	subFieldEnumOptNums map[string]map[int32]bool,
	subFieldEnumValOptNums map[string]map[int32]bool,
	subFieldSvcOptNums map[string]map[int32]bool,
	subFieldMethodOptNums map[string]map[int32]bool,
	subFieldOneofOptNums map[string]map[int32]bool,
	subFieldExtRangeOptNums map[string]map[int32]bool,
) *descriptorpb.FileDescriptorProto {
	fd := parsed[name]

	if !retainOptions && relFileSet[name] {
		fd = stripSourceRetention(fd, srcRetFields)
	} else if !retainOptions && !relFileSet[name] {
		fd = stripSourceRetention(parsed[name], srcRetFields)
	}

	if pr := parseResults[name]; pr != nil && hasSubFieldCustomOpts(pr) {
		fd = cloneWithMergedExtUnknowns(fd, subFieldFileOptNums[name], subFieldFieldOptNums[name],
			subFieldMsgOptNums[name], subFieldEnumOptNums[name], subFieldEnumValOptNums[name],
			subFieldSvcOptNums[name], subFieldMethodOptNums[name], subFieldOneofOptNums[name],
			subFieldExtRangeOptNums[name])
	}

	if hasOptionsUnknowns(fd) {
		if fd == parsed[name] {
			fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
		}
		sortFDOptionsUnknownFields(fd)
	}

	fdCopy := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	if !includeSourceInfo {
		fdCopy.SourceCodeInfo = nil
	}

	return fdCopy
}

// readFileBytes reads a file and returns its contents as bytes.
func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}
