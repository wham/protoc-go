package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/wham/protoc-go/compiler/importer"
	"github.com/wham/protoc-go/compiler/parser"
	"github.com/wham/protoc-go/compiler/plugin"
	"github.com/wham/protoc-go/io/tokenizer"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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
  --direct_dependencies       A colon delimited list of imports that are
                              allowed to be used in "import"
                              declarations, when explictily provided.
  --option_dependencies       A colon delimited list of imports that are
                              allowed to be used in "import option"
                              declarations, when explicitly provided.
  --notices                   Show notice file and exit.
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
var noticesText = `Protoc uses the following open source libraries:

Abseil
Apache 2.0
                                 Apache License
                           Version 2.0, January 2004
                        https://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.
=============================================================================
utf8_range
MIT License

Copyright (c) 2019 Yibo Cai
Copyright 2022 Google LLC

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
=============================================================================
zlib

Copyright notice:

 (C) 1995-2024 Jean-loup Gailly and Mark Adler

  This software is provided 'as-is', without any express or implied
  warranty.  In no event will the authors be held liable for any damages
  arising from the use of this software.

  Permission is granted to anyone to use this software for any purpose,
  including commercial applications, and to alter it and redistribute it
  freely, subject to the following restrictions:

  1. The origin of this software must not be misrepresented; you must not
     claim that you wrote the original software. If you use this software
     in a product, an acknowledgment in the product documentation would be
     appreciated but is not required.
  2. Altered source versions must be plainly marked as such, and must not be
     misrepresented as being the original software.
  3. This notice may not be removed or altered from any source distribution.

  Jean-loup Gailly        Mark Adler
  jloup@gzip.org          madler@alumni.caltech.edu
=============================================================================
rules_cc

Copyright 2024 The Bazel Authors.
Apache License 2.0
=============================================================================
Bazel

Copyright 2014 The Bazel Authors.
Apache License 2.0`

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
	notices               bool
	errorFormat           string // "gcc" (default) or "msvs"
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

	// --decode_raw reads binary proto from stdin and decodes as raw wire format
	if cfg.decodeRaw {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("Failed to parse input.")
		}
		if len(data) == 0 {
			return nil
		}
		if err := validateRawProto(data); err != nil {
			return fmt.Errorf("Failed to parse input.")
		}
		decodeRawProto(os.Stdout, data, 0)
		return nil
	}

	// --notices prints license text and exits
	if cfg.notices {
		fmt.Println(noticesText)
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
	parseResults := make(map[string]*parser.ParseResult)
	var orderedFiles []string
	var collectErrors []string

	for _, f := range relFiles {
		ok, err := parseRecursive(f, srcTree, parsed, explicitJsonNames, parseResults, &orderedFiles, nil, &collectErrors)
		if err != nil {
			return err
		}
		_ = ok
	}

	if len(collectErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			collectErrors = formatErrorsMSVS(collectErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(collectErrors, "\n"))
	}

	// Resolve type references across all files (must happen after all files parsed)
	var resolveErrors []string
	for _, name := range orderedFiles {
		resolveErrors = append(resolveErrors, parser.ResolveTypes(parsed[name], parsed)...)
	}
	if len(resolveErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			resolveErrors = formatErrorsMSVS(resolveErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(resolveErrors, "\n"))
	}

	// Resolve custom options (parenthesized extension options)
	customOptErrors, subFieldFileOptNums := resolveCustomFileOptions(orderedFiles, parsed, parseResults)
	if len(customOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customOptErrors = formatErrorsMSVS(customOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customOptErrors, "\n"))
	}

	customFieldOptErrors, subFieldFieldOptNums := resolveCustomFieldOptions(orderedFiles, parsed, parseResults)
	if len(customFieldOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customFieldOptErrors = formatErrorsMSVS(customFieldOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customFieldOptErrors, "\n"))
	}

	customMsgOptErrors, subFieldMsgOptNums := resolveCustomMessageOptions(orderedFiles, parsed, parseResults)
	if len(customMsgOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customMsgOptErrors = formatErrorsMSVS(customMsgOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customMsgOptErrors, "\n"))
	}

	customSvcOptErrors, subFieldSvcOptNums := resolveCustomServiceOptions(orderedFiles, parsed, parseResults)
	if len(customSvcOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customSvcOptErrors = formatErrorsMSVS(customSvcOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customSvcOptErrors, "\n"))
	}

	customMethodOptErrors, subFieldMethodOptNums := resolveCustomMethodOptions(orderedFiles, parsed, parseResults)
	if len(customMethodOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customMethodOptErrors = formatErrorsMSVS(customMethodOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customMethodOptErrors, "\n"))
	}

	customEnumOptErrors, subFieldEnumOptNums := resolveCustomEnumOptions(orderedFiles, parsed, parseResults)
	if len(customEnumOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customEnumOptErrors = formatErrorsMSVS(customEnumOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customEnumOptErrors, "\n"))
	}

	customEnumValOptErrors, subFieldEnumValOptNums := resolveCustomEnumValueOptions(orderedFiles, parsed, parseResults)
	if len(customEnumValOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customEnumValOptErrors = formatErrorsMSVS(customEnumValOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customEnumValOptErrors, "\n"))
	}

	customOneofOptErrors, subFieldOneofOptNums := resolveCustomOneofOptions(orderedFiles, parsed, parseResults)
	if len(customOneofOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customOneofOptErrors = formatErrorsMSVS(customOneofOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customOneofOptErrors, "\n"))
	}

	customExtRangeOptErrors, subFieldExtRangeOptNums := resolveCustomExtRangeOptions(orderedFiles, parsed, parseResults)
	if len(customExtRangeOptErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			customExtRangeOptErrors = formatErrorsMSVS(customExtRangeOptErrors, srcTree)
		}
		return fmt.Errorf("%s", strings.Join(customExtRangeOptErrors, "\n"))
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
	buildErrors = append(buildErrors, validateMessageSetExtensions(orderedFiles, parsed)...)
	if len(buildErrors) > 0 {
		if cfg.errorFormat == "msvs" {
			buildErrors = formatErrorsMSVS(buildErrors, srcTree)
		}
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
	valErrors = append(valErrors, validateEditionGroups(orderedFiles, parsed)...)
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
		if cfg.errorFormat == "msvs" {
			valErrors = formatErrorsMSVS(valErrors, srcTree)
		}
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

	// Build ordered list of FileDescriptorProtos (strip source-retention options only for source files)
	relFileSet := make(map[string]bool)
	for _, name := range relFiles {
		relFileSet[name] = true
	}
	var protoFiles []*descriptorpb.FileDescriptorProto
	strippedMap := make(map[string]*descriptorpb.FileDescriptorProto)
	for _, name := range orderedFiles {
		fd := parsed[name]
		if relFileSet[name] {
			fd = stripSourceRetention(fd)
		}
		// If the file has sub-field custom options, clone and merge unknown fields
		// so proto_file has merged entries (matching C++ protoc's linked descriptors)
		if pr := parseResults[name]; pr != nil && hasSubFieldCustomOpts(pr) {
			fd = cloneWithMergedExtUnknowns(fd, subFieldFileOptNums[name], subFieldFieldOptNums[name], subFieldMsgOptNums[name], subFieldEnumOptNums[name], subFieldEnumValOptNums[name], subFieldSvcOptNums[name], subFieldMethodOptNums[name], subFieldOneofOptNums[name], subFieldExtRangeOptNums[name])
		}
		// Sort unknown fields by field number on Options (C++ proto_file order).
		// Clone if fd still shares pointer with parsed to avoid modifying source_file_descriptors.
		if hasOptionsUnknowns(fd) {
			if fd == parsed[name] {
				fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
			}
			sortFDOptionsUnknownFields(fd)
		}
		protoFiles = append(protoFiles, fd)
		strippedMap[name] = fd
	}

	// Handle descriptor set output
	if cfg.descriptorSetOut != "" {
		fds := &descriptorpb.FileDescriptorSet{}
		if cfg.includeImports {
			for _, name := range orderedFiles {
				fd := strippedMap[name]
				if !relFileSet[name] {
					fd = stripSourceRetention(parsed[name])
				}
				fdCopy := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
				if !cfg.includeSourceInfo {
					fdCopy.SourceCodeInfo = nil
				}
				fds.File = append(fds.File, fdCopy)
			}
		} else {
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
		if err := writeDescriptorSet(cfg.descriptorSetOut, data); err != nil {
			return err
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
			var startErr *plugin.PluginStartError
			if errors.As(err, &startErr) {
				return fmt.Errorf("--%s_out: protoc-gen-%s: Plugin failed with status code 1.", plug.name, plug.name)
			}
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

// writeDescriptorSet writes the descriptor set file, matching C++ protoc error format.
func writeDescriptorSet(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		if pe, ok := err.(*os.PathError); ok {
			errMsg := pe.Err.Error()
			if len(errMsg) > 0 {
				errMsg = strings.ToUpper(errMsg[:1]) + errMsg[1:]
			}
			return fmt.Errorf("%s: %s", path, errMsg)
		}
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func parseRecursive(filename string, srcTree *importer.SourceTree, parsed map[string]*descriptorpb.FileDescriptorProto, explicitJsonNames map[*descriptorpb.FieldDescriptorProto]bool, parseResults map[string]*parser.ParseResult, orderedFiles *[]string, importStack []string, collectErrors *[]string) (bool, error) {
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
	parseResults[filename] = result

	// Parse dependencies
	newStack := append(importStack, filename)
	failedDeps := map[string]bool{}
	for _, dep := range fd.GetDependency() {
		ok, err := parseRecursive(dep, srcTree, parsed, explicitJsonNames, parseResults, orderedFiles, newStack, collectErrors)
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
			cfg.errorFormat = strings.TrimPrefix(arg, "--error_format=")
			if cfg.errorFormat != "gcc" && cfg.errorFormat != "msvs" {
				return nil, fmt.Errorf("Unknown error format: %s", cfg.errorFormat)
			}
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

		if strings.HasPrefix(arg, "--direct_dependencies=") {
			continue
		}

		if strings.HasPrefix(arg, "--option_dependencies=") {
			continue
		}

		if arg == "--notices" {
			cfg.notices = true
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
			if arg == "--decode" {
				return nil, fmt.Errorf("Missing value for flag: %s\nTo decode an unknown message, use --decode_raw.", arg)
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
					if line == 0 && col == 0 {
						path2 := []int32{7, int32(i), 6}
						line, col = findLocationByPath(path2, sci)
					}
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
				line, col := findFieldTypeLocation(msgPath, i, sci)
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
				if line == 0 && col == 0 {
					extPath2 := append(append([]int32{}, msgPath...), 6, int32(i), 6)
					line, col = findLocationByPath(extPath2, sci)
				}
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
			nextAvail := suggestFieldNumbers(msg, 1)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Field number %d has already been used in \"%s\" by field \"%s\". Next available field number is %s.",
				filename, line, col, num, fqn, firstName, nextAvail))
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
			valFqn := val.GetName()
			if parentScope != "" {
				valFqn = parentScope + "." + valFqn
			}
			firstFqn := firstName
			if parentScope != "" {
				firstFqn = parentScope + "." + firstFqn
			}
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
	if !msg.GetOptions().GetDeprecatedLegacyJsonFieldConflicts() {
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
		sci := fd.GetSourceCodeInfo()
		// File-level features
		if fd.GetOptions() != nil && fd.GetOptions().GetFeatures() != nil {
			line, col := findLocationByPath([]int32{12}, sci)
			errs = append(errs, fmt.Sprintf("%s:%d:%d: Features are only valid under editions.", name, line, col))
		}
		collectFeaturesEditionsErrors(name, fd, sci, &errs)
	}
	return errs
}

func featEdErr(name string, path []int32, sci *descriptorpb.SourceCodeInfo) string {
	line, col := findLocationByPath(path, sci)
	if line == 0 && col == 0 {
		return fmt.Sprintf("%s: Features are only valid under editions.", name)
	}
	return fmt.Sprintf("%s:%d:%d: Features are only valid under editions.", name, line, col)
}

func collectFeaturesEditionsErrors(name string, fd *descriptorpb.FileDescriptorProto, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, msg := range fd.GetMessageType() {
		collectFeaturesEditionsInMsg(name, []int32{4, int32(i)}, msg, sci, errs)
	}
	for i, e := range fd.GetEnumType() {
		collectFeaturesEditionsInEnum(name, []int32{5, int32(i)}, e, sci, errs)
	}
	for i, svc := range fd.GetService() {
		svcPath := []int32{6, int32(i)}
		if svc.GetOptions() != nil && svc.GetOptions().GetFeatures() != nil {
			*errs = append(*errs, featEdErr(name, append(clonePath(svcPath), 1), sci))
		}
		for j, m := range svc.GetMethod() {
			if m.GetOptions() != nil && m.GetOptions().GetFeatures() != nil {
				*errs = append(*errs, featEdErr(name, append(clonePath(svcPath), 2, int32(j), 1), sci))
			}
		}
	}
	for i, ext := range fd.GetExtension() {
		if ext.GetOptions() != nil && ext.GetOptions().GetFeatures() != nil {
			*errs = append(*errs, featEdErr(name, []int32{7, int32(i), 1}, sci))
		}
	}
}

func collectFeaturesEditionsInMsg(name string, msgPath []int32, msg *descriptorpb.DescriptorProto, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions() != nil && msg.GetOptions().GetFeatures() != nil {
		*errs = append(*errs, featEdErr(name, append(clonePath(msgPath), 1), sci))
	}
	for i, f := range msg.GetField() {
		if f.GetOptions() != nil && f.GetOptions().GetFeatures() != nil {
			*errs = append(*errs, featEdErr(name, append(clonePath(msgPath), 2, int32(i), 1), sci))
		}
	}
	for _, o := range msg.GetOneofDecl() {
		if o.GetOptions() != nil && o.GetOptions().GetFeatures() != nil {
			*errs = append(*errs, fmt.Sprintf("%s: Features are only valid under editions.", name))
		}
	}
	for i, e := range msg.GetEnumType() {
		collectFeaturesEditionsInEnum(name, append(clonePath(msgPath), 4, int32(i)), e, sci, errs)
	}
	for i, ext := range msg.GetExtension() {
		if ext.GetOptions() != nil && ext.GetOptions().GetFeatures() != nil {
			*errs = append(*errs, featEdErr(name, append(clonePath(msgPath), 6, int32(i), 1), sci))
		}
	}
	for i, nested := range msg.GetNestedType() {
		collectFeaturesEditionsInMsg(name, append(clonePath(msgPath), 3, int32(i)), nested, sci, errs)
	}
}

func collectFeaturesEditionsInEnum(name string, enumPath []int32, e *descriptorpb.EnumDescriptorProto, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if e.GetOptions() != nil && e.GetOptions().GetFeatures() != nil {
		*errs = append(*errs, featEdErr(name, append(clonePath(enumPath), 1), sci))
	}
	for i, v := range e.GetValue() {
		if v.GetOptions() != nil && v.GetOptions().GetFeatures() != nil {
			*errs = append(*errs, featEdErr(name, append(clonePath(enumPath), 2, int32(i), 1), sci))
		}
	}
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

func validateEditionGroups(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		for i, msg := range fd.GetMessageType() {
			collectEditionGroupErrors(fd.GetName(), msg, []int32{4, int32(i)}, fd.GetSourceCodeInfo(), &errs)
		}
	}
	return errs
}

func collectEditionGroupErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP {
			line, col := findFieldTypeLocation(msgPath, i, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Group syntax is no longer supported in editions. To get group behavior you can specify features.message_encoding = DELIMITED on a message field.", filename, line, col))
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		collectEditionGroupErrors(filename, nested, append(append([]int32{}, msgPath...), 3, int32(i)), sci, errs)
	}
}

func validateRepeatedFieldEncoding(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectRepeatedFieldEncodingErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		for i, ext := range fd.GetExtension() {
			checkRepeatedFieldEncodingField(fd.GetName(), ext, []int32{7, int32(i), 1}, sci, &errs)
		}
	}
	return errs
}

func collectRepeatedFieldEncodingErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for i, field := range msg.GetField() {
		checkRepeatedFieldEncodingField(filename, field, append(clonePath(msgPath), 2, int32(i), 1), sci, errs)
	}
	for i, ext := range msg.GetExtension() {
		checkRepeatedFieldEncodingField(filename, ext, append(clonePath(msgPath), 6, int32(i), 1), sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		collectRepeatedFieldEncodingErrors(filename, nested, append(clonePath(msgPath), 3, int32(i)), sci, errs)
	}
}

func checkRepeatedFieldEncodingField(filename string, field *descriptorpb.FieldDescriptorProto, namePath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
		return
	}
	if field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil && field.GetOptions().GetFeatures().RepeatedFieldEncoding != nil {
		line, col := findLocationByPath(namePath, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Only repeated fields can specify repeated field encoding.", filename, line, col))
	}
}

func validateFieldPresenceRepeated(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectFieldPresenceRepeatedErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		for i, ext := range fd.GetExtension() {
			checkFieldPresenceRepeatedField(fd.GetName(), ext, []int32{7, int32(i), 1}, sci, &errs)
		}
	}
	return errs
}

func collectFieldPresenceRepeatedErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for i, field := range msg.GetField() {
		checkFieldPresenceRepeatedField(filename, field, append(clonePath(msgPath), 2, int32(i), 1), sci, errs)
	}
	for i, ext := range msg.GetExtension() {
		checkFieldPresenceRepeatedField(filename, ext, append(clonePath(msgPath), 6, int32(i), 1), sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		collectFieldPresenceRepeatedErrors(filename, nested, append(clonePath(msgPath), 3, int32(i)), sci, errs)
	}
}

func checkFieldPresenceRepeatedField(filename string, field *descriptorpb.FieldDescriptorProto, namePath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if field.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
		return
	}
	if field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil && field.GetOptions().GetFeatures().FieldPresence != nil {
		line, col := findLocationByPath(namePath, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Repeated fields can't specify field presence.", filename, line, col))
	}
}

func validateFieldPresenceOneof(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectFieldPresenceOneofErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
	}
	return errs
}

func collectFieldPresenceOneofErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, field := range msg.GetField() {
		if field.OneofIndex != nil && field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil && field.GetOptions().GetFeatures().FieldPresence != nil {
			namePath := append(clonePath(msgPath), 2, int32(i), 1)
			line, col := findLocationByPath(namePath, sci)
			*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Oneof fields can't specify field presence.", filename, line, col))
		}
	}
	for i, nested := range msg.GetNestedType() {
		collectFieldPresenceOneofErrors(filename, nested, append(clonePath(msgPath), 3, int32(i)), sci, errs)
	}
}

func validateMessageEncodingScalar(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectMessageEncodingScalarErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		for i, ext := range fd.GetExtension() {
			checkMessageEncodingScalarField(fd.GetName(), ext, []int32{7, int32(i), 1}, sci, &errs, false)
		}
	}
	return errs
}

func collectMessageEncodingScalarErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	// Build set of map entry nested type names
	mapEntryNames := make(map[string]bool)
	for _, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			mapEntryNames[nested.GetName()] = true
		}
	}
	for i, field := range msg.GetField() {
		isMap := false
		if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			tn := field.GetTypeName()
			if idx := strings.LastIndex(tn, "."); idx >= 0 {
				tn = tn[idx+1:]
			}
			isMap = mapEntryNames[tn]
		}
		checkMessageEncodingScalarField(filename, field, append(clonePath(msgPath), 2, int32(i), 1), sci, errs, isMap)
	}
	for i, ext := range msg.GetExtension() {
		checkMessageEncodingScalarField(filename, ext, append(clonePath(msgPath), 6, int32(i), 1), sci, errs, false)
	}
	for i, nested := range msg.GetNestedType() {
		collectMessageEncodingScalarErrors(filename, nested, append(clonePath(msgPath), 3, int32(i)), sci, errs)
	}
}

func checkMessageEncodingScalarField(filename string, field *descriptorpb.FieldDescriptorProto, namePath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string, isMapField bool) {
	if !isMapField && (field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP) {
		return
	}
	if field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil && field.GetOptions().GetFeatures().MessageEncoding != nil {
		line, col := findLocationByPath(namePath, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Only message fields can specify message encoding.", filename, line, col))
	}
}

func validateRequiredExtensionEditions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectRequiredExtensionEditionsErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		for i, ext := range fd.GetExtension() {
			checkRequiredExtensionEditionsField(fd.GetName(), ext, []int32{7, int32(i), 1}, sci, &errs)
		}
	}
	return errs
}

func collectRequiredExtensionEditionsErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, ext := range msg.GetExtension() {
		checkRequiredExtensionEditionsField(filename, ext, append(clonePath(msgPath), 6, int32(i), 1), sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		collectRequiredExtensionEditionsErrors(filename, nested, append(clonePath(msgPath), 3, int32(i)), sci, errs)
	}
}

func checkRequiredExtensionEditionsField(filename string, field *descriptorpb.FieldDescriptorProto, namePath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil &&
		field.GetOptions().GetFeatures().FieldPresence != nil &&
		field.GetOptions().GetFeatures().GetFieldPresence() == descriptorpb.FeatureSet_LEGACY_REQUIRED {
		line, col := findLocationByPath(namePath, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extensions can't be required.", filename, line, col))
	}
}

func validateFieldPresenceExtension(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectFieldPresenceExtensionErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		for i, ext := range fd.GetExtension() {
			checkFieldPresenceExtensionField(fd.GetName(), ext, []int32{7, int32(i), 1}, sci, &errs)
		}
	}
	return errs
}

func collectFieldPresenceExtensionErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	for i, ext := range msg.GetExtension() {
		checkFieldPresenceExtensionField(filename, ext, append(clonePath(msgPath), 6, int32(i), 1), sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		collectFieldPresenceExtensionErrors(filename, nested, append(clonePath(msgPath), 3, int32(i)), sci, errs)
	}
}

func checkFieldPresenceExtensionField(filename string, field *descriptorpb.FieldDescriptorProto, namePath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil &&
		field.GetOptions().GetFeatures().FieldPresence != nil &&
		field.GetOptions().GetFeatures().GetFieldPresence() != descriptorpb.FeatureSet_LEGACY_REQUIRED {
		line, col := findLocationByPath(namePath, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extensions can't specify field presence.", filename, line, col))
	}
}

func validateUtf8ValidationNonString(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		if fd.GetSyntax() != "editions" {
			continue
		}
		sci := fd.GetSourceCodeInfo()
		for i, msg := range fd.GetMessageType() {
			collectUtf8ValidationNonStringErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, &errs)
		}
		for i, ext := range fd.GetExtension() {
			checkUtf8ValidationNonStringField(fd.GetName(), ext, []int32{7, int32(i), 1}, sci, &errs)
		}
	}
	return errs
}

func collectUtf8ValidationNonStringErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	for i, field := range msg.GetField() {
		checkUtf8ValidationNonStringField(filename, field, append(clonePath(msgPath), 2, int32(i), 1), sci, errs)
	}
	for i, ext := range msg.GetExtension() {
		checkUtf8ValidationNonStringField(filename, ext, append(clonePath(msgPath), 6, int32(i), 1), sci, errs)
	}
	for i, nested := range msg.GetNestedType() {
		collectUtf8ValidationNonStringErrors(filename, nested, append(clonePath(msgPath), 3, int32(i)), sci, errs)
	}
}

func checkUtf8ValidationNonStringField(filename string, field *descriptorpb.FieldDescriptorProto, namePath []int32, sci *descriptorpb.SourceCodeInfo, errs *[]string) {
	if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING {
		return
	}
	if field.GetOptions() != nil && field.GetOptions().GetFeatures() != nil && field.GetOptions().GetFeatures().Utf8Validation != nil {
		line, col := findLocationByPath(namePath, sci)
		*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Only string fields can specify utf8 validation.", filename, line, col))
	}
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
	// Try path [5] (type) first, then [6] (type_name) for message/enum refs
	for _, sub := range []int32{5, 6} {
		target := append(append([]int32{}, msgPath...), 2, int32(fieldIdx), sub)
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
				if scope == "" {
					// No package — C++ protoc omits " in ..." suffix
					if line > 0 && col > 0 {
						errMsg = fmt.Sprintf("%s:%d:%d: \"%s\" is already defined.",
							fd.GetName(), line, col, shortName)
					} else {
						errMsg = fmt.Sprintf("%s: \"%s\" is already defined.",
							fd.GetName(), shortName)
					}
				} else if line > 0 && col > 0 {
					errMsg = fmt.Sprintf("%s:%d:%d: \"%s\" is already defined in \"%s\".",
						fd.GetName(), line, col, shortName, scope)
				} else {
					errMsg = fmt.Sprintf("%s: \"%s\" is already defined in \"%s\".",
						fd.GetName(), shortName, scope)
				}
				errs = append(errs, errMsg)
				// Only emit scoping note when the conflict is cross-enum
				// (C++ protoc: added_to_inner_scope && !added_to_outer_scope)
				if enumName != "" && enumValParent[fqn] != enumName {
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

// validateMessageSetExtensions checks that extensions of messages with
// message_set_wire_format must be optional messages.
func validateMessageSetExtensions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []string {
	// Build set of message FQNs that have message_set_wire_format = true.
	msgSetMsgs := map[string]bool{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for _, msg := range fd.GetMessageType() {
			collectMessageSetMsgs(msg, pkg, msgSetMsgs)
		}
	}

	var errs []string
	for _, name := range orderedFiles {
		fd := parsed[name]
		sci := fd.GetSourceCodeInfo()
		// File-level extensions
		for i, ext := range fd.GetExtension() {
			extendee := strings.TrimPrefix(ext.GetExtendee(), ".")
			if msgSetMsgs[extendee] {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || ext.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL {
					line, col := findLocationByPath([]int32{7, int32(i), 5}, sci)
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Extensions of MessageSets must be optional messages.",
						fd.GetName(), line, col))
				}
			}
		}
		// Message-level extensions
		for i, msg := range fd.GetMessageType() {
			collectMessageSetExtErrors(fd.GetName(), msg, []int32{4, int32(i)}, sci, msgSetMsgs, &errs)
		}
	}
	return errs
}

func collectMessageSetMsgs(msg *descriptorpb.DescriptorProto, prefix string, out map[string]bool) {
	fqn := msg.GetName()
	if prefix != "" {
		fqn = prefix + "." + fqn
	}
	if msg.GetOptions().GetMessageSetWireFormat() {
		out[fqn] = true
	}
	for _, nested := range msg.GetNestedType() {
		collectMessageSetMsgs(nested, fqn, out)
	}
}

func collectMessageSetExtErrors(filename string, msg *descriptorpb.DescriptorProto, msgPath []int32, sci *descriptorpb.SourceCodeInfo, msgSetMsgs map[string]bool, errs *[]string) {
	for i, ext := range msg.GetExtension() {
		extendee := strings.TrimPrefix(ext.GetExtendee(), ".")
		if msgSetMsgs[extendee] {
			if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || ext.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL {
				path := append(append([]int32{}, msgPath...), 6, int32(i), 5)
				line, col := findLocationByPath(path, sci)
				*errs = append(*errs, fmt.Sprintf("%s:%d:%d: Extensions of MessageSets must be optional messages.",
					filename, line, col))
			}
		}
	}
	for i, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nestedPath := append(append([]int32{}, msgPath...), 3, int32(i))
		collectMessageSetExtErrors(filename, nested, nestedPath, sci, msgSetMsgs, errs)
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

func clonePath(p []int32) []int32 {
	c := make([]int32, len(p))
	copy(c, p)
	return c
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

func hasSubFieldCustomOpts(pr *parser.ParseResult) bool {
	for _, opt := range pr.CustomFileOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomFieldOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomMessageOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomEnumOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomServiceOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomMethodOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomEnumValueOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomOneofOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	for _, opt := range pr.CustomExtRangeOptions {
		if len(opt.SubFieldPath) > 0 {
			return true
		}
	}
	return false
}

// cloneWithMergedExtUnknowns clones fd and merges multiple unknown field entries
// with the same tag (length-delimited) in FileOptions, FieldOptions, MessageOptions, EnumOptions, EnumValueOptions, ServiceOptions, MethodOptions, OneofOptions, and ExtensionRangeOptions into single entries.
func cloneWithMergedExtUnknowns(fd *descriptorpb.FileDescriptorProto, mergeableFileFields map[int32]bool, mergeableFieldOptFields map[int32]bool, mergeableMsgOptFields map[int32]bool, mergeableEnumOptFields map[int32]bool, mergeableEnumValOptFields map[int32]bool, mergeableSvcOptFields map[int32]bool, mergeableMethodOptFields map[int32]bool, mergeableOneofOptFields map[int32]bool, mergeableExtRangeOptFields map[int32]bool) *descriptorpb.FileDescriptorProto {
	fdCopy := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	if fdCopy.Options != nil {
		mergeUnknownExtensions(fdCopy.Options.ProtoReflect(), mergeableFileFields)
	}
	// Merge field options for top-level extensions (fd.Extension)
	for _, ext := range fdCopy.GetExtension() {
		if ext.Options != nil {
			mergeUnknownExtensions(ext.Options.ProtoReflect(), mergeableFieldOptFields)
		}
	}
	mergeFieldOptionsInMessages(fdCopy.GetMessageType(), mergeableFieldOptFields)
	mergeMessageOptionsInMessages(fdCopy.GetMessageType(), mergeableMsgOptFields)
	mergeEnumOptions(fdCopy.GetEnumType(), mergeableEnumOptFields)
	mergeEnumOptionsInMessages(fdCopy.GetMessageType(), mergeableEnumOptFields)
	mergeEnumValueOptions(fdCopy.GetEnumType(), mergeableEnumValOptFields)
	mergeEnumValueOptionsInMessages(fdCopy.GetMessageType(), mergeableEnumValOptFields)
	mergeServiceOptions(fdCopy.GetService(), mergeableSvcOptFields)
	mergeMethodOptions(fdCopy.GetService(), mergeableMethodOptFields)
	mergeOneofOptionsInMessages(fdCopy.GetMessageType(), mergeableOneofOptFields)
	mergeExtRangeOptionsInMessages(fdCopy.GetMessageType(), mergeableExtRangeOptFields)
	return fdCopy
}

// mergeFieldOptionsInMessages recursively merges unknown extensions in FieldOptions
// for all fields in the given messages and their nested types.
func mergeFieldOptionsInMessages(msgs []*descriptorpb.DescriptorProto, mergeableFields map[int32]bool) {
	for _, msg := range msgs {
		for _, field := range msg.GetField() {
			if field.Options != nil {
				mergeUnknownExtensions(field.Options.ProtoReflect(), mergeableFields)
			}
		}
		for _, ext := range msg.GetExtension() {
			if ext.Options != nil {
				mergeUnknownExtensions(ext.Options.ProtoReflect(), mergeableFields)
			}
		}
		mergeFieldOptionsInMessages(msg.GetNestedType(), mergeableFields)
	}
}

// mergeMessageOptionsInMessages recursively merges unknown extensions in MessageOptions
// for all messages and their nested types.
func mergeMessageOptionsInMessages(msgs []*descriptorpb.DescriptorProto, mergeableFields map[int32]bool) {
	for _, msg := range msgs {
		if msg.Options != nil {
			mergeUnknownExtensions(msg.Options.ProtoReflect(), mergeableFields)
		}
		mergeMessageOptionsInMessages(msg.GetNestedType(), mergeableFields)
	}
}

// mergeEnumOptions merges unknown extensions in EnumOptions for top-level enums.
func mergeEnumOptions(enums []*descriptorpb.EnumDescriptorProto, mergeableFields map[int32]bool) {
	for _, enum := range enums {
		if enum.Options != nil {
			mergeUnknownExtensions(enum.Options.ProtoReflect(), mergeableFields)
		}
	}
}

// mergeEnumOptionsInMessages recursively merges unknown extensions in EnumOptions
// for all enums in the given messages and their nested types.
func mergeEnumOptionsInMessages(msgs []*descriptorpb.DescriptorProto, mergeableFields map[int32]bool) {
	for _, msg := range msgs {
		mergeEnumOptions(msg.GetEnumType(), mergeableFields)
		mergeEnumOptionsInMessages(msg.GetNestedType(), mergeableFields)
	}
}

// mergeEnumValueOptions merges unknown extensions in EnumValueOptions
// for all values in the given enums.
func mergeEnumValueOptions(enums []*descriptorpb.EnumDescriptorProto, mergeableFields map[int32]bool) {
	for _, enum := range enums {
		for _, val := range enum.GetValue() {
			if val.Options != nil {
				mergeUnknownExtensions(val.Options.ProtoReflect(), mergeableFields)
			}
		}
	}
}

// mergeEnumValueOptionsInMessages recursively merges unknown extensions in EnumValueOptions
// for all enum values in the given messages and their nested types.
func mergeEnumValueOptionsInMessages(msgs []*descriptorpb.DescriptorProto, mergeableFields map[int32]bool) {
	for _, msg := range msgs {
		mergeEnumValueOptions(msg.GetEnumType(), mergeableFields)
		mergeEnumValueOptionsInMessages(msg.GetNestedType(), mergeableFields)
	}
}

// mergeServiceOptions merges unknown extensions in ServiceOptions for all services.
func mergeServiceOptions(services []*descriptorpb.ServiceDescriptorProto, mergeableFields map[int32]bool) {
	for _, svc := range services {
		if svc.Options != nil {
			mergeUnknownExtensions(svc.Options.ProtoReflect(), mergeableFields)
		}
	}
}

// mergeMethodOptions merges unknown extensions in MethodOptions for all methods in all services.
func mergeMethodOptions(services []*descriptorpb.ServiceDescriptorProto, mergeableFields map[int32]bool) {
	for _, svc := range services {
		for _, method := range svc.GetMethod() {
			if method.Options != nil {
				mergeUnknownExtensions(method.Options.ProtoReflect(), mergeableFields)
			}
		}
	}
}

// mergeOneofOptionsInMessages recursively merges unknown extensions in OneofOptions
// for all oneofs in the given messages and their nested types.
func mergeOneofOptionsInMessages(msgs []*descriptorpb.DescriptorProto, mergeableFields map[int32]bool) {
	for _, msg := range msgs {
		for _, oneof := range msg.GetOneofDecl() {
			if oneof.Options != nil {
				mergeUnknownExtensions(oneof.Options.ProtoReflect(), mergeableFields)
			}
		}
		mergeOneofOptionsInMessages(msg.GetNestedType(), mergeableFields)
	}
}

func mergeExtRangeOptionsInMessages(msgs []*descriptorpb.DescriptorProto, mergeableFields map[int32]bool) {
	for _, msg := range msgs {
		for _, er := range msg.GetExtensionRange() {
			if er.Options != nil {
				mergeUnknownExtensions(er.Options.ProtoReflect(), mergeableFields)
			}
		}
		mergeExtRangeOptionsInMessages(msg.GetNestedType(), mergeableFields)
	}
}

// mergeUnknownExtensions merges multiple unknown field entries with the same
// field number (BytesType) into single entries. If mergeableFields is non-nil,
// only field numbers in the set are merged; others are left as separate entries.
func mergeUnknownExtensions(m protoreflect.Message, mergeableFields map[int32]bool) {
	raw := m.GetUnknown()
	if len(raw) == 0 {
		return
	}

	type entry struct {
		num     protowire.Number
		wtyp    protowire.Type
		payload []byte
		raw     []byte
	}
	var entries []entry
	buf := raw
	for len(buf) > 0 {
		entryStart := buf
		num, wtyp, n := protowire.ConsumeTag(buf)
		if n < 0 {
			return
		}
		buf = buf[n:]
		var payload []byte
		switch wtyp {
		case protowire.BytesType:
			v, vn := protowire.ConsumeBytes(buf)
			if vn < 0 {
				return
			}
			payload = v
			buf = buf[vn:]
		case protowire.VarintType:
			_, vn := protowire.ConsumeVarint(buf)
			if vn < 0 {
				return
			}
			buf = buf[vn:]
		case protowire.Fixed32Type:
			_, vn := protowire.ConsumeFixed32(buf)
			if vn < 0 {
				return
			}
			buf = buf[vn:]
		case protowire.Fixed64Type:
			_, vn := protowire.ConsumeFixed64(buf)
			if vn < 0 {
				return
			}
			buf = buf[vn:]
		default:
			return
		}
		entries = append(entries, entry{num: num, wtyp: wtyp, payload: payload, raw: entryStart[:len(entryStart)-len(buf)]})
	}

	// Check if any BytesType field appears more than once and is mergeable
	counts := make(map[protowire.Number]int)
	needsMerge := false
	for _, e := range entries {
		if e.wtyp == protowire.BytesType && (mergeableFields == nil || mergeableFields[int32(e.num)]) {
			counts[e.num]++
			if counts[e.num] > 1 {
				needsMerge = true
			}
		}
	}
	if !needsMerge {
		return
	}

	merged := make(map[protowire.Number][]byte)
	for _, e := range entries {
		if e.wtyp == protowire.BytesType && (mergeableFields == nil || mergeableFields[int32(e.num)]) {
			merged[e.num] = append(merged[e.num], e.payload...)
		}
	}
	emitted := make(map[protowire.Number]bool)
	var result []byte
	for _, e := range entries {
		if e.wtyp == protowire.BytesType && (mergeableFields == nil || mergeableFields[int32(e.num)]) {
			if !emitted[e.num] {
				emitted[e.num] = true
				result = protowire.AppendTag(result, e.num, protowire.BytesType)
				result = protowire.AppendBytes(result, merged[e.num])
			}
		} else {
			result = append(result, e.raw...)
		}
	}
	m.SetUnknown(result)
}

// stripSourceRetention returns a copy of fd with source-retention-only options removed.
// If the descriptor has no such options, returns the original to avoid cloning.
func stripSourceRetention(fd *descriptorpb.FileDescriptorProto) *descriptorpb.FileDescriptorProto {
	if !hasExtRangeOpts(fd.GetMessageType()) {
		return fd
	}
	fdCopy := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	// Collect paths of ext ranges whose options become nil after stripping verification.
	var emptyOptsPaths [][]int32
	clearExtRangeOptsCollect(fdCopy.GetMessageType(), []int32{4}, &emptyOptsPaths)
	if fdCopy.SourceCodeInfo != nil {
		var filtered []*descriptorpb.SourceCodeInfo_Location
		for _, loc := range fdCopy.SourceCodeInfo.Location {
			if !isStrippedExtRangeOptsPath(loc.Path, emptyOptsPaths) {
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

// clearExtRangeOptsCollect clears verification from ext range options and
// collects paths prefix (e.g. [4,0,5,0,3]) for ranges whose options became nil.
func clearExtRangeOptsCollect(msgs []*descriptorpb.DescriptorProto, basePath []int32, emptyPaths *[][]int32) {
	for mi, msg := range msgs {
		msgPath := append(append([]int32{}, basePath...), int32(mi))
		for ri, er := range msg.GetExtensionRange() {
			if er.Options != nil {
				er.Options.Verification = nil
				er.Options.Declaration = nil
				if proto.Size(er.Options) == 0 {
					er.Options = nil
					optsPath := append(append([]int32{}, msgPath...), 5, int32(ri), 3)
					*emptyPaths = append(*emptyPaths, optsPath)
				}
			}
		}
		clearExtRangeOptsCollect(msg.GetNestedType(), append(append([]int32{}, msgPath...), 3), emptyPaths)
	}
}

// isStrippedExtRangeOptsPath checks if a SCI path should be stripped.
// Strip: verification-specific paths ([..., 5, N, 3, 3, ...]) always.
// Also strip options container and all sub-paths for ranges in emptyPaths.
func isStrippedExtRangeOptsPath(path []int32, emptyPaths [][]int32) bool {
	// Check if this is a verification path (field 3 of ExtensionRangeOptions)
	if isVerificationPath(path) {
		return true
	}
	// Check if this path is under an emptied options container
	for _, ep := range emptyPaths {
		if len(path) >= len(ep) && pathPrefix(path, ep) {
			return true
		}
	}
	return false
}

func pathPrefix(path, prefix []int32) bool {
	for i, v := range prefix {
		if path[i] != v {
			return false
		}
	}
	return true
}

// isVerificationPath checks if a SCI path points to the verification field
// (field 3) within ExtensionRangeOptions.
// Pattern: 4, N, [3, N, ]*, 5, N, 3, 3[, ...]
func isVerificationPath(path []int32) bool {
	if len(path) < 6 || path[0] != 4 {
		return false
	}
	i := 2
	for i+3 < len(path) {
		if path[i] == 5 && path[i+2] == 3 && path[i+3] == 3 {
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

// formatErrorsMSVS transforms error lines from GCC format to MSVS format.
// GCC: "file:line:col: message" → MSVS: "file(line) : error in column=col: message"
// GCC: "file: message" → MSVS: "file: message" (filename resolved to disk path)
func formatErrorsMSVS(errors []string, srcTree *importer.SourceTree) []string {
	result := make([]string, len(errors))
	for i, e := range errors {
		result[i] = formatErrorLineMSVS(e, srcTree)
	}
	return result
}

func formatErrorLineMSVS(line string, srcTree *importer.SourceTree) string {
	// Try to parse "filename:line:col: message"
	// Find first ":" then check if followed by "digits:digits: "
	colon1 := strings.Index(line, ":")
	if colon1 < 0 {
		return line
	}
	rest := line[colon1+1:]
	colon2 := strings.Index(rest, ":")
	if colon2 < 0 {
		// "filename: message" — resolve filename
		filename := line[:colon1]
		if srcTree != nil {
			if diskPath, ok := srcTree.VirtualFileToDiskFile(filename); ok {
				return diskPath + line[colon1:]
			}
		}
		return line
	}

	lineStr := rest[:colon2]
	lineNum, err1 := strconv.Atoi(lineStr)
	if err1 != nil {
		// Not a line number — treat as "filename: message"
		filename := line[:colon1]
		if srcTree != nil {
			if diskPath, ok := srcTree.VirtualFileToDiskFile(filename); ok {
				return diskPath + line[colon1:]
			}
		}
		return line
	}

	rest2 := rest[colon2+1:]
	colon3 := strings.Index(rest2, ":")
	if colon3 < 0 {
		return line
	}

	colStr := rest2[:colon3]
	colNum, err2 := strconv.Atoi(colStr)
	if err2 != nil {
		return line
	}

	message := rest2[colon3+1:] // includes leading space
	filename := line[:colon1]
	if srcTree != nil {
		if diskPath, ok := srcTree.VirtualFileToDiskFile(filename); ok {
			filename = diskPath
		}
	}
	return fmt.Sprintf("%s(%d) : error in column=%d:%s", filename, lineNum, colNum, message)
}

type fileOptExtInfo struct {
	field *descriptorpb.FieldDescriptorProto
	pkg   string
}

// resolveCustomFileOptions resolves parenthesized custom file options against
// extension definitions. It finds the matching extension field, encodes the
// value, and sets it on the FileOptions proto as unknown (extension) fields.
// It also returns a map from filename to the set of extension field numbers
// that have sub-field options (needed to know which fields to merge in proto_file).
func resolveCustomFileOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	// Build extension map: name → extension field for FileOptions extensions
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.FileOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		// Also check extensions nested in messages
		for _, msg := range fd.GetMessageType() {
			collectFileOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	// Build enum value number map: enum FQN → (value name → number)
	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	// Build message field map: message FQN → (field name → field descriptor)
	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	var errs []string
	subFieldNums := map[string]map[int32]bool{}
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		// Track repeated index per extension field number
		repeatedIdx := map[int32]int32{}
		seenCustomOpts := map[string]bool{}
		for _, opt := range result.CustomFileOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED

			if !isRepeated && len(opt.SubFieldPath) == 0 {
				optKey := opt.ParenName
				if seenCustomOpts[optKey] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenCustomOpts[optKey] = true
			}

			// Validate boolean option values must be "true" or "false"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				if opt.Value != "inf" && opt.Value != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
				// Sub-field option: option (ext).sub1.sub2... = value;
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				// Walk through the message type hierarchy for each path segment
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				sciPath := []int32{8, ext.GetNumber()}
				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					sciPath = append(sciPath, subFieldDesc.GetNumber())
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						// Navigate into the sub-field's message type
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}

				// Update SCI path with actual field numbers
				if opt.SCIIndex < len(fd.GetSourceCodeInfo().GetLocation()) {
					fd.GetSourceCodeInfo().GetLocation()[opt.SCIIndex].Path = sciPath
				}

				// Encode the leaf sub-field value
				value := opt.Value
				if opt.Negative {
					value = "-" + value
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				// Wrap in nested length-delimited tags from innermost to outermost
				// Walk the path segments in reverse to build nested encoding
				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					// Look up the field number at this level
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				// Wrap in the extension's length-delimited tag
				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				fd.Options.ProtoReflect().SetUnknown(
					append(fd.Options.ProtoReflect().GetUnknown(), rawBytes...))
			} else {
				// Update SCI path with actual field number
				if opt.SCIIndex < len(fd.GetSourceCodeInfo().GetLocation()) {
					if isRepeated {
						idx := repeatedIdx[ext.GetNumber()]
						fd.GetSourceCodeInfo().GetLocation()[opt.SCIIndex].Path = []int32{8, ext.GetNumber(), idx}
						repeatedIdx[ext.GetNumber()] = idx + 1
					} else {
						fd.GetSourceCodeInfo().GetLocation()[opt.SCIIndex].Path = []int32{8, ext.GetNumber()}
					}
				}

				// Encode the extension value as protowire bytes
				var rawBytes []byte
				var err error
				if opt.AggregateFields != nil {
					rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
				} else {
					value := opt.Value
					if opt.Negative {
						value = "-" + value
					}
					rawBytes, err = encodeCustomOptionValue(ext, value, opt.ValueType, enumValueNumbers)
				}
				if err != nil {
					errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
					continue
				}

				fd.Options.ProtoReflect().SetUnknown(
					append(fd.Options.ProtoReflect().GetUnknown(), rawBytes...))
			}
		}
	}
	return errs, subFieldNums
}

func collectEnumValueNumbers(enums []*descriptorpb.EnumDescriptorProto, prefix string, out map[string]map[string]int32) {
	for _, e := range enums {
		fqn := prefix + "." + e.GetName()
		if prefix == "" {
			fqn = e.GetName()
		}
		vals := map[string]int32{}
		for _, v := range e.GetValue() {
			vals[v.GetName()] = v.GetNumber()
		}
		out[fqn] = vals
	}
}

func collectEnumValueNumbersInMsgs(msgs []*descriptorpb.DescriptorProto, prefix string, out map[string]map[string]int32) {
	for _, msg := range msgs {
		msgFQN := prefix + "." + msg.GetName()
		if prefix == "" {
			msgFQN = msg.GetName()
		}
		collectEnumValueNumbers(msg.GetEnumType(), msgFQN, out)
		collectEnumValueNumbersInMsgs(msg.GetNestedType(), msgFQN, out)
	}
}

func collectFileOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.FileOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectFileOptionsExtensions(nested, msgFQN, exts)
	}
}

// buildMsgFQNMap builds a map from *DescriptorProto to its fully-qualified name
// for all messages in the given file descriptors.
func buildMsgFQNMap(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) map[*descriptorpb.DescriptorProto]string {
	m := map[*descriptorpb.DescriptorProto]string{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for _, msg := range fd.GetMessageType() {
			buildMsgFQNMapRecursive(msg, pkg, m)
		}
	}
	return m
}

func buildMsgFQNMapRecursive(msg *descriptorpb.DescriptorProto, parentFQN string, m map[*descriptorpb.DescriptorProto]string) {
	fqn := parentFQN + "." + msg.GetName()
	if parentFQN == "" {
		fqn = msg.GetName()
	}
	m[msg] = fqn
	for _, nested := range msg.GetNestedType() {
		buildMsgFQNMapRecursive(nested, fqn, m)
	}
}

// findFileOptionExtension looks up an extension field by name, considering package scope.
func findFileOptionExtension(name string, currentPkg string, allExts []fileOptExtInfo) (*descriptorpb.FieldDescriptorProto, string) {
	buildFQN := func(e fileOptExtInfo) string {
		if e.pkg != "" {
			return e.pkg + "." + e.field.GetName()
		}
		return e.field.GetName()
	}
	// Try fully-qualified lookup (name starts with .)
	if strings.HasPrefix(name, ".") {
		fqn := name[1:] // strip leading dot
		for _, e := range allExts {
			if buildFQN(e) == fqn {
				return e.field, buildFQN(e)
			}
		}
		return nil, ""
	}

	// Walk scopes from innermost (current package) to outermost (root),
	// matching C++ protoc scope resolution for extension names.
	scope := currentPkg
	for {
		candidate := name
		if scope != "" {
			candidate = scope + "." + name
		}
		for _, e := range allExts {
			if buildFQN(e) == candidate {
				return e.field, buildFQN(e)
			}
		}
		if scope == "" {
			break
		}
		// Move to parent scope
		if dot := strings.LastIndex(scope, "."); dot >= 0 {
			scope = scope[:dot]
		} else {
			scope = ""
		}
	}

	return nil, ""
}

// resolveCustomFieldOptions resolves parenthesized custom options on fields
// (e.g., [(my_ext) = "value"]) against extension definitions for FieldOptions.
func resolveCustomFieldOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	subFieldNums := map[string]map[int32]bool{}
	// Build extension map for FieldOptions extensions
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.FieldOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectFieldOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	// Build enum value number map
	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	// Build message field map
	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	type fieldRepKey struct {
		field *descriptorpb.FieldDescriptorProto
		num   int32
	}
	fieldRepeatIdx := map[fieldRepKey]int32{}

	type fieldOptKey struct {
		field *descriptorpb.FieldDescriptorProto
		name  string
	}
	seenFieldOpts := map[fieldOptKey]bool{}

	var errs []string
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomFieldOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				k := fieldOptKey{opt.Field, opt.ParenName}
				if seenFieldOpts[k] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenFieldOpts[k] = true
			}

			// Validate boolean option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.ValTok.Line+1, opt.ValTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.ValTok.Line+1, opt.ValTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.ValTok.Line+1, opt.ValTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.ValTok.Line+1, opt.ValTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				floatCheckVal := opt.Value
				if strings.HasPrefix(floatCheckVal, "-") {
					floatCheckVal = floatCheckVal[1:]
				}
				if floatCheckVal != "inf" && floatCheckVal != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			if opt.SCILoc != nil && len(opt.SCILoc.Path) >= 2 {
				opt.SCILoc.Path[len(opt.SCILoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
			}

			// For repeated extensions, append an index to the SCI path
			if ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED && opt.SCILoc != nil {
				key := fieldRepKey{opt.Field, ext.GetNumber()}
				idx := fieldRepeatIdx[key]
				fieldRepeatIdx[key]++
				insertPos := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				newPath := make([]int32, len(opt.SCILoc.Path)+1)
				copy(newPath, opt.SCILoc.Path[:insertPos])
				newPath[insertPos] = idx
				copy(newPath[insertPos+1:], opt.SCILoc.Path[insertPos:])
				opt.SCILoc.Path = newPath
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
if subFieldNums[name] == nil {
subFieldNums[name] = map[int32]bool{}
}
subFieldNums[name][ext.GetNumber()] = true
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				sciPathOffset := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					if opt.SCILoc != nil {
						opt.SCILoc.Path[sciPathOffset+i] = subFieldDesc.GetNumber()
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, opt.Value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				opt.Field.Options.ProtoReflect().SetUnknown(
					append(opt.Field.Options.ProtoReflect().GetUnknown(), rawBytes...))
				continue
			}

			// Encode the extension value as protowire bytes
			var rawBytes []byte
			var err error
			if opt.AggregateFields != nil {
				rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
			} else {
				rawBytes, err = encodeCustomOptionValue(ext, opt.Value, opt.ValueType, enumValueNumbers)
			}
			if err != nil {
				errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
				continue
			}

			// Add to FieldOptions unknown fields
			opt.Field.Options.ProtoReflect().SetUnknown(
				append(opt.Field.Options.ProtoReflect().GetUnknown(), rawBytes...))
		}
	}
	return errs, subFieldNums
}

func collectFieldOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.FieldOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectFieldOptionsExtensions(nested, msgFQN, exts)
	}
}

// resolveCustomMessageOptions resolves parenthesized custom options on messages
// (e.g., option (my_msg_label) = "primary";) against extension definitions for MessageOptions.
func resolveCustomMessageOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	// Build extension map for MessageOptions extensions
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.MessageOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectMessageOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	// Build enum value number map
	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	// Build message field map
	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)
	msgFQNMap := buildMsgFQNMap(orderedFiles, parsed)

	type msgOptKey struct {
		msg  *descriptorpb.DescriptorProto
		name string
	}
	seenMsgOpts := map[msgOptKey]bool{}

	var errs []string
	subFieldNums := map[string]map[int32]bool{}
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomMessageOptions {
			scope := fd.GetPackage()
			if fqn, ok := msgFQNMap[opt.Message]; ok {
				scope = fqn
			}
			ext, extFQN := findFileOptionExtension(opt.InnerName, scope, allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				k := msgOptKey{opt.Message, opt.ParenName}
				if seenMsgOpts[k] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenMsgOpts[k] = true
			}

			// Update SCI path with actual field number
			if opt.SCILoc != nil && len(opt.SCILoc.Path) >= 2 {
				opt.SCILoc.Path[len(opt.SCILoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			// Validate boolean option values must be "true" or "false"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				floatCheckVal := opt.Value
				if strings.HasPrefix(floatCheckVal, "-") {
					floatCheckVal = floatCheckVal[1:]
				}
				if floatCheckVal != "inf" && floatCheckVal != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				sciPathOffset := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					if opt.SCILoc != nil {
						opt.SCILoc.Path[sciPathOffset+i] = subFieldDesc.GetNumber()
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, opt.Value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				opt.Message.Options.ProtoReflect().SetUnknown(
					append(opt.Message.Options.ProtoReflect().GetUnknown(), rawBytes...))
				continue
			}

			// Encode the extension value as protowire bytes
			var rawBytes []byte
			var err error
			if opt.AggregateFields != nil {
				rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
			} else {
				rawBytes, err = encodeCustomOptionValue(ext, opt.Value, opt.ValueType, enumValueNumbers)
			}
			if err != nil {
				errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
				continue
			}

			// Add to MessageOptions unknown fields
			opt.Message.Options.ProtoReflect().SetUnknown(
				append(opt.Message.Options.ProtoReflect().GetUnknown(), rawBytes...))
		}
	}
	return errs, subFieldNums
}

func collectMessageOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.MessageOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectMessageOptionsExtensions(nested, msgFQN, exts)
	}
}

// resolveCustomServiceOptions resolves parenthesized custom options on services
// (e.g., option (service_label) = "primary";) against extensions of ServiceOptions.
func resolveCustomServiceOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	subFieldNums := map[string]map[int32]bool{}
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.ServiceOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectServiceOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	type svcOptKey struct {
		svc  *descriptorpb.ServiceDescriptorProto
		name string
	}
	seenSvcOpts := map[svcOptKey]bool{}

	var errs []string
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomServiceOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				k := svcOptKey{opt.Service, opt.ParenName}
				if seenSvcOpts[k] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenSvcOpts[k] = true
			}

			if opt.SCILoc != nil && len(opt.SCILoc.Path) >= 2 {
				opt.SCILoc.Path[len(opt.SCILoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			// Validate boolean option values must be "true" or "false"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				floatCheckVal := opt.Value
				if strings.HasPrefix(floatCheckVal, "-") {
					floatCheckVal = floatCheckVal[1:]
				}
				if floatCheckVal != "inf" && floatCheckVal != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				sciPathOffset := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					if opt.SCILoc != nil {
						opt.SCILoc.Path[sciPathOffset+i] = subFieldDesc.GetNumber()
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, opt.Value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				opt.Service.Options.ProtoReflect().SetUnknown(
					append(opt.Service.Options.ProtoReflect().GetUnknown(), rawBytes...))
				continue
			}

			var rawBytes []byte
			var err error
			if opt.AggregateFields != nil {
				rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
			} else {
				rawBytes, err = encodeCustomOptionValue(ext, opt.Value, opt.ValueType, enumValueNumbers)
			}
			if err != nil {
				errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
				continue
			}

			opt.Service.Options.ProtoReflect().SetUnknown(
				append(opt.Service.Options.ProtoReflect().GetUnknown(), rawBytes...))
		}
	}
	return errs, subFieldNums
}

func collectServiceOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.ServiceOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectServiceOptionsExtensions(nested, msgFQN, exts)
	}
}

// resolveCustomMethodOptions resolves parenthesized custom options on methods
// (e.g., option (auth_role) = "admin";) against extensions of MethodOptions.
func resolveCustomMethodOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	subFieldNums := map[string]map[int32]bool{}
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.MethodOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectMethodOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	type mtdOptKey struct {
		mtd  *descriptorpb.MethodDescriptorProto
		name string
	}
	seenMtdOpts := map[mtdOptKey]bool{}

	var errs []string
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomMethodOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				k := mtdOptKey{opt.Method, opt.ParenName}
				if seenMtdOpts[k] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenMtdOpts[k] = true
			}

			if opt.SCILoc != nil && len(opt.SCILoc.Path) >= 2 {
				opt.SCILoc.Path[len(opt.SCILoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			// Validate boolean option values must be "true" or "false"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				floatCheckVal := opt.Value
				if strings.HasPrefix(floatCheckVal, "-") {
					floatCheckVal = floatCheckVal[1:]
				}
				if floatCheckVal != "inf" && floatCheckVal != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				sciPathOffset := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					if opt.SCILoc != nil {
						opt.SCILoc.Path[sciPathOffset+i] = subFieldDesc.GetNumber()
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, opt.Value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				opt.Method.Options.ProtoReflect().SetUnknown(
					append(opt.Method.Options.ProtoReflect().GetUnknown(), rawBytes...))
				continue
			}

			var rawBytes []byte
			var err error
			if opt.AggregateFields != nil {
				rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
			} else {
				rawBytes, err = encodeCustomOptionValue(ext, opt.Value, opt.ValueType, enumValueNumbers)
			}
			if err != nil {
				errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
				continue
			}

			opt.Method.Options.ProtoReflect().SetUnknown(
				append(opt.Method.Options.ProtoReflect().GetUnknown(), rawBytes...))
		}
	}
	return errs, subFieldNums
}

func collectMethodOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.MethodOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectMethodOptionsExtensions(nested, msgFQN, exts)
	}
}

// resolveCustomEnumOptions resolves parenthesized custom options on enums
// (e.g., option (enum_label) = "status_tracker";) against extensions of EnumOptions.
func resolveCustomEnumOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	subFieldNums := map[string]map[int32]bool{}
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.EnumOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectEnumOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	type enumOptKey struct {
		enum *descriptorpb.EnumDescriptorProto
		name string
	}
	seenEnumOpts := map[enumOptKey]bool{}

	var errs []string
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomEnumOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				k := enumOptKey{opt.Enum, opt.ParenName}
				if seenEnumOpts[k] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenEnumOpts[k] = true
			}

			if opt.SCILoc != nil && len(opt.SCILoc.Path) >= 2 {
				opt.SCILoc.Path[len(opt.SCILoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
			}

			// Validate boolean option values must be "true" or "false"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				floatCheckVal := opt.Value
				if strings.HasPrefix(floatCheckVal, "-") {
					floatCheckVal = floatCheckVal[1:]
				}
				if floatCheckVal != "inf" && floatCheckVal != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				sciPathOffset := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					if opt.SCILoc != nil {
						opt.SCILoc.Path[sciPathOffset+i] = subFieldDesc.GetNumber()
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, opt.Value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				opt.Enum.Options.ProtoReflect().SetUnknown(
					append(opt.Enum.Options.ProtoReflect().GetUnknown(), rawBytes...))
				continue
			}

			var rawBytes []byte
			var err error
			if opt.AggregateFields != nil {
				rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
			} else {
				rawBytes, err = encodeCustomOptionValue(ext, opt.Value, opt.ValueType, enumValueNumbers)
			}
			if err != nil {
				errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
				continue
			}

			opt.Enum.Options.ProtoReflect().SetUnknown(
				append(opt.Enum.Options.ProtoReflect().GetUnknown(), rawBytes...))
		}
	}
	return errs, subFieldNums
}

func collectEnumOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.EnumOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectEnumOptionsExtensions(nested, msgFQN, exts)
	}
}

// resolveCustomEnumValueOptions resolves parenthesized custom options on enum values
// (e.g., HIGH = 1 [(display_name) = "High Priority"]) against extension definitions for EnumValueOptions.
func resolveCustomEnumValueOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	subFieldNums := map[string]map[int32]bool{}
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.EnumValueOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectEnumValueOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	type evOptKey struct {
		ev   *descriptorpb.EnumValueDescriptorProto
		name string
	}
	seenEvOpts := map[evOptKey]bool{}

	var errs []string
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomEnumValueOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				k := evOptKey{opt.EnumValue, opt.ParenName}
				if seenEvOpts[k] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenEvOpts[k] = true
			}

			if opt.SCILoc != nil && len(opt.SCILoc.Path) >= 2 {
				opt.SCILoc.Path[len(opt.SCILoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
			}

			// Validate boolean option values must be "true" or "false"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				floatCheckVal := opt.Value
				if strings.HasPrefix(floatCheckVal, "-") {
					floatCheckVal = floatCheckVal[1:]
				}
				if floatCheckVal != "inf" && floatCheckVal != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				sciPathOffset := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					if opt.SCILoc != nil {
						opt.SCILoc.Path[sciPathOffset+i] = subFieldDesc.GetNumber()
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, opt.Value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				opt.EnumValue.Options.ProtoReflect().SetUnknown(
					append(opt.EnumValue.Options.ProtoReflect().GetUnknown(), rawBytes...))
				continue
			}

			var rawBytes []byte
			var err error
			if opt.AggregateFields != nil {
				rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
			} else {
				rawBytes, err = encodeCustomOptionValue(ext, opt.Value, opt.ValueType, enumValueNumbers)
			}
			if err != nil {
				errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
				continue
			}

			opt.EnumValue.Options.ProtoReflect().SetUnknown(
				append(opt.EnumValue.Options.ProtoReflect().GetUnknown(), rawBytes...))
		}
	}
	return errs, subFieldNums
}

func collectEnumValueOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.EnumValueOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectEnumValueOptionsExtensions(nested, msgFQN, exts)
	}
}

// resolveCustomOneofOptions resolves parenthesized custom options on oneofs
// (e.g., option (oneof_label) = "primary";) against extensions of OneofOptions.
func resolveCustomOneofOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	subFieldNums := map[string]map[int32]bool{}
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.OneofOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectOneofOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	type oneofOptKey struct {
		oneof *descriptorpb.OneofDescriptorProto
		name  string
	}
	seenOneofOpts := map[oneofOptKey]bool{}

	var errs []string
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomOneofOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				k := oneofOptKey{opt.Oneof, opt.ParenName}
				if seenOneofOpts[k] {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				seenOneofOpts[k] = true
			}

			if opt.SCILoc != nil && len(opt.SCILoc.Path) >= 2 {
				opt.SCILoc.Path[len(opt.SCILoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			// Validate boolean option values must be "true" or "false"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				floatCheckVal := opt.Value
				if strings.HasPrefix(floatCheckVal, "-") {
					floatCheckVal = floatCheckVal[1:]
				}
				if floatCheckVal != "inf" && floatCheckVal != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, typeName, extFQN))
					continue
				}
			}

			if len(opt.SubFieldPath) > 0 {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}

				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				sciPathOffset := len(opt.SCILoc.Path) - len(opt.SubFieldPath)
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					if opt.SCILoc != nil {
						opt.SCILoc.Path[sciPathOffset+i] = subFieldDesc.GetNumber()
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				leafBytes, err := encodeCustomOptionValue(leafFieldDesc, opt.Value, opt.ValueType, enumValueNumbers)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: error encoding custom option: %v", name, err))
					continue
				}

				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentFields := msgFieldMap[parentTypeName]
						parentField := parentFields[opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentFields := msgFieldMap[parentTypeName]
					parentField := parentFields[opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}

				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)

				opt.Oneof.Options.ProtoReflect().SetUnknown(
					append(opt.Oneof.Options.ProtoReflect().GetUnknown(), rawBytes...))
				continue
			}

			var rawBytes []byte
			var err error
			if opt.AggregateFields != nil {
				rawBytes, err = encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
			} else {
				rawBytes, err = encodeCustomOptionValue(ext, opt.Value, opt.ValueType, enumValueNumbers)
			}
			if err != nil {
				errs = append(errs, formatAggregateError(err, name, opt.AggregateBraceTok, ext.GetName()))
				continue
			}

			opt.Oneof.Options.ProtoReflect().SetUnknown(
				append(opt.Oneof.Options.ProtoReflect().GetUnknown(), rawBytes...))
		}
	}
	return errs, subFieldNums
}

func collectOneofOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.OneofOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectOneofOptionsExtensions(nested, msgFQN, exts)
	}
}

// resolveCustomExtRangeOptions resolves parenthesized custom options on extension ranges
// (e.g., extensions 100 to 199 [(my_annotation) = "annotated"];) against extension definitions.
func resolveCustomExtRangeOptions(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, parseResults map[string]*parser.ParseResult) ([]string, map[string]map[int32]bool) {
	subFieldNums := map[string]map[int32]bool{}
	var allExts []fileOptExtInfo
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == ".google.protobuf.ExtensionRangeOptions" {
				allExts = append(allExts, fileOptExtInfo{field: ext, pkg: fd.GetPackage()})
			}
		}
		for _, msg := range fd.GetMessageType() {
			collectExtRangeOptionsExtensions(msg, fd.GetPackage(), &allExts)
		}
	}

	enumValueNumbers := map[string]map[string]int32{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := fd.GetPackage()
		collectEnumValueNumbers(fd.GetEnumType(), prefix, enumValueNumbers)
		collectEnumValueNumbersInMsgs(fd.GetMessageType(), prefix, enumValueNumbers)
	}

	msgFieldMap := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, n := range orderedFiles {
		fd := parsed[n]
		prefix := fd.GetPackage()
		collectMsgFields(fd.GetMessageType(), prefix, msgFieldMap)
	}
	extByExtendee := collectExtensionsByExtendee(orderedFiles, parsed)

	type extRangeOptKey struct {
		rng  *descriptorpb.DescriptorProto_ExtensionRange
		name string
	}
	seenExtRangeOpts := map[extRangeOptKey]bool{}

	var errs []string
	for _, name := range orderedFiles {
		result := parseResults[name]
		if result == nil {
			continue
		}
		fd := parsed[name]
		for _, opt := range result.CustomExtRangeOptions {
			ext, extFQN := findFileOptionExtension(opt.InnerName, fd.GetPackage(), allExts)
			if ext == nil {
				errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).",
					name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
				continue
			}

			isRepeated := ext.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if !isRepeated && len(opt.SubFieldPath) == 0 {
				dup := false
				for _, rng := range opt.Ranges {
					k := extRangeOptKey{rng, opt.ParenName}
					if seenExtRangeOpts[k] {
						dup = true
						break
					}
				}
				if dup {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" was already set.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				for _, rng := range opt.Ranges {
					k := extRangeOptKey{rng, opt.ParenName}
					seenExtRangeOpts[k] = true
				}
			}

			// Validate boolean option values
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for boolean option \"%s\".",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, extFQN))
					continue
				}
				if opt.Value != "true" && opt.Value != "false" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be \"true\" or \"false\" for boolean option \"%s\".",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, extFQN))
					continue
				}
			}


			// Validate enum option values must be identifiers
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenIdent {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be identifier for enum-valued option \"%s\".",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, extFQN))
					continue
				}
			}
			// Validate string/bytes option values must be quoted strings
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if opt.ValueType != tokenizer.TokenString {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be quoted string for string option \"%s\".",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, extFQN))
					continue
				}
			}

			// Validate float/double identifier values must be lowercase "inf" or "nan"
			if (ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE) && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 && opt.ValueType == tokenizer.TokenIdent {
				if opt.Value != "inf" && opt.Value != "nan" {
					typeName := "float"
					if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
						typeName = "double"
					}
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Value must be number for %s option \"%s\".",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, typeName, extFQN))
					continue
				}
			}

			// Validate integer range for 32-bit types
			if opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 {
				if rangeErr := checkIntRangeOption(ext, opt.Value, opt.Negative, extFQN); rangeErr != "" {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: %s",
						name, opt.AggregateBraceTok.Line+1, opt.AggregateBraceTok.Column+1, rangeErr))
					continue
				}
			}

			// Update SCI paths with actual field number
			for _, sciLoc := range opt.SCILocs {
				if sciLoc != nil && len(sciLoc.Path) >= 1 {
					sciLoc.Path[len(sciLoc.Path)-1-len(opt.SubFieldPath)] = ext.GetNumber()
				}
			}

			if len(opt.SubFieldPath) > 0 {
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE && ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					errs = append(errs, fmt.Sprintf("%s:%d:%d: Option \"%s\" is an atomic type, not a message.",
						name, opt.NameTok.Line+1, opt.NameTok.Column+1, opt.ParenName))
					continue
				}
				if subFieldNums[name] == nil {
					subFieldNums[name] = map[int32]bool{}
				}
				subFieldNums[name][ext.GetNumber()] = true
				currentTypeName := ext.GetTypeName()
				if strings.HasPrefix(currentTypeName, ".") {
					currentTypeName = currentTypeName[1:]
				}
				var leafFieldDesc *descriptorpb.FieldDescriptorProto
				valid := true
				for i, seg := range opt.SubFieldPath {
					fields, ok := msgFieldMap[currentTypeName]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown message type %s for extension %s", name, currentTypeName, opt.InnerName))
						valid = false
						break
					}
					subFieldDesc, ok := fields[seg]
					if !ok {
						errs = append(errs, fmt.Sprintf("%s: unknown field %q in message type %s", name, seg, currentTypeName))
						valid = false
						break
					}
					for _, sciLoc := range opt.SCILocs {
						if sciLoc != nil {
							sciLoc.Path[len(sciLoc.Path)-len(opt.SubFieldPath)+i] = subFieldDesc.GetNumber()
						}
					}
					if i == len(opt.SubFieldPath)-1 {
						leafFieldDesc = subFieldDesc
					} else {
						nextType := subFieldDesc.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						currentTypeName = nextType
					}
				}
				if !valid {
					continue
				}
				val := opt.Value
				if opt.Negative {
					val = "-" + val
				}
				leafBytes, encErr := encodeCustomOptionValue(leafFieldDesc, val, opt.ValueType, enumValueNumbers)
				if encErr != nil {
					errs = append(errs, fmt.Sprintf("%s: %s", name, encErr.Error()))
					continue
				}
				encoded := leafBytes
				for i := len(opt.SubFieldPath) - 2; i >= 0; i-- {
					parentTypeName := ext.GetTypeName()
					if strings.HasPrefix(parentTypeName, ".") {
						parentTypeName = parentTypeName[1:]
					}
					for j := 0; j < i; j++ {
						parentField := msgFieldMap[parentTypeName][opt.SubFieldPath[j]]
						nextType := parentField.GetTypeName()
						if strings.HasPrefix(nextType, ".") {
							nextType = nextType[1:]
						}
						parentTypeName = nextType
					}
					parentField := msgFieldMap[parentTypeName][opt.SubFieldPath[i]]
					var wrapper []byte
					wrapper = protowire.AppendTag(wrapper, protowire.Number(parentField.GetNumber()), protowire.BytesType)
					wrapper = protowire.AppendBytes(wrapper, encoded)
					encoded = wrapper
				}
				var rawBytes []byte
				rawBytes = protowire.AppendTag(rawBytes, protowire.Number(ext.GetNumber()), protowire.BytesType)
				rawBytes = protowire.AppendBytes(rawBytes, encoded)
				for _, rng := range opt.Ranges {
					rng.Options.ProtoReflect().SetUnknown(
						append(rng.Options.ProtoReflect().GetUnknown(), rawBytes...))
				}
			} else if opt.AggregateFields != nil {
				rawBytes, aggErr := encodeAggregateOption(ext, opt.AggregateFields, msgFieldMap, enumValueNumbers, extByExtendee)
				if aggErr != nil {
					if ade, ok := aggErr.(*aggregateDupFieldError); ok {
						errs = append(errs, formatAggregateError(ade, name, opt.AggregateBraceTok, opt.ParenName))
					} else {
						errs = append(errs, fmt.Sprintf("%s: %s", name, aggErr.Error()))
					}
					continue
				}
				for _, rng := range opt.Ranges {
					rng.Options.ProtoReflect().SetUnknown(
						append(rng.Options.ProtoReflect().GetUnknown(), rawBytes...))
				}
			} else {
				val := opt.Value
				if opt.Negative {
					val = "-" + val
				}
				rawBytes, encErr := encodeCustomOptionValue(ext, val, opt.ValueType, enumValueNumbers)
				if encErr != nil {
					errs = append(errs, fmt.Sprintf("%s: %s", name, encErr.Error()))
					continue
				}
				for _, rng := range opt.Ranges {
					rng.Options.ProtoReflect().SetUnknown(
						append(rng.Options.ProtoReflect().GetUnknown(), rawBytes...))
				}
			}
		}
	}
	return errs, subFieldNums
}

func collectExtRangeOptionsExtensions(msg *descriptorpb.DescriptorProto, parentFQN string, exts *[]fileOptExtInfo) {
	msgFQN := parentFQN + "." + msg.GetName()
	for _, ext := range msg.GetExtension() {
		if ext.GetExtendee() == ".google.protobuf.ExtensionRangeOptions" {
			*exts = append(*exts, fileOptExtInfo{field: ext, pkg: msgFQN})
		}
	}
	for _, nested := range msg.GetNestedType() {
		collectExtRangeOptionsExtensions(nested, msgFQN, exts)
	}
}

// encodeCustomOptionValue encodes a custom option value as protowire bytes.
func encodeCustomOptionValue(ext *descriptorpb.FieldDescriptorProto, value string, valueType tokenizer.TokenType, enumValueNumbers map[string]map[string]int32) ([]byte, error) {
	fieldNum := protowire.Number(ext.GetNumber())
	var b []byte

	switch ext.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_STRING,
		descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		if valueType != tokenizer.TokenString {
			return nil, &aggregateStringExpectedError{gotValue: value}
		}
		b = protowire.AppendTag(b, fieldNum, protowire.BytesType)
		b = protowire.AppendString(b, value)
	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		v, err := strconv.ParseInt(value, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer value: %s", value)
		}
		if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_INT32 ||
			ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_SINT32 {
			if v < math.MinInt32 || v > math.MaxInt32 {
				return nil, &aggregateIntRangeError{rawValue: value}
			}
		}
		if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_SINT32 ||
			ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_SINT64 {
			b = protowire.AppendTag(b, fieldNum, protowire.VarintType)
			b = protowire.AppendVarint(b, protowire.EncodeZigZag(v))
		} else {
			b = protowire.AppendTag(b, fieldNum, protowire.VarintType)
			b = protowire.AppendVarint(b, uint64(v))
		}
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		v, err := strconv.ParseUint(value, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid unsigned integer value: %s", value)
		}
		if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_UINT32 {
			if v > math.MaxUint32 {
				return nil, &aggregateIntRangeError{rawValue: value}
			}
		}
		b = protowire.AppendTag(b, fieldNum, protowire.VarintType)
		b = protowire.AppendVarint(b, v)
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		b = protowire.AppendTag(b, fieldNum, protowire.VarintType)
		switch value {
		case "true", "True", "t", "1":
			b = protowire.AppendVarint(b, 1)
		case "false", "False", "f", "0":
			b = protowire.AppendVarint(b, 0)
		default:
			if _, err := strconv.ParseUint(value, 0, 64); err == nil {
				return nil, &aggregateIntRangeError{rawValue: value}
			}
			if len(value) > 0 && value[0] == '-' {
				if _, err := strconv.ParseInt(value, 0, 64); err == nil {
					return nil, &aggregateIntRangeError{rawValue: value}
				}
			}
			return nil, &aggregateBoolError{fieldName: ext.GetName(), value: value}
		}
	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		if valueType == tokenizer.TokenString {
			return nil, &aggregateFloatExpectedError{gotValue: "\"" + value + "\""}
		}
		var floatBits uint32
		switch strings.ToLower(value) {
		case "nan", "-nan":
			floatBits = 0x7FC00000 // C++ canonical float NaN
		default:
			v, err := strconv.ParseFloat(value, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid float value: %s", value)
			}
			floatBits = math.Float32bits(float32(v))
		}
		b = protowire.AppendTag(b, fieldNum, protowire.Fixed32Type)
		b = protowire.AppendFixed32(b, floatBits)
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		if valueType == tokenizer.TokenString {
			return nil, &aggregateFloatExpectedError{gotValue: "\"" + value + "\""}
		}
		var bits uint64
		switch strings.ToLower(value) {
		case "nan", "-nan":
			bits = 0x7FF8000000000000 // C++ canonical double NaN
		default:
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid double value: %s", value)
			}
			bits = math.Float64bits(v)
			if math.IsNaN(v) {
				bits = 0x7FF8000000000000
			}
		}
		b = protowire.AppendTag(b, fieldNum, protowire.Fixed64Type)
		b = protowire.AppendFixed64(b, bits)
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		v, err := strconv.ParseUint(value, 0, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid fixed32 value: %s", value)
		}
		b = protowire.AppendTag(b, fieldNum, protowire.Fixed32Type)
		b = protowire.AppendFixed32(b, uint32(v))
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		v, err := strconv.ParseInt(value, 0, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid sfixed32 value: %s", value)
		}
		b = protowire.AppendTag(b, fieldNum, protowire.Fixed32Type)
		b = protowire.AppendFixed32(b, uint32(int32(v)))
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		v, err := strconv.ParseUint(value, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid fixed64 value: %s", value)
		}
		b = protowire.AppendTag(b, fieldNum, protowire.Fixed64Type)
		b = protowire.AppendFixed64(b, v)
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		v, err := strconv.ParseInt(value, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid sfixed64 value: %s", value)
		}
		b = protowire.AppendTag(b, fieldNum, protowire.Fixed64Type)
		b = protowire.AppendFixed64(b, uint64(v))
	case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		v, err := strconv.ParseInt(value, 0, 32)
		if err != nil {
			// Resolve enum value name to number
			enumTypeName := ext.GetTypeName()
			if strings.HasPrefix(enumTypeName, ".") {
				enumTypeName = enumTypeName[1:]
			}
			vals, ok := enumValueNumbers[enumTypeName]
			if !ok {
				return nil, fmt.Errorf("unknown enum type %s for value: %s", ext.GetTypeName(), value)
			}
			num, found := vals[value]
			if !found {
				return nil, &aggregateEnumError{enumValue: value, fieldName: ext.GetName()}
			}
			v = int64(num)
		}
		b = protowire.AppendTag(b, fieldNum, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(v))
	default:
		return nil, fmt.Errorf("unsupported custom option type: %v", ext.GetType())
	}

	return b, nil
}

// collectMsgFields builds a map of message FQN → (field name → field descriptor).
func collectMsgFields(msgs []*descriptorpb.DescriptorProto, prefix string, out map[string]map[string]*descriptorpb.FieldDescriptorProto) {
	for _, msg := range msgs {
		fqn := msg.GetName()
		if prefix != "" {
			fqn = prefix + "." + msg.GetName()
		}
		fields := map[string]*descriptorpb.FieldDescriptorProto{}
		for _, f := range msg.GetField() {
			fields[f.GetName()] = f
			// Group fields are referenced by message type name in text format
			if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP {
				tn := f.GetTypeName()
				if idx := strings.LastIndex(tn, "."); idx >= 0 {
					tn = tn[idx+1:]
				}
				fields[tn] = f
			}
		}
		out[fqn] = fields
		collectMsgFields(msg.GetNestedType(), fqn, out)
	}
}

// collectExtensionsByExtendee builds a map: extendee FQN (without leading dot) → extension FQN → FieldDescriptorProto
// for resolving [ext.name] references in aggregate option message literals.
func collectExtensionsByExtendee(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) map[string]map[string]*descriptorpb.FieldDescriptorProto {
	out := map[string]map[string]*descriptorpb.FieldDescriptorProto{}
	for _, name := range orderedFiles {
		fd := parsed[name]
		pkg := fd.GetPackage()
		for _, ext := range fd.GetExtension() {
			extendee := ext.GetExtendee()
			if strings.HasPrefix(extendee, ".") {
				extendee = extendee[1:]
			}
			if out[extendee] == nil {
				out[extendee] = map[string]*descriptorpb.FieldDescriptorProto{}
			}
			fqn := ext.GetName()
			if pkg != "" {
				fqn = pkg + "." + fqn
			}
			out[extendee][fqn] = ext
		}
		collectMsgExtensionsByExtendee(fd.GetMessageType(), pkg, out)
	}
	return out
}

func collectMsgExtensionsByExtendee(msgs []*descriptorpb.DescriptorProto, prefix string, out map[string]map[string]*descriptorpb.FieldDescriptorProto) {
	for _, msg := range msgs {
		fqn := msg.GetName()
		if prefix != "" {
			fqn = prefix + "." + msg.GetName()
		}
		for _, ext := range msg.GetExtension() {
			extendee := ext.GetExtendee()
			if strings.HasPrefix(extendee, ".") {
				extendee = extendee[1:]
			}
			if out[extendee] == nil {
				out[extendee] = map[string]*descriptorpb.FieldDescriptorProto{}
			}
			extFqn := fqn + "." + ext.GetName()
			out[extendee][extFqn] = ext
		}
		collectMsgExtensionsByExtendee(msg.GetNestedType(), fqn, out)
	}
}

// encodeAggregateOption encodes an aggregate (message literal) custom option value.
func encodeAggregateOption(ext *descriptorpb.FieldDescriptorProto, aggFields []parser.AggregateField, msgFieldMap map[string]map[string]*descriptorpb.FieldDescriptorProto, enumValueNumbers map[string]map[string]int32, extByExtendee map[string]map[string]*descriptorpb.FieldDescriptorProto) ([]byte, error) {
	typeName := ext.GetTypeName()
	if strings.HasPrefix(typeName, ".") {
		typeName = typeName[1:]
	}

	msgFields, ok := msgFieldMap[typeName]
	if !ok {
		return nil, fmt.Errorf("unknown message type %s for aggregate option", ext.GetTypeName())
	}

	// Encode each field of the aggregate, then sort by field number (C++ protoc order)
	seenFields := map[string]bool{}
	type encodedEntry struct {
		fieldNum int32
		data     []byte
	}
	var entries []encodedEntry
	for _, af := range aggFields {
		var subField *descriptorpb.FieldDescriptorProto
		if af.IsExtension {
			// Look up extension field by FQN in the extendee's extensions
			if exts, ok := extByExtendee[typeName]; ok {
				subField = exts[af.Name]
			}
			if subField == nil {
				return nil, fmt.Errorf("unknown field %q in message %s", af.Name, typeName)
			}
		} else {
			var ok bool
			subField, ok = msgFields[af.Name]
			if !ok {
				return nil, fmt.Errorf("unknown field %q in message %s", af.Name, typeName)
			}
		}
		if subField.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			if seenFields[af.Name] {
				return nil, &aggregateDupFieldError{fieldName: af.Name}
			}
			seenFields[af.Name] = true
		}
		if len(af.SubFields) > 0 {
			// Nested message literal — recurse
			subBytes, err := encodeAggregateFields(subField, af.SubFields, msgFieldMap, enumValueNumbers, extByExtendee)
			if err != nil {
				return nil, err
			}
			entries = append(entries, encodedEntry{subField.GetNumber(), subBytes})
		} else {
			if af.Positive {
				return nil, &aggregatePositiveSignError{fieldType: subField.GetType()}
			}
			val := af.Value
			if af.Negative {
				val = "-" + val
			}
			encoded, err := encodeCustomOptionValue(subField, val, af.ValueType, enumValueNumbers)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", af.Name, err)
			}
			entries = append(entries, encodedEntry{subField.GetNumber(), encoded})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].fieldNum < entries[j].fieldNum
	})
	var inner []byte
	for _, e := range entries {
		inner = append(inner, e.data...)
	}

	fieldNum := protowire.Number(ext.GetNumber())
	var b []byte
	if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP {
		b = protowire.AppendTag(b, fieldNum, protowire.StartGroupType)
		b = append(b, inner...)
		b = protowire.AppendTag(b, fieldNum, protowire.EndGroupType)
	} else {
		b = protowire.AppendTag(b, fieldNum, protowire.BytesType)
		b = protowire.AppendBytes(b, inner)
	}
	return b, nil
}

// encodeAggregateFields encodes nested message literal sub-fields for a TYPE_MESSAGE field.
func encodeAggregateFields(field *descriptorpb.FieldDescriptorProto, aggFields []parser.AggregateField, msgFieldMap map[string]map[string]*descriptorpb.FieldDescriptorProto, enumValueNumbers map[string]map[string]int32, extByExtendee map[string]map[string]*descriptorpb.FieldDescriptorProto) ([]byte, error) {
	typeName := field.GetTypeName()
	if strings.HasPrefix(typeName, ".") {
		typeName = typeName[1:]
	}
	msgFields, ok := msgFieldMap[typeName]
	if !ok {
		return nil, fmt.Errorf("unknown message type %s", field.GetTypeName())
	}

	seenFields := map[string]bool{}
	type encodedEntry struct {
		fieldNum int32
		data     []byte
	}
	var entries []encodedEntry
	for _, af := range aggFields {
		// Any type URL expansion: [type.googleapis.com/pkg.MsgType]: { ... }
		if af.IsExtension && typeName == "google.protobuf.Any" && strings.Contains(af.Name, "/") {
			// Encode type_url (field 1)
			var anyBytes []byte
			anyBytes = protowire.AppendTag(anyBytes, 1, protowire.BytesType)
			anyBytes = protowire.AppendString(anyBytes, af.Name)
			// Extract message type from URL (part after last '/')
			slashIdx := strings.LastIndex(af.Name, "/")
			msgType := af.Name[slashIdx+1:]
			// Create a synthetic field descriptor to recurse into
			syntheticField := &descriptorpb.FieldDescriptorProto{
				Name:     proto.String("value"),
				Number:   proto.Int32(2),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String("." + msgType),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}
			valBytes, err := encodeAggregateFields(syntheticField, af.SubFields, msgFieldMap, enumValueNumbers, extByExtendee)
			if err != nil {
				return nil, err
			}
			anyBytes = append(anyBytes, valBytes...)
			// Any expansion uses field numbers 1 and 2, treat as field 1 for sorting
			entries = append(entries, encodedEntry{1, anyBytes})
			continue
		}
		var subField *descriptorpb.FieldDescriptorProto
		if af.IsExtension {
			if exts, ok := extByExtendee[typeName]; ok {
				subField = exts[af.Name]
			}
			if subField == nil {
				return nil, fmt.Errorf("unknown field %q in message %s", af.Name, typeName)
			}
		} else {
			var ok bool
			subField, ok = msgFields[af.Name]
			if !ok {
				return nil, fmt.Errorf("unknown field %q in message %s", af.Name, typeName)
			}
		}
		if subField.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			if seenFields[af.Name] {
				return nil, &aggregateDupFieldError{fieldName: af.Name}
			}
			seenFields[af.Name] = true
		}
		if len(af.SubFields) > 0 {
			subBytes, err := encodeAggregateFields(subField, af.SubFields, msgFieldMap, enumValueNumbers, extByExtendee)
			if err != nil {
				return nil, err
			}
			entries = append(entries, encodedEntry{subField.GetNumber(), subBytes})
		} else {
			if af.Positive {
				return nil, &aggregatePositiveSignError{fieldType: subField.GetType()}
			}
			val := af.Value
			if af.Negative {
				val = "-" + val
			}
			encoded, err := encodeCustomOptionValue(subField, val, af.ValueType, enumValueNumbers)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", af.Name, err)
			}
			entries = append(entries, encodedEntry{subField.GetNumber(), encoded})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].fieldNum < entries[j].fieldNum
	})
	var inner []byte
	for _, e := range entries {
		inner = append(inner, e.data...)
	}

	fieldNum := protowire.Number(field.GetNumber())
	var b []byte
	if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP {
		b = protowire.AppendTag(b, fieldNum, protowire.StartGroupType)
		b = append(b, inner...)
		b = protowire.AppendTag(b, fieldNum, protowire.EndGroupType)
	} else {
		b = protowire.AppendTag(b, fieldNum, protowire.BytesType)
		b = protowire.AppendBytes(b, inner)
	}
	return b, nil
}

type aggregateDupFieldError struct {
	fieldName string
}

func (e *aggregateDupFieldError) Error() string {
	return fmt.Sprintf("Non-repeated field \"%s\" is specified multiple times.", e.fieldName)
}

type aggregateBoolError struct {
	fieldName string
	value     string
}

func (e *aggregateBoolError) Error() string {
	return fmt.Sprintf("Invalid value for boolean field \"%s\". Value: \"%s\".", e.fieldName, e.value)
}

type aggregateIntRangeError struct {
	rawValue string
}

func (e *aggregateIntRangeError) Error() string {
	return fmt.Sprintf("Integer out of range (%s)", e.rawValue)
}

type aggregatePositiveSignError struct {
	fieldType descriptorpb.FieldDescriptorProto_Type
}

func (e *aggregatePositiveSignError) Error() string {
	switch e.fieldType {
	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT,
		descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		return "Expected double, got: +"
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return "Expected identifier, got: +"
	case descriptorpb.FieldDescriptorProto_TYPE_STRING,
		descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return "Expected string, got: +"
	default:
		return "Expected integer, got: +"
	}
}

type aggregateStringExpectedError struct {
	gotValue string
}

func (e *aggregateStringExpectedError) Error() string {
	return fmt.Sprintf("Expected string, got: %s", e.gotValue)
}

type aggregateFloatExpectedError struct {
	gotValue string
}

func (e *aggregateFloatExpectedError) Error() string {
	return fmt.Sprintf("Expected double, got: %s", e.gotValue)
}

type aggregateEnumError struct {
	enumValue string
	fieldName string
}

func (e *aggregateEnumError) Error() string {
	return fmt.Sprintf("Unknown enumeration value of \"%s\" for field \"%s\".", e.enumValue, e.fieldName)
}

// checkIntRangeOption validates that integer values fit in 32-bit types.
// Returns an error string if out of range, empty string if OK.
func checkIntRangeOption(ext *descriptorpb.FieldDescriptorProto, value string, negative bool, extFQN string) string {
	switch ext.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_INT32, descriptorpb.FieldDescriptorProto_TYPE_SINT32, descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		s := value
		if negative {
			s = "-" + s
		}
		v, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return ""
		}
		if v < math.MinInt32 || v > math.MaxInt32 {
			typeName := "int32"
			if ext.GetType() == descriptorpb.FieldDescriptorProto_TYPE_SINT32 {
				typeName = "sint32"
			}
			return fmt.Sprintf("Value out of range, %d to %d, for %s option \"%s\".",
				int64(math.MinInt32), int64(math.MaxInt32), typeName, extFQN)
		}
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32, descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		s := value
		if negative {
			s = "-" + s
		}
		v, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			uv, uerr := strconv.ParseUint(s, 0, 64)
			if uerr != nil {
				return ""
			}
			if uv > math.MaxUint32 {
				return fmt.Sprintf("Value out of range, 0 to %d, for uint32 option \"%s\".",
					uint64(math.MaxUint32), extFQN)
			}
			return ""
		}
		if v < 0 {
			return fmt.Sprintf("Value must be integer, from 0 to %d, for uint32 option \"%s\".",
				uint64(math.MaxUint32), extFQN)
		}
		if v > math.MaxUint32 {
			return fmt.Sprintf("Value out of range, 0 to %d, for uint32 option \"%s\".",
				uint64(math.MaxUint32), extFQN)
		}
	}
	return ""
}

func formatAggregateError(err error, filename string, braceTok tokenizer.Token, optName string) string {
	var dupErr *aggregateDupFieldError
	if errors.As(err, &dupErr) {
		return fmt.Sprintf("%s:%d:%d: Error while parsing option value for \"%s\": %s",
			filename, braceTok.Line+1, braceTok.Column+1, optName, dupErr.Error())
	}
	var boolErr *aggregateBoolError
	if errors.As(err, &boolErr) {
		return fmt.Sprintf("%s:%d:%d: Error while parsing option value for \"%s\": %s",
			filename, braceTok.Line+1, braceTok.Column+1, optName, boolErr.Error())
	}
	var posErr *aggregatePositiveSignError
	if errors.As(err, &posErr) {
		return fmt.Sprintf("%s:%d:%d: Error while parsing option value for \"%s\": %s",
			filename, braceTok.Line+1, braceTok.Column+1, optName, posErr.Error())
	}
	var intRangeErr *aggregateIntRangeError
	if errors.As(err, &intRangeErr) {
		return fmt.Sprintf("%s:%d:%d: Error while parsing option value for \"%s\": %s",
			filename, braceTok.Line+1, braceTok.Column+1, optName, intRangeErr.Error())
	}
	var strErr *aggregateStringExpectedError
	if errors.As(err, &strErr) {
		return fmt.Sprintf("%s:%d:%d: Error while parsing option value for \"%s\": %s",
			filename, braceTok.Line+1, braceTok.Column+1, optName, strErr.Error())
	}
	var floatErr *aggregateFloatExpectedError
	if errors.As(err, &floatErr) {
		return fmt.Sprintf("%s:%d:%d: Error while parsing option value for \"%s\": %s",
			filename, braceTok.Line+1, braceTok.Column+1, optName, floatErr.Error())
	}
	var enumErr *aggregateEnumError
	if errors.As(err, &enumErr) {
		return fmt.Sprintf("%s:%d:%d: Error while parsing option value for \"%s\": %s",
			filename, braceTok.Line+1, braceTok.Column+1, optName, enumErr.Error())
	}
	return fmt.Sprintf("%s: error encoding custom option: %v", filename, err)
}

func decodeRawProto(w *os.File, data []byte, indent int) error {
	for len(data) > 0 {
		num, wtype, n := protowire.ConsumeTag(data)
		if n < 0 || num < 1 {
			return fmt.Errorf("invalid tag")
		}
		data = data[n:]
		prefix := strings.Repeat("  ", indent)
		switch wtype {
		case protowire.VarintType:
			v, vn := protowire.ConsumeVarint(data)
			if vn < 0 {
				return fmt.Errorf("invalid varint")
			}
			data = data[vn:]
			fmt.Fprintf(w, "%s%d: %d\n", prefix, num, v)
		case protowire.Fixed64Type:
			v, vn := protowire.ConsumeFixed64(data)
			if vn < 0 {
				return fmt.Errorf("invalid fixed64")
			}
			data = data[vn:]
			fmt.Fprintf(w, "%s%d: 0x%016x\n", prefix, num, v)
		case protowire.Fixed32Type:
			v, vn := protowire.ConsumeFixed32(data)
			if vn < 0 {
				return fmt.Errorf("invalid fixed32")
			}
			data = data[vn:]
			fmt.Fprintf(w, "%s%d: 0x%08x\n", prefix, num, v)
		case protowire.BytesType:
			v, vn := protowire.ConsumeBytes(data)
			if vn < 0 {
				return fmt.Errorf("invalid bytes")
			}
			data = data[vn:]
			// Try to parse as sub-message first
			if tryErr := validateRawProto(v); tryErr == nil && len(v) > 0 {
				fmt.Fprintf(w, "%s%d {\n", prefix, num)
				decodeRawProto(w, v, indent+1)
				fmt.Fprintf(w, "%s}\n", prefix)
			} else {
				fmt.Fprintf(w, "%s%d: \"%s\"\n", prefix, num, cEscapeForDecode(v))
			}
		case protowire.StartGroupType:
			fmt.Fprintf(w, "%s%d {\n", prefix, num)
			for len(data) > 0 {
				peekNum, peekType, peekN := protowire.ConsumeTag(data)
				if peekN < 0 {
					return fmt.Errorf("invalid tag in group")
				}
				if peekType == protowire.EndGroupType && peekNum == num {
					data = data[peekN:]
					break
				}
				consumed, err := decodeRawField(w, data, indent+1)
				if err != nil {
					return err
				}
				data = data[consumed:]
			}
			fmt.Fprintf(w, "%s}\n", prefix)
		default:
			return fmt.Errorf("unknown wire type %d", wtype)
		}
	}
	return nil
}

func validateRawProto(data []byte) error {
	for len(data) > 0 {
		num, wtype, n := protowire.ConsumeTag(data)
		if n < 0 || num < 1 {
			return fmt.Errorf("invalid")
		}
		data = data[n:]
		switch wtype {
		case protowire.VarintType:
			_, vn := protowire.ConsumeVarint(data)
			if vn < 0 {
				return fmt.Errorf("invalid")
			}
			data = data[vn:]
		case protowire.Fixed64Type:
			if len(data) < 8 {
				return fmt.Errorf("invalid")
			}
			data = data[8:]
		case protowire.Fixed32Type:
			if len(data) < 4 {
				return fmt.Errorf("invalid")
			}
			data = data[4:]
		case protowire.BytesType:
			_, vn := protowire.ConsumeBytes(data)
			if vn < 0 {
				return fmt.Errorf("invalid")
			}
			data = data[vn:]
		case protowire.StartGroupType:
			// skip group contents
			for len(data) > 0 {
				gNum, gType, gn := protowire.ConsumeTag(data)
				if gn < 0 {
					return fmt.Errorf("invalid")
				}
				if gType == protowire.EndGroupType && gNum == num {
					data = data[gn:]
					break
				}
				return fmt.Errorf("group validation not implemented")
			}
		default:
			return fmt.Errorf("invalid")
		}
	}
	return nil
}

func decodeRawField(w *os.File, data []byte, indent int) (int, error) {
	num, wtype, n := protowire.ConsumeTag(data)
	if n < 0 || num < 1 {
		return 0, fmt.Errorf("invalid tag")
	}
	consumed := n
	rest := data[n:]
	prefix := strings.Repeat("  ", indent)
	switch wtype {
	case protowire.VarintType:
		v, vn := protowire.ConsumeVarint(rest)
		if vn < 0 {
			return 0, fmt.Errorf("invalid varint")
		}
		consumed += vn
		fmt.Fprintf(w, "%s%d: %d\n", prefix, num, v)
	case protowire.Fixed64Type:
		v, vn := protowire.ConsumeFixed64(rest)
		if vn < 0 {
			return 0, fmt.Errorf("invalid fixed64")
		}
		consumed += vn
		fmt.Fprintf(w, "%s%d: 0x%016x\n", prefix, num, v)
	case protowire.Fixed32Type:
		v, vn := protowire.ConsumeFixed32(rest)
		if vn < 0 {
			return 0, fmt.Errorf("invalid fixed32")
		}
		consumed += vn
		fmt.Fprintf(w, "%s%d: 0x%08x\n", prefix, num, v)
	case protowire.BytesType:
		v, vn := protowire.ConsumeBytes(rest)
		if vn < 0 {
			return 0, fmt.Errorf("invalid bytes")
		}
		consumed += vn
		if tryErr := validateRawProto(v); tryErr == nil && len(v) > 0 {
			fmt.Fprintf(w, "%s%d {\n", prefix, num)
			decodeRawProto(w, v, indent+1)
			fmt.Fprintf(w, "%s}\n", prefix)
		} else {
			fmt.Fprintf(w, "%s%d: \"%s\"\n", prefix, num, cEscapeForDecode(v))
		}
	default:
		return 0, fmt.Errorf("unknown wire type")
	}
	return consumed, nil
}

func cEscapeForDecode(data []byte) string {
	var sb strings.Builder
	for _, b := range data {
		switch b {
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		default:
			if b >= 0x20 && b < 0x7f {
				sb.WriteByte(b)
			} else {
				fmt.Fprintf(&sb, `\%03o`, b)
			}
		}
	}
	return sb.String()
}

// sortUnknownFields sorts raw protobuf unknown fields by field number (stable).
// C++ protoc emits extension/unknown fields in field-number order; we must match.
func sortUnknownFields(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	type entry struct {
		num  protowire.Number
		data []byte
	}
	var entries []entry
	r := raw
	for len(r) > 0 {
		num, _, n := protowire.ConsumeTag(r)
		if n < 0 {
			return raw
		}
		_, _, total := protowire.ConsumeField(r)
		if total < 0 {
			return raw
		}
		entries = append(entries, entry{num: num, data: append([]byte(nil), r[:total]...)})
		r = r[total:]
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].num < entries[j].num
	})
	var result []byte
	for _, e := range entries {
		result = append(result, e.data...)
	}
	return result
}

// sortOptionsUnknownFields sorts unknown fields on all Options messages in all
// parsed files, so that custom extension options appear in field-number order
// (matching C++ protoc behavior).
func sortOptionsUnknownFields(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) {
	for _, name := range orderedFiles {
		fd := parsed[name]
		sortFDOptionsUnknownFields(fd)
	}
}

// sortFDOptionsUnknownFields sorts unknown fields on all Options messages in fd.
func sortFDOptionsUnknownFields(fd *descriptorpb.FileDescriptorProto) {
	sortProtoUnknown(fd.GetOptions())
	for _, msg := range fd.GetMessageType() {
		sortMessageOptionsDeep(msg)
	}
	for _, e := range fd.GetEnumType() {
		sortEnumOptions(e)
	}
	for _, ext := range fd.GetExtension() {
		sortProtoUnknown(ext.GetOptions())
	}
	for _, svc := range fd.GetService() {
		sortProtoUnknown(svc.GetOptions())
		for _, m := range svc.GetMethod() {
			sortProtoUnknown(m.GetOptions())
		}
	}
}

// hasOptionsUnknowns returns true if any Options message in fd has unknown fields.
func hasOptionsUnknowns(fd *descriptorpb.FileDescriptorProto) bool {
	if hasUnknown(fd.GetOptions()) {
		return true
	}
	for _, msg := range fd.GetMessageType() {
		if hasMessageOptionsUnknowns(msg) {
			return true
		}
	}
	for _, e := range fd.GetEnumType() {
		if hasEnumOptionsUnknowns(e) {
			return true
		}
	}
	for _, ext := range fd.GetExtension() {
		if hasUnknown(ext.GetOptions()) {
			return true
		}
	}
	for _, svc := range fd.GetService() {
		if hasUnknown(svc.GetOptions()) {
			return true
		}
		for _, m := range svc.GetMethod() {
			if hasUnknown(m.GetOptions()) {
				return true
			}
		}
	}
	return false
}

func hasMessageOptionsUnknowns(msg *descriptorpb.DescriptorProto) bool {
	if hasUnknown(msg.GetOptions()) {
		return true
	}
	for _, f := range msg.GetField() {
		if hasUnknown(f.GetOptions()) {
			return true
		}
	}
	for _, o := range msg.GetOneofDecl() {
		if hasUnknown(o.GetOptions()) {
			return true
		}
	}
	for _, e := range msg.GetEnumType() {
		if hasEnumOptionsUnknowns(e) {
			return true
		}
	}
	for _, ext := range msg.GetExtension() {
		if hasUnknown(ext.GetOptions()) {
			return true
		}
	}
	for _, rng := range msg.GetExtensionRange() {
		if hasUnknown(rng.GetOptions()) {
			return true
		}
	}
	for _, nested := range msg.GetNestedType() {
		if hasMessageOptionsUnknowns(nested) {
			return true
		}
	}
	return false
}

func hasEnumOptionsUnknowns(e *descriptorpb.EnumDescriptorProto) bool {
	if hasUnknown(e.GetOptions()) {
		return true
	}
	for _, v := range e.GetValue() {
		if hasUnknown(v.GetOptions()) {
			return true
		}
	}
	return false
}

func hasUnknown(m proto.Message) bool {
	if m == nil {
		return false
	}
	return len(m.ProtoReflect().GetUnknown()) > 0
}

func sortMessageOptionsDeep(msg *descriptorpb.DescriptorProto) {
	sortProtoUnknown(msg.GetOptions())
	for _, f := range msg.GetField() {
		sortProtoUnknown(f.GetOptions())
	}
	for _, o := range msg.GetOneofDecl() {
		sortProtoUnknown(o.GetOptions())
	}
	for _, e := range msg.GetEnumType() {
		sortEnumOptions(e)
	}
	for _, ext := range msg.GetExtension() {
		sortProtoUnknown(ext.GetOptions())
	}
	for _, rng := range msg.GetExtensionRange() {
		sortProtoUnknown(rng.GetOptions())
	}
	for _, nested := range msg.GetNestedType() {
		sortMessageOptionsDeep(nested)
	}
}

func sortEnumOptions(e *descriptorpb.EnumDescriptorProto) {
	sortProtoUnknown(e.GetOptions())
	for _, v := range e.GetValue() {
		sortProtoUnknown(v.GetOptions())
	}
}

func sortProtoUnknown(m proto.Message) {
	if m == nil {
		return
	}
	ref := m.ProtoReflect()
	raw := ref.GetUnknown()
	if len(raw) == 0 {
		return
	}
	sorted := sortUnknownFields(raw)
	ref.SetUnknown(sorted)
}

