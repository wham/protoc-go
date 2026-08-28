package cli

import (
	"sort"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// optionFields describes the pool as far as re-encoding an option value needs:
// each message's fields and the extensions of it by number, which of those
// numbers are extensions, and which messages use the MessageSet wire format.
type optionFields struct {
	byNumber   map[string]map[int32]*descriptorpb.FieldDescriptorProto
	extension  map[string]map[int32]bool
	messageSet map[string]bool
}

// The options messages whose extension fields protoc-go stores as unknown bytes.
const (
	fileOptionsFQN       = "google.protobuf.FileOptions"
	messageOptionsFQN    = "google.protobuf.MessageOptions"
	fieldOptionsFQN      = "google.protobuf.FieldOptions"
	oneofOptionsFQN      = "google.protobuf.OneofOptions"
	enumOptionsFQN       = "google.protobuf.EnumOptions"
	enumValueOptionsFQN  = "google.protobuf.EnumValueOptions"
	serviceOptionsFQN    = "google.protobuf.ServiceOptions"
	methodOptionsFQN     = "google.protobuf.MethodOptions"
	extRangeOptionsFQN   = "google.protobuf.ExtensionRangeOptions"
	featureSetOptionsFQN = "google.protobuf.FeatureSet"
)

// collectOptionFields indexes every message in the pool by fully-qualified name,
// and every extension under the message it extends.
func collectOptionFields(orderedFiles []string, parsed map[string]*descriptorpb.FileDescriptorProto) optionFields {
	fields := optionFields{
		byNumber:   map[string]map[int32]*descriptorpb.FieldDescriptorProto{},
		extension:  map[string]map[int32]bool{},
		messageSet: map[string]bool{},
	}
	add := func(owner string, f *descriptorpb.FieldDescriptorProto, isExtension bool) {
		if fields.byNumber[owner] == nil {
			fields.byNumber[owner] = map[int32]*descriptorpb.FieldDescriptorProto{}
		}
		fields.byNumber[owner][f.GetNumber()] = f
		if isExtension {
			if fields.extension[owner] == nil {
				fields.extension[owner] = map[int32]bool{}
			}
			fields.extension[owner][f.GetNumber()] = true
		}
	}
	var inMessages func(msgs []*descriptorpb.DescriptorProto, prefix string)
	inMessages = func(msgs []*descriptorpb.DescriptorProto, prefix string) {
		for _, msg := range msgs {
			fqn := msg.GetName()
			if prefix != "" {
				fqn = prefix + "." + msg.GetName()
			}
			if msg.GetOptions().GetMessageSetWireFormat() {
				fields.messageSet[fqn] = true
			}
			for _, f := range msg.GetField() {
				add(fqn, f, false)
			}
			for _, ext := range msg.GetExtension() {
				add(trimLeadingDot(ext.GetExtendee()), ext, true)
			}
			inMessages(msg.GetNestedType(), fqn)
		}
	}
	for _, name := range orderedFiles {
		fd := parsed[name]
		for _, ext := range fd.GetExtension() {
			add(trimLeadingDot(ext.GetExtendee()), ext, true)
		}
		inMessages(fd.GetMessageType(), fd.GetPackage())
	}
	return fields
}

func trimLeadingDot(s string) string {
	if len(s) > 0 && s[0] == '.' {
		return s[1:]
	}
	return s
}

// canonicalizeUnknown re-serializes encoded option fields the way C++ protoc
// emits them. protoc builds the option message and serializes it once, so its
// fields come out in field-number order and several statements that set
// different parts of one singular message field arrive merged. protoc-go
// appends a fragment per option statement instead, so the same information is
// in source order and in pieces; this puts it back into protoc's form.
//
// Merging needs the schema: two fragments of a singular message field are one
// value, but two entries of a repeated field are two, and must stay apart.
func canonicalizeUnknown(raw []byte, msgFQN string, fields optionFields) []byte {
	if len(raw) == 0 {
		return raw
	}
	type entry struct {
		num     protowire.Number
		wtyp    protowire.Type
		payload []byte // message payload, for BytesType only
		raw     []byte
	}
	var entries []entry
	buf := raw
	for len(buf) > 0 {
		num, wtyp, tagLen := protowire.ConsumeTag(buf)
		if tagLen < 0 {
			return raw
		}
		_, _, total := protowire.ConsumeField(buf)
		if total < 0 {
			return raw
		}
		e := entry{num: num, wtyp: wtyp, raw: buf[:total]}
		switch wtyp {
		case protowire.BytesType:
			payload, n := protowire.ConsumeBytes(buf[tagLen:])
			if n < 0 {
				return raw
			}
			e.payload = payload
		case protowire.StartGroupType:
			// The body sits between the start and end tags, which are the same
			// length because they carry the same field number.
			e.payload = buf[tagLen : total-tagLen]
		}
		entries = append(entries, e)
		buf = buf[total:]
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].num < entries[j].num })

	owner := fields.byNumber[msgFQN]
	messageSet := fields.messageSet[msgFQN]
	var out []byte
	for i := 0; i < len(entries); {
		e := entries[i]
		f := owner[int32(e.num)]
		isGroup := f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_GROUP
		submessage := f != nil && (isGroup || f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE)
		if e.payload == nil || !submessage {
			out = append(out, e.raw...)
			i++
			continue
		}
		sub := trimLeadingDot(f.GetTypeName())
		emit := func(payload []byte) {
			body := canonicalizeUnknown(payload, sub, fields)
			// An extension of a MessageSet is written as an item group holding
			// the extension number and the encoded message, not as a field.
			if messageSet && fields.extension[msgFQN][int32(e.num)] {
				out = protowire.AppendTag(out, 1, protowire.StartGroupType)
				out = protowire.AppendVarint(protowire.AppendTag(out, 2, protowire.VarintType), uint64(e.num))
				out = protowire.AppendBytes(protowire.AppendTag(out, 3, protowire.BytesType), body)
				out = protowire.AppendTag(out, 1, protowire.EndGroupType)
				return
			}
			if isGroup {
				out = protowire.AppendTag(out, e.num, protowire.StartGroupType)
				out = append(out, body...)
				out = protowire.AppendTag(out, e.num, protowire.EndGroupType)
				return
			}
			out = protowire.AppendTag(out, e.num, protowire.BytesType)
			out = protowire.AppendBytes(out, body)
		}
		if f.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			emit(e.payload)
			i++
			continue
		}
		var merged []byte
		j := i
		for ; j < len(entries) && entries[j].num == e.num; j++ {
			merged = append(merged, entries[j].payload...)
		}
		emit(merged)
		i = j
	}
	return out
}

// canonicalizeOptions rewrites one options message's encoded extension fields,
// and those of the FeatureSet inside it, which carries custom features.
func canonicalizeOptions(opts optionsWithFeatures, msgFQN string, fields optionFields) {
	if opts == nil {
		return
	}
	canonicalizeMessage(opts, msgFQN, fields)
	if fs := opts.GetFeatures(); fs != nil {
		canonicalizeMessage(fs, featureSetOptionsFQN, fields)
	}
}

func canonicalizeMessage(m proto.Message, msgFQN string, fields optionFields) {
	ref := m.ProtoReflect()
	if raw := ref.GetUnknown(); len(raw) > 0 {
		ref.SetUnknown(canonicalizeUnknown(raw, msgFQN, fields))
	}
}

// canonicalizeFDOptions rewrites every options message in fd.
func canonicalizeFDOptions(fd *descriptorpb.FileDescriptorProto, fields optionFields) {
	canonicalizeOptions(fd.GetOptions(), fileOptionsFQN, fields)
	for _, msg := range fd.GetMessageType() {
		canonicalizeMessageOptions(msg, fields)
	}
	for _, e := range fd.GetEnumType() {
		canonicalizeEnumOptions(e, fields)
	}
	for _, ext := range fd.GetExtension() {
		canonicalizeOptions(ext.GetOptions(), fieldOptionsFQN, fields)
	}
	for _, svc := range fd.GetService() {
		canonicalizeOptions(svc.GetOptions(), serviceOptionsFQN, fields)
		for _, m := range svc.GetMethod() {
			canonicalizeOptions(m.GetOptions(), methodOptionsFQN, fields)
		}
	}
}

func canonicalizeMessageOptions(msg *descriptorpb.DescriptorProto, fields optionFields) {
	canonicalizeOptions(msg.GetOptions(), messageOptionsFQN, fields)
	for _, f := range msg.GetField() {
		canonicalizeOptions(f.GetOptions(), fieldOptionsFQN, fields)
	}
	for _, o := range msg.GetOneofDecl() {
		canonicalizeOptions(o.GetOptions(), oneofOptionsFQN, fields)
	}
	for _, ext := range msg.GetExtension() {
		canonicalizeOptions(ext.GetOptions(), fieldOptionsFQN, fields)
	}
	for _, rng := range msg.GetExtensionRange() {
		canonicalizeOptions(rng.GetOptions(), extRangeOptionsFQN, fields)
	}
	for _, e := range msg.GetEnumType() {
		canonicalizeEnumOptions(e, fields)
	}
	for _, nested := range msg.GetNestedType() {
		canonicalizeMessageOptions(nested, fields)
	}
}

func canonicalizeEnumOptions(e *descriptorpb.EnumDescriptorProto, fields optionFields) {
	canonicalizeOptions(e.GetOptions(), enumOptionsFQN, fields)
	for _, v := range e.GetValue() {
		canonicalizeOptions(v.GetOptions(), enumValueOptionsFQN, fields)
	}
}
