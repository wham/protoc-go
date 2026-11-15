package cli

import (
	"strings"
	"testing"
)

// The hash screen in front of the duplicate-name check may only ever say
// "nothing here" when that is true, so every shape of duplicate the exact
// check knows has to get past it.
func TestDuplicateNameScreenLetsEveryDuplicateThrough(t *testing.T) {
	cases := map[string]map[string]string{
		"top-level messages":       {"a.proto": `syntax = "proto3"; message M {} message M {}`},
		"message and enum":         {"a.proto": `syntax = "proto3"; package p; message M {} enum M { Z = 0; }`},
		"fields":                   {"a.proto": `syntax = "proto3"; message M { int32 f = 1; string f = 2; }`},
		"field and oneof":          {"a.proto": `syntax = "proto3"; message M { int32 f = 1; oneof f { int32 g = 2; } }`},
		"field and nested message": {"a.proto": `syntax = "proto3"; package p.q; message M { message N {} int32 N = 1; }`},
		"nested messages":          {"a.proto": `syntax = "proto3"; message M { message N { message O {} message O {} } }`},
		"sibling enum values":      {"a.proto": `syntax = "proto3"; enum A { X = 0; } enum B { X = 0; }`},
		"nested sibling enum values": {
			"a.proto": `syntax = "proto3"; package p; message M { enum A { X = 0; } enum B { X = 0; } }`,
		},
		"enum value and message": {"a.proto": `syntax = "proto3"; package p; enum A { M = 0; } message M {}`},
		"methods":                {"a.proto": `syntax = "proto3"; message M {} service S { rpc R(M) returns (M); rpc R(M) returns (M); }`},
		"service and message":    {"a.proto": `syntax = "proto3"; package p; message S {} service S {}`},
		"extensions": {
			"a.proto": `syntax = "proto2"; message M { extensions 1 to 10; } extend M { optional int32 e = 1; } extend M { optional int32 e = 2; }`,
		},
		"across files": {
			"a.proto": `syntax = "proto3"; package p; import "b.proto"; message M {}`,
			"b.proto": `syntax = "proto3"; package p; message M {}`,
		},
		"across files without a package": {
			"a.proto": `syntax = "proto3"; import "b.proto"; message M {}`,
			"b.proto": `syntax = "proto3"; message M {}`,
		},
		// The same scope "p.M", reached as a message's members in one file
		// and as a package's top level in the other.
		"package scope against message scope": {
			"a.proto": `syntax = "proto3"; package p; import "b.proto"; message M { int32 x = 1; }`,
			"b.proto": `syntax = "proto3"; package p.M; message x {}`,
		},
	}
	for name, files := range cases {
		_, err := Compile(&CompileRequest{ProtoFiles: []string{"a.proto"}, Overlay: files})
		if err == nil || !strings.Contains(err.Error(), "already defined") {
			t.Errorf("%s: want an \"already defined\" error, got %v", name, err)
		}
	}
}
