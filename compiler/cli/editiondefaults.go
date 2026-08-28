package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// The edition range --edition_defaults_out covers when the flags don't say.
const (
	defaultEditionDefaultsMinimum = descriptorpb.Edition_EDITION_PROTO2
	defaultEditionDefaultsMaximum = descriptorpb.Edition_EDITION_2026
)

// parseEditionFlag resolves an --edition_defaults_minimum/maximum value. Unlike
// an `edition = "X";` declaration, every Edition enum member is spellable here.
func parseEditionFlag(flag, value string) (descriptorpb.Edition, error) {
	v, ok := descriptorpb.Edition_value["EDITION_"+value]
	if !ok {
		return 0, fmt.Errorf("%s unknown edition %q.", flag, value)
	}
	return descriptorpb.Edition(v), nil
}

// featureField is one feature the defaults have to describe: a field of
// FeatureSet, or an extension of it contributed by a compiled file.
type featureField struct {
	number   int32
	field    *descriptorpb.FieldDescriptorProto
	defaults []*descriptorpb.FieldOptions_EditionDefault
	support  *descriptorpb.FieldOptions_FeatureSupport
}

// writeEditionDefaults renders the FeatureSetDefaults for the compiled pool —
// what every feature resolves to in each edition, and whether a .proto file may
// override it there. The features come from the pool rather than from this
// binary's own descriptor.proto, so the answer describes the descriptor.proto
// that was actually compiled.
func writeEditionDefaults(path string, minimum, maximum descriptorpb.Edition, orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) error {
	featureSet := findMessage(orderedFiles, parsed, "google.protobuf.FeatureSet")
	if featureSet == nil {
		return fmt.Errorf("%s: Could not find FeatureSet in descriptor pool.  Please make sure descriptor.proto is in your import path", path)
	}
	if minimum > maximum {
		return fmt.Errorf("%s: Invalid edition range, edition %s is newer than edition %s", path, editionName(minimum), editionName(maximum))
	}

	fields := collectFeatureFields(featureSet, orderedFiles, parsed)
	enums := collectEnumValues(orderedFiles, parsed)

	var out []byte
	for _, edition := range featureEditions(fields, maximum) {
		overridable, fixed, err := featureSetsFor(fields, enums, edition)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		var entry []byte
		entry = protowire.AppendTag(entry, 3, protowire.VarintType)
		entry = protowire.AppendVarint(entry, uint64(edition))
		entry = protowire.AppendTag(entry, 4, protowire.BytesType)
		entry = protowire.AppendBytes(entry, overridable)
		entry = protowire.AppendTag(entry, 5, protowire.BytesType)
		entry = protowire.AppendBytes(entry, fixed)

		out = protowire.AppendTag(out, 1, protowire.BytesType)
		out = protowire.AppendBytes(out, entry)
	}
	out = protowire.AppendTag(out, 4, protowire.VarintType)
	out = protowire.AppendVarint(out, uint64(minimum))
	out = protowire.AppendTag(out, 5, protowire.VarintType)
	out = protowire.AppendVarint(out, uint64(maximum))

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.Write(out); err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	return nil
}

// collectFeatureFields is FeatureSet's own fields plus every extension of it the
// pool defines, in field number order — the order they serialize in.
func collectFeatureFields(featureSet *descriptorpb.DescriptorProto, orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) []featureField {
	var fields []featureField
	add := func(f *descriptorpb.FieldDescriptorProto) {
		fields = append(fields, featureField{
			number:   f.GetNumber(),
			field:    f,
			defaults: f.GetOptions().GetEditionDefaults(),
			support:  f.GetOptions().GetFeatureSupport(),
		})
	}
	for _, f := range featureSet.GetField() {
		add(f)
	}
	forEachExtension(orderedFiles, parsed, ".google.protobuf.FeatureSet", add)
	sortFeatureFields(fields)
	return fields
}

// featureEditions is every edition the fields give a default for, up to the
// maximum. The minimum does not trim the list: a file compiled in the oldest
// edition still has to resolve against the pre-editions defaults.
func featureEditions(fields []featureField, maximum descriptorpb.Edition) []descriptorpb.Edition {
	seen := map[descriptorpb.Edition]bool{}
	var editions []descriptorpb.Edition
	for _, f := range fields {
		for _, d := range f.defaults {
			e := d.GetEdition()
			if e > maximum || seen[e] {
				continue
			}
			seen[e] = true
			editions = append(editions, e)
		}
	}
	sortEditions(editions)
	return editions
}

// featureSetsFor splits the features into the two FeatureSet messages one
// edition's defaults carry: those a .proto file may override in that edition,
// and those that are settled for it.
func featureSetsFor(fields []featureField, enums map[string]map[string]int32, edition descriptorpb.Edition) (overridable, fixed []byte, err error) {
	for _, f := range fields {
		def := latestDefault(f.defaults, edition)
		if def == nil {
			continue
		}
		encoded, err := encodeFeatureValue(f.field, enums, def.GetValue())
		if err != nil {
			return nil, nil, err
		}
		if isOverridable(f.support, edition) {
			overridable = append(overridable, encoded...)
		} else {
			fixed = append(fixed, encoded...)
		}
	}
	return overridable, fixed, nil
}

// latestDefault is the newest default at or before edition.
func latestDefault(defaults []*descriptorpb.FieldOptions_EditionDefault, edition descriptorpb.Edition) *descriptorpb.FieldOptions_EditionDefault {
	var best *descriptorpb.FieldOptions_EditionDefault
	for _, d := range defaults {
		if d.GetEdition() > edition {
			continue
		}
		if best == nil || d.GetEdition() > best.GetEdition() {
			best = d
		}
	}
	return best
}

// isOverridable reports whether a file in this edition may set the feature: it
// has to have been introduced by then and not yet removed.
func isOverridable(support *descriptorpb.FieldOptions_FeatureSupport, edition descriptorpb.Edition) bool {
	if support == nil {
		return true
	}
	if support.EditionIntroduced != nil && edition < support.GetEditionIntroduced() {
		return false
	}
	if support.EditionRemoved != nil && edition >= support.GetEditionRemoved() {
		return false
	}
	return true
}

// encodeFeatureValue encodes one default, whose value is written the way it
// would appear in text format.
func encodeFeatureValue(field *descriptorpb.FieldDescriptorProto, enums map[string]map[string]int32, value string) ([]byte, error) {
	num := protowire.Number(field.GetNumber())
	switch field.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		v, ok := enums[field.GetTypeName()][value]
		if !ok {
			return nil, fmt.Errorf("unknown value %q for enum %s", value, field.GetTypeName())
		}
		return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), uint64(int64(v))), nil
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		set := uint64(0)
		if value == "true" {
			set = 1
		}
		return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), set), nil
	case descriptorpb.FieldDescriptorProto_TYPE_INT32, descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32, descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value %q for field %s", value, field.GetName())
		}
		return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), uint64(n)), nil
	case descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), []byte(value)), nil
	}
	return nil, fmt.Errorf("feature %s has an unsupported type for an edition default", field.GetName())
}

// findMessage looks a top-level message up by fully-qualified name.
func findMessage(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, fqn string) *descriptorpb.DescriptorProto {
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := ""
		if pkg := fd.GetPackage(); pkg != "" {
			prefix = pkg + "."
		}
		for _, msg := range fd.GetMessageType() {
			if prefix+msg.GetName() == fqn {
				return msg
			}
		}
	}
	return nil
}

// forEachExtension visits every extension of extendee in the pool, wherever it
// was declared.
func forEachExtension(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto, extendee string, visit func(*descriptorpb.FieldDescriptorProto)) {
	var inMessages func(msgs []*descriptorpb.DescriptorProto)
	inMessages = func(msgs []*descriptorpb.DescriptorProto) {
		for _, msg := range msgs {
			for _, ext := range msg.GetExtension() {
				if ext.GetExtendee() == extendee {
					visit(ext)
				}
			}
			inMessages(msg.GetNestedType())
		}
	}
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			if ext.GetExtendee() == extendee {
				visit(ext)
			}
		}
		inMessages(fd.GetMessageType())
	}
}

// collectEnumValues maps each enum's fully-qualified name — spelled with the
// leading dot a field's type_name uses — to its value names.
func collectEnumValues(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) map[string]map[string]int32 {
	enums := map[string]map[string]int32{}
	record := func(fqn string, e *descriptorpb.EnumDescriptorProto) {
		values := map[string]int32{}
		for _, v := range e.GetValue() {
			values[v.GetName()] = v.GetNumber()
		}
		enums[fqn] = values
	}
	var inMessages func(msgs []*descriptorpb.DescriptorProto, prefix string)
	inMessages = func(msgs []*descriptorpb.DescriptorProto, prefix string) {
		for _, msg := range msgs {
			fqn := prefix + "." + msg.GetName()
			for _, e := range msg.GetEnumType() {
				record(fqn+"."+e.GetName(), e)
			}
			inMessages(msg.GetNestedType(), fqn)
		}
	}
	for _, name := range orderedFiles {
		fd := parsed[name]
		prefix := ""
		if pkg := fd.GetPackage(); pkg != "" {
			prefix = "." + pkg
		}
		for _, e := range fd.GetEnumType() {
			record(prefix+"."+e.GetName(), e)
		}
		inMessages(fd.GetMessageType(), prefix)
	}
	return enums
}

func sortFeatureFields(fields []featureField) {
	sort.Slice(fields, func(i, j int) bool { return fields[i].number < fields[j].number })
}

func sortEditions(editions []descriptorpb.Edition) {
	sort.Slice(editions, func(i, j int) bool { return editions[i] < editions[j] })
}
