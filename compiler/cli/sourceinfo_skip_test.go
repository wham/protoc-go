package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wham/protoc-go/compiler/importer"
	"github.com/wham/protoc-go/compiler/wellknown"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

type skipCase struct {
	name  string
	roots []string
	files []string
}

// skipCases is every numbered fixture compiled as a directory, plus every file
// of the vendored upstream corpus compiled alone, the way scripts/test does.
func skipCases(t *testing.T) []skipCase {
	var cases []skipCase
	dirs, _ := filepath.Glob("../../testdata/*")
	sort.Strings(dirs)
	for _, dir := range dirs {
		protos, _ := filepath.Glob(filepath.Join(dir, "*.proto"))
		if len(protos) == 0 {
			continue
		}
		c := skipCase{name: filepath.Base(dir), roots: []string{dir}}
		for _, p := range protos {
			c.files = append(c.files, filepath.Base(p))
		}
		cases = append(cases, c)
	}
	const corpus = "../../testdata/google"
	protos, _ := filepath.Glob(filepath.Join(corpus, "google/protobuf/*.proto"))
	for _, p := range protos {
		rel, _ := filepath.Rel(corpus, p)
		cases = append(cases, skipCase{name: "google/" + filepath.Base(p), roots: []string{corpus}, files: []string{filepath.ToSlash(rel)}})
	}
	if len(cases) < 100 {
		t.Fatalf("found only %d cases; testdata missing?", len(cases))
	}
	return cases
}

func (c skipCase) input() *compileInput {
	return &compileInput{
		relFiles: c.files,
		srcTree:  &importer.SourceTree{Roots: c.roots, FallbackFS: wellknown.ProtoFiles},
	}
}

// A pass without source info is trusted only when it comes back clean, so the
// property that keeps it honest is one-sided: whenever the full pass reports
// anything, the skipping pass must report something too — and when both are
// clean they must have built the same descriptors.
func TestSkippingSourceInfoNeverHidesADiagnostic(t *testing.T) {
	clean, flagged := 0, 0
	for _, c := range skipCases(t) {
		full, fullErr := compilePass(c.input(), false)
		skip, skipErr := compilePass(c.input(), true)

		fullWarned := false
		if fullErr == nil {
			// Unused imports are the one warning a skipping pass can
			// position; anything else it must flag.
			for _, w := range full.warnings {
				if !strings.HasSuffix(w, " is unused.") {
					fullWarned = true
				}
			}
		}
		if fullErr != nil || fullWarned {
			flagged++
			if skipErr == nil && !skip.needsSourceInfo {
				t.Errorf("%s: full pass reported a diagnostic (err=%v, warnings=%q) that the skipping pass missed", c.name, fullErr, full.warnings)
			}
			continue
		}
		if skipErr != nil || skip.needsSourceInfo {
			// Allowed: costs a second pass, changes no output.
			continue
		}
		clean++
		if !equalStrings(full.warnings, skip.warnings) {
			t.Errorf("%s: warnings differ\n full: %q\n skip: %q", c.name, full.warnings, skip.warnings)
		}
		if !equalStrings(full.orderedFiles, skip.orderedFiles) {
			t.Errorf("%s: file order differs", c.name)
			continue
		}
		for _, name := range full.orderedFiles {
			want := proto.Clone(full.parsed[name]).(*descriptorpb.FileDescriptorProto)
			want.SourceCodeInfo = nil
			got := skip.parsed[name]
			got = proto.Clone(got).(*descriptorpb.FileDescriptorProto)
			got.SourceCodeInfo = nil
			if !proto.Equal(want, got) {
				t.Errorf("%s: descriptor of %s differs without source info", c.name, name)
			}
			wantCanon := proto.Clone(full.canonical[name]).(*descriptorpb.FileDescriptorProto)
			gotCanon := proto.Clone(skip.canonical[name]).(*descriptorpb.FileDescriptorProto)
			wantCanon.SourceCodeInfo, gotCanon.SourceCodeInfo = nil, nil
			if !proto.Equal(wantCanon, gotCanon) {
				t.Errorf("%s: canonical descriptor of %s differs without source info", c.name, name)
			}
		}
	}
	if clean < 100 || flagged < 20 {
		t.Fatalf("too few cases exercised: %d clean, %d with diagnostics", clean, flagged)
	}
	t.Logf("%d clean cases matched, %d cases with diagnostics all flagged", clean, flagged)
}

// The public entry point hides the two-pass arrangement entirely: asking it to
// skip source info may change how the answer is reached, never the answer.
func TestSkipSourceInfoChangesNoOutput(t *testing.T) {
	for _, c := range skipCases(t) {
		for _, includeImports := range []bool{false, true} {
			base := CompileRequest{ProtoFiles: c.files, ProtoPaths: c.roots, IncludeImports: includeImports}
			skipReq := base
			skipReq.SkipSourceInfo = true
			want, wantErr := Compile(&base)
			got, gotErr := Compile(&skipReq)
			if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && wantErr.Error() != gotErr.Error()) {
				t.Errorf("%s: errors differ\n full: %v\n skip: %v", c.name, wantErr, gotErr)
				continue
			}
			if wantErr != nil {
				continue
			}
			if !equalStrings(want.Warnings, got.Warnings) {
				t.Errorf("%s: warnings differ\n full: %q\n skip: %q", c.name, want.Warnings, got.Warnings)
			}
			wantBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(want.AsFileDescriptorSet())
			gotBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(got.AsFileDescriptorSet())
			if string(wantBytes) != string(gotBytes) {
				t.Errorf("%s: descriptor sets differ (include_imports=%v)", c.name, includeImports)
			}
		}
	}
}

// Running a plugin on a result that skipped source info must hand the plugin
// exactly the request a full compile would have.
func TestPluginAfterSkippedSourceInfoGetsFullRequest(t *testing.T) {
	dir := "../../testdata/01_basic_message"
	if _, err := os.Stat(dir); err != nil {
		t.Skip(err)
	}
	protos, _ := filepath.Glob(filepath.Join(dir, "*.proto"))
	var files []string
	for _, p := range protos {
		files = append(files, filepath.Base(p))
	}
	capture := func(skip bool) []byte {
		res, err := Compile(&CompileRequest{ProtoFiles: files, ProtoPaths: []string{dir}, SkipSourceInfo: skip})
		if err != nil {
			t.Fatal(err)
		}
		var req []byte
		_, err = res.RunLibraryPlugin(requestCapture{&req}, "p")
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	if full, skipped := capture(false), capture(true); string(full) != string(skipped) {
		t.Errorf("plugin request differs after a compile that skipped source info")
	} else if len(full) == 0 {
		t.Errorf("empty plugin request")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type requestCapture struct{ out *[]byte }

func (c requestCapture) Generate(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	*c.out = b
	return &pluginpb.CodeGeneratorResponse{}, err
}
