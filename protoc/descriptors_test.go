package protoc_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/wham/protoc-go/protoc"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

// descriptorCorpus compiles every fixture under testdata that compiles cleanly
// into a set with imports and source info, the shape a descriptor set handed
// to WithDescriptors usually has. Fixtures that are meant to fail are skipped.
func descriptorCorpus(t testing.TB) map[string][]*descriptorpb.FileDescriptorProto {
	t.Helper()
	dirs, err := filepath.Glob("../testdata/[0-9]*")
	if err != nil {
		t.Fatal(err)
	}
	corpus := map[string][]*descriptorpb.FileDescriptorProto{}
	for _, dir := range dirs {
		sources, _ := filepath.Glob(filepath.Join(dir, "*.proto"))
		if len(sources) == 0 {
			continue
		}
		for i, s := range sources {
			sources[i] = filepath.Base(s)
		}
		res, err := protoc.New(
			protoc.WithProtoPaths(dir),
			protoc.WithIncludeImports(),
			protoc.WithIncludeSourceInfo(),
		).Compile(sources...)
		if err != nil {
			continue
		}
		corpus[filepath.Base(dir)] = res.Files
	}
	if len(corpus) < 100 {
		t.Fatalf("only %d fixtures compiled; corpus looks broken", len(corpus))
	}
	return corpus
}

func fileNames(files []*descriptorpb.FileDescriptorProto) []string {
	names := make([]string, len(files))
	for i, fd := range files {
		names[i] = fd.GetName()
	}
	return names
}

func cloneFiles(files []*descriptorpb.FileDescriptorProto) []*descriptorpb.FileDescriptorProto {
	out := make([]*descriptorpb.FileDescriptorProto, len(files))
	for i, fd := range files {
		out[i] = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	}
	return out
}

func assertUnchanged(t *testing.T, label string, got, want []*descriptorpb.FileDescriptorProto) {
	t.Helper()
	for i := range want {
		if !proto.Equal(got[i], want[i]) {
			t.Errorf("%s: provided descriptor %s was modified", label, want[i].GetName())
		}
	}
}

func writeSet(t *testing.T, files []*descriptorpb.FileDescriptorProto) string {
	t.Helper()
	data, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: files})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "set.binpb")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// requestOf runs an in-process plugin over result and returns the request it
// received.
func requestOf(t *testing.T, result *protoc.Result) *pluginpb.CodeGeneratorRequest {
	t.Helper()
	var req *pluginpb.CodeGeneratorRequest
	_, err := result.RunLibraryPlugin(protoc.PluginFunc(func(r *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
		req = proto.Clone(r).(*pluginpb.CodeGeneratorRequest)
		return &pluginpb.CodeGeneratorResponse{}, nil
	}), "p")
	if err != nil {
		t.Fatalf("RunLibraryPlugin: %v", err)
	}
	return req
}

// TestDescriptorsMatchDescriptorSetIn compiles every fixture's set once from
// memory and once from a file, and requires the same result, the same plugin
// request, and provided descriptors left exactly as they were.
func TestDescriptorsMatchDescriptorSetIn(t *testing.T) {
	corpus := descriptorCorpus(t)
	variants := []struct {
		name string
		opts []protoc.Option
	}{
		{"default", nil},
		{"include_imports", []protoc.Option{protoc.WithIncludeImports()}},
		{"source_info", []protoc.Option{protoc.WithIncludeSourceInfo(), protoc.WithIncludeImports()}},
		{"retain_options", []protoc.Option{protoc.WithRetainOptions()}},
		{"descriptors_only", []protoc.Option{protoc.WithDescriptorsOnly()}},
	}
	for name, files := range corpus {
		before := cloneFiles(files)
		setPath := writeSet(t, before)
		names := fileNames(files)
		for _, v := range variants {
			mem, err := protoc.New(append([]protoc.Option{protoc.WithDescriptors(files...)}, v.opts...)...).Compile(names...)
			if err != nil {
				t.Errorf("%s/%s: WithDescriptors: %v", name, v.name, err)
				continue
			}
			disk, err := protoc.New(append([]protoc.Option{protoc.WithDescriptorSetIn(setPath)}, v.opts...)...).Compile(names...)
			if err != nil {
				t.Errorf("%s/%s: WithDescriptorSetIn: %v", name, v.name, err)
				continue
			}
			if !proto.Equal(mem.AsFileDescriptorSet(), disk.AsFileDescriptorSet()) {
				t.Errorf("%s/%s: Result.Files differ", name, v.name)
			}
			if !proto.Equal(requestOf(t, mem), requestOf(t, disk)) {
				t.Errorf("%s/%s: plugin requests differ", name, v.name)
			}
			assertUnchanged(t, name+"/"+v.name, files, before)
		}
	}
}

// TestDescriptorsConcurrent shares one set of descriptors across concurrent
// compiles and plugin runs; under -race any write to them is reported.
func TestDescriptorsConcurrent(t *testing.T) {
	corpus := descriptorCorpus(t)
	keys := make([]string, 0, len(corpus))
	for k := range corpus {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	before := map[string][]*descriptorpb.FileDescriptorProto{}
	for _, k := range keys {
		before[k] = cloneFiles(corpus[k])
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			opts := []protoc.Option{protoc.WithIncludeImports()}
			if g%2 == 1 {
				opts = append(opts, protoc.WithDescriptorsOnly())
			}
			for _, k := range keys {
				files := corpus[k]
				res, err := protoc.New(append(opts, protoc.WithDescriptors(files...))...).Compile(fileNames(files)...)
				if err != nil {
					t.Errorf("%s: %v", k, err)
					return
				}
				if _, err := res.RunLibraryPlugin(protoc.PluginFunc(func(*pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
					return &pluginpb.CodeGeneratorResponse{}, nil
				}), ""); err != nil {
					t.Errorf("%s: %v", k, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	for _, k := range keys {
		assertUnchanged(t, k, corpus[k], before[k])
	}
}

func hasLeadingComment(req *pluginpb.CodeGeneratorRequest, file, comment string) bool {
	for _, fd := range req.GetProtoFile() {
		if fd.GetName() != file {
			continue
		}
		for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
			if strings.Contains(loc.GetLeadingComments(), comment) {
				return true
			}
		}
	}
	return false
}

// TestDescriptorsOnlyKeepsProvidedSourceInfo checks that skipping source info
// never strips it from provided files, both on a clean first pass and when the
// first pass falls back to a full recompile.
func TestDescriptorsOnlyKeepsProvidedSourceInfo(t *testing.T) {
	provided, err := protoc.New(
		protoc.WithOverlay(map[string]string{"dep.proto": `syntax = "proto3";
package dep;
// Leading comment on Dep.
message Dep { string id = 1; }
`}),
		protoc.WithIncludeSourceInfo(),
	).Compile("dep.proto")
	if err != nil {
		t.Fatal(err)
	}
	files := provided.Files
	before := cloneFiles(files)

	cases := []struct {
		name    string
		overlay string
	}{
		{"clean", `syntax = "proto3";
package use;
import "dep.proto";
message Use { dep.Dep d = 1; }
`},
		// The json name clash is a warning the first pass cannot position, so
		// it sends the compile round again in full.
		{"recompile", `syntax = "proto2";
package use;
import "dep.proto";
message Use { optional dep.Dep d = 1; optional string foo_bar = 2; optional string fooBar = 3; }
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := protoc.New(
				protoc.WithDescriptors(files...),
				protoc.WithOverlay(map[string]string{"use.proto": tc.overlay}),
				protoc.WithDescriptorsOnly(),
			).Compile("dep.proto", "use.proto")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "recompile" && len(res.Warnings) == 0 {
				t.Fatal("expected a warning to force the full recompile")
			}
			assertUnchanged(t, tc.name, files, before)
			if files[0].GetSourceCodeInfo() == nil {
				t.Fatal("provided file lost its SourceCodeInfo")
			}
			if !hasLeadingComment(requestOf(t, res), "dep.proto", "Leading comment on Dep.") {
				t.Error("plugin request lost the provided file's comments")
			}
			assertUnchanged(t, tc.name+" after plugin", files, before)
		})
	}
}

const invalidProto = "this is not a proto file {\n"

func shadowSet(t *testing.T) []*descriptorpb.FileDescriptorProto {
	t.Helper()
	res, err := protoc.New(protoc.WithOverlay(map[string]string{
		"a.proto": `syntax = "proto3"; package fromset; message A {}`,
	})).Compile("a.proto")
	if err != nil {
		t.Fatal(err)
	}
	return res.Files
}

// TestProvidedFileNotReadFromSourceTree puts an unparsable a.proto where the
// source tree would find it; the set's a.proto must win without it being read.
func TestProvidedFileNotReadFromSourceTree(t *testing.T) {
	files := shadowSet(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.proto"), []byte(invalidProto), 0o644); err != nil {
		t.Fatal(err)
	}
	setPath := writeSet(t, files)

	check := func(t *testing.T, opts ...protoc.Option) {
		t.Helper()
		res, err := protoc.New(opts...).Compile("a.proto")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if len(res.Files) != 1 || res.Files[0].GetPackage() != "fromset" {
			t.Fatalf("expected the set's a.proto, got %v", res.Files)
		}
	}

	for _, src := range []struct {
		name string
		opt  protoc.Option
	}{
		{"descriptors", protoc.WithDescriptors(files...)},
		{"descriptor_set_in", protoc.WithDescriptorSetIn(setPath)},
	} {
		t.Run(src.name+"/proto_path", func(t *testing.T) {
			check(t, src.opt, protoc.WithProtoPaths(root))
		})
		t.Run(src.name+"/working_dir", func(t *testing.T) {
			wd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(wd)
			check(t, src.opt)
		})
	}
}

// TestUnprovidedImportResolves imports a provided file, a file on the proto
// path and a well-known type from one source; only the first comes from the set.
func TestUnprovidedImportResolves(t *testing.T) {
	files := shadowSet(t)
	root := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.proto", invalidProto)
	write("b.proto", `syntax = "proto3"; package b; message B {}`)
	write("c.proto", `syntax = "proto3";
package c;
import "a.proto";
import "b.proto";
import "google/protobuf/timestamp.proto";
message C { fromset.A a = 1; b.B b = 2; google.protobuf.Timestamp t = 3; }
`)
	before := cloneFiles(files)
	res, err := protoc.New(
		protoc.WithDescriptors(files...),
		protoc.WithProtoPaths(root),
		protoc.WithIncludeImports(),
	).Compile("c.proto")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fileNames(res.Files), ",")
	if want := "a.proto,b.proto,google/protobuf/timestamp.proto,c.proto"; got != want {
		t.Errorf("files = %s, want %s", got, want)
	}
	assertUnchanged(t, "c.proto", files, before)
}

// TestUnresolvedProvidedDescriptorIsCopied hands in a descriptor whose type
// references are not yet fully qualified, so linking it has to rewrite them;
// that must happen on a copy.
func TestUnresolvedProvidedDescriptorIsCopied(t *testing.T) {
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("raw.proto"),
		Package: proto.String("raw"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Inner")},
			{Name: proto.String("Outer"), Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     proto.String("inner"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				TypeName: proto.String("Inner"),
				JsonName: proto.String("inner"),
			}}},
		},
	}
	before := proto.Clone(fd).(*descriptorpb.FileDescriptorProto)

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := protoc.New(protoc.WithDescriptors(fd)).Compile("raw.proto")
			if err != nil {
				t.Error(err)
				return
			}
			f := res.Files[0].GetMessageType()[1].GetField()[0]
			if f.GetTypeName() != ".raw.Inner" || f.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
				t.Errorf("field not resolved: %v", f)
			}
		}()
	}
	wg.Wait()
	if !proto.Equal(fd, before) {
		t.Error("provided descriptor was modified")
	}
}
