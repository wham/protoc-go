// Package wellknown embeds the protobuf well-known type .proto files.
// These are bundled into the binary so that imports like
// "google/protobuf/timestamp.proto" resolve without needing the files on disk,
// mirroring how C++ protoc bundles them via compiled-in descriptors.
package wellknown

import "embed"

//go:embed google/protobuf/*.proto google/protobuf/compiler/*.proto
var ProtoFiles embed.FS
