// Package parser implements parsing of .proto files to FileDescriptorProtos.
// This mirrors C++ google::protobuf::compiler::Parser from compiler/parser.cc.
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wham/protoc-go/io/tokenizer"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

type parser struct {
	tok       *tokenizer.Tokenizer
	locations []*descriptorpb.SourceCodeInfo_Location
	lastLine  int
	lastCol   int
}

// ParseFile parses a .proto file and returns a FileDescriptorProto.
func ParseFile(filename string, content string) (*descriptorpb.FileDescriptorProto, error) {
	p := &parser{tok: tokenizer.New(content)}

	fd := &descriptorpb.FileDescriptorProto{
		Name: proto.String(filename),
	}

	// Record file-level span — will be updated at the end
	fileLocIdx := p.addLocationPlaceholder(nil)

	for p.tok.Peek().Type != tokenizer.TokenEOF {
		tok := p.tok.Peek()

		switch tok.Value {
		case "syntax":
			if err := p.parseSyntax(fd); err != nil {
				return nil, err
			}
		case "package":
			if err := p.parsePackage(fd); err != nil {
				return nil, err
			}
		case "import":
			if err := p.parseImport(fd); err != nil {
				return nil, err
			}
		case "message":
			msgIdx := int32(len(fd.MessageType))
			msg, err := p.parseMessage([]int32{4, msgIdx})
			if err != nil {
				return nil, err
			}
			fd.MessageType = append(fd.MessageType, msg)
		case "enum":
			enumIdx := int32(len(fd.EnumType))
			e, err := p.parseEnum([]int32{5, enumIdx})
			if err != nil {
				return nil, err
			}
			fd.EnumType = append(fd.EnumType, e)
		case "service":
			svcIdx := int32(len(fd.Service))
			svc, err := p.parseService([]int32{6, svcIdx})
			if err != nil {
				return nil, err
			}
			fd.Service = append(fd.Service, svc)
		case "option":
			if err := p.parseFileOption(fd); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("line %d:%d: unexpected token %q", tok.Line+1, tok.Column+1, tok.Value)
		}
	}

	// Update file-level span using last real token end position
	p.locations[fileLocIdx].Span = []int32{0, 0, int32(p.lastLine), int32(p.lastCol)}

	fd.SourceCodeInfo = &descriptorpb.SourceCodeInfo{
		Location: p.locations,
	}

	return fd, nil
}

func (p *parser) parseSyntax(fd *descriptorpb.FileDescriptorProto) error {
	startTok := p.tok.Next() // consume "syntax"
	if _, err := p.tok.Expect("="); err != nil {
		return err
	}
	valTok, err := p.tok.ExpectString()
	if err != nil {
		return err
	}
	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}

	fd.Syntax = proto.String(valTok.Value)
	p.trackEnd(endTok)
	// path [12] = syntax field in FileDescriptorProto
	p.addLocationSpan([]int32{12}, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	return nil
}

func (p *parser) parsePackage(fd *descriptorpb.FileDescriptorProto) error {
	startTok := p.tok.Next() // consume "package"
	nameTok := p.tok.Next()  // package name (may contain dots)
	name := nameTok.Value
	for p.tok.Peek().Value == "." {
		p.tok.Next() // consume "."
		part := p.tok.Next()
		name += "." + part.Value
	}
	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}

	fd.Package = proto.String(name)
	p.trackEnd(endTok)
	// path [2] = package field
	p.addLocationSpan([]int32{2}, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	return nil
}

func (p *parser) parseImport(fd *descriptorpb.FileDescriptorProto) error {
	p.tok.Next() // consume "import"

	// Check for "public" or "weak"
	if p.tok.Peek().Value == "public" {
		p.tok.Next()
	} else if p.tok.Peek().Value == "weak" {
		p.tok.Next()
	}

	pathTok, err := p.tok.ExpectString()
	if err != nil {
		return err
	}
	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)
	_ = pathTok

	fd.Dependency = append(fd.Dependency, pathTok.Value)
	return nil
}

func (p *parser) parseMessage(path []int32) (*descriptorpb.DescriptorProto, error) {
	startTok := p.tok.Next() // consume "message"
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, err
	}

	msg := &descriptorpb.DescriptorProto{
		Name: proto.String(nameTok.Value),
	}

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, err
	}

	// Add message declaration and name spans BEFORE child spans (matches C++ order)
	msgLocIdx := p.addLocationPlaceholder(path)
	p.addLocationSpan(append(copyPath(path), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	var fieldIdx, nestedMsgIdx, nestedEnumIdx, oneofIdx int32
	var reservedRangeIdx, reservedNameIdx int32

	for p.tok.Peek().Value != "}" {
		tok := p.tok.Peek()

		switch tok.Value {
		case "message":
			nested, err := p.parseMessage(append(copyPath(path), 3, nestedMsgIdx))
			if err != nil {
				return nil, err
			}
			msg.NestedType = append(msg.NestedType, nested)
			nestedMsgIdx++
		case "enum":
			e, err := p.parseEnum(append(copyPath(path), 4, nestedEnumIdx))
			if err != nil {
				return nil, err
			}
			msg.EnumType = append(msg.EnumType, e)
			nestedEnumIdx++
		case "oneof":
			fields, decl, err := p.parseOneof(path, oneofIdx, &fieldIdx)
			if err != nil {
				return nil, err
			}
			msg.OneofDecl = append(msg.OneofDecl, decl)
			msg.Field = append(msg.Field, fields...)
			oneofIdx++
		case "map":
			field, entry, err := p.parseMapField(path, fieldIdx, nestedMsgIdx)
			if err != nil {
				return nil, err
			}
			msg.Field = append(msg.Field, field)
			msg.NestedType = append(msg.NestedType, entry)
			fieldIdx++
			nestedMsgIdx++
		case "reserved":
			if err := p.parseMessageReserved(msg, path, &reservedRangeIdx, &reservedNameIdx); err != nil {
				return nil, err
			}
		case "option", "extensions":
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		default:
			field, err := p.parseField(append(copyPath(path), 2, fieldIdx))
			if err != nil {
				return nil, err
			}
			msg.Field = append(msg.Field, field)
			fieldIdx++
		}
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update message declaration span
	p.locations[msgLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	return msg, nil
}

func (p *parser) parseMessageReserved(msg *descriptorpb.DescriptorProto, msgPath []int32, rangeIdx, nameIdx *int32) error {
	startTok := p.tok.Next() // consume "reserved"

	// Determine if this is a name reservation (first token is a string) or range reservation
	if p.tok.Peek().Type == tokenizer.TokenString {
		// reserved "name1", "name2";
		stmtPath := append(copyPath(msgPath), 10) // field 10 = reserved_name
		for {
			nameTok, err := p.tok.ExpectString()
			if err != nil {
				return err
			}
			msg.ReservedName = append(msg.ReservedName, nameTok.Value)

			// Source code info for individual reserved name
			p.addLocationSpan(append(copyPath(stmtPath), *nameIdx),
				nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value)+2) // +2 for quotes
			*nameIdx++

			if p.tok.Peek().Value == "," {
				p.tok.Next()
			} else {
				break
			}
		}
		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)
		// Statement-level span
		p.addLocationSpan(stmtPath, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		// Move statement span before individual names
		stmtLoc := p.locations[len(p.locations)-1]
		copy(p.locations[len(p.locations)-int(*nameIdx):], p.locations[len(p.locations)-int(*nameIdx)-1:len(p.locations)-1])
		p.locations[len(p.locations)-int(*nameIdx)-1] = stmtLoc
	} else {
		// reserved 2, 15, 9 to 11;
		stmtPath := append(copyPath(msgPath), 9) // field 9 = reserved_range
		startCount := *rangeIdx
		for {
			numTok, err := p.tok.ExpectInt()
			if err != nil {
				return err
			}
			startNum, _ := strconv.ParseInt(numTok.Value, 0, 32)
			endNum := startNum + 1 // exclusive, single number
			endSpanLine, endSpanCol := numTok.Line, numTok.Column+len(numTok.Value)

			endNumLine, endNumCol, endNumLen := numTok.Line, numTok.Column, len(numTok.Value)
			if p.tok.Peek().Value == "to" {
				p.tok.Next()
				endNumTok, err := p.tok.ExpectInt()
				if err != nil {
					return err
				}
				e, _ := strconv.ParseInt(endNumTok.Value, 0, 32)
				endNum = e + 1 // exclusive
				endSpanLine = endNumTok.Line
				endSpanCol = endNumTok.Column + len(endNumTok.Value)
				endNumLine = endNumTok.Line
				endNumCol = endNumTok.Column
				endNumLen = len(endNumTok.Value)
			}

			msg.ReservedRange = append(msg.ReservedRange, &descriptorpb.DescriptorProto_ReservedRange{
				Start: proto.Int32(int32(startNum)),
				End:   proto.Int32(int32(endNum)),
			})

			rangePath := append(copyPath(stmtPath), *rangeIdx)
			// Range span covers from start number to end number
			p.addLocationSpan(rangePath, numTok.Line, numTok.Column, endSpanLine, endSpanCol)
			// Start field (1) — spans just the start number token
			p.addLocationSpan(append(copyPath(rangePath), 1), numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))
			// End field (2) — spans just the end number token
			p.addLocationSpan(append(copyPath(rangePath), 2), endNumLine, endNumCol, endNumLine, endNumCol+endNumLen)
			*rangeIdx++

			if p.tok.Peek().Value == "," {
				p.tok.Next()
			} else {
				break
			}
		}
		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)
		// Statement-level span
		p.addLocationSpan(stmtPath, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		// Move statement span before individual ranges
		count := int(*rangeIdx - startCount)
		stmtLoc := p.locations[len(p.locations)-1]
		copy(p.locations[len(p.locations)-count*3:], p.locations[len(p.locations)-count*3-1:len(p.locations)-1])
		p.locations[len(p.locations)-count*3-1] = stmtLoc
	}
	return nil
}

func (p *parser) parseField(path []int32) (*descriptorpb.FieldDescriptorProto, error) {
	field := &descriptorpb.FieldDescriptorProto{}
	startTok := p.tok.Peek()
	startLine := startTok.Line
	startCol := startTok.Column

	// Check for label (repeated/optional in proto3 is rarely explicit, but handle it)
	var labelTok *tokenizer.Token
	if startTok.Value == "repeated" {
		lt := p.tok.Next()
		labelTok = &lt
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	} else {
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	}

	// Type
	typeTok := p.tok.Next()
	typeStartLine, typeStartCol := typeTok.Line, typeTok.Column

	if builtinType, ok := builtinTypes[typeTok.Value]; ok {
		field.Type = builtinType.Enum()
	} else {
		// Message or enum reference
		typeName := typeTok.Value
		for p.tok.Peek().Value == "." {
			p.tok.Next()
			part := p.tok.Next()
			typeName += "." + part.Value
		}
		field.TypeName = proto.String(typeName)
	}

	// Name
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, err
	}
	field.Name = proto.String(nameTok.Value)
	field.JsonName = proto.String(tokenizer.ToJSONName(nameTok.Value))

	// = number
	if _, err := p.tok.Expect("="); err != nil {
		return nil, err
	}
	numTok, err := p.tok.ExpectInt()
	if err != nil {
		return nil, err
	}
	num, err := strconv.ParseInt(numTok.Value, 0, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid field number: %s", numTok.Value)
	}
	field.Number = proto.Int32(int32(num))

	// Optional field options [deprecated = true, etc.]
	if p.tok.Peek().Value == "[" {
		p.skipBracketedOptions()
	}

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return nil, err
	}
	p.trackEnd(endTok)

	// Source code info — field declaration span
	p.addLocationSpan(path, startLine, startCol, endTok.Line, endTok.Column+1)

	// Label span (only for 'repeated' in proto3)
	if labelTok != nil {
		p.addLocationSpan(append(copyPath(path), 4),
			labelTok.Line, labelTok.Column, labelTok.Line, labelTok.Column+len(labelTok.Value))
	}

	// Type span
	if field.TypeName != nil {
		// path [6] = type_name
		typeNameEnd := typeStartCol + len(field.GetTypeName())
		p.addLocationSpan(append(copyPath(path), 6),
			typeStartLine, typeStartCol, typeTok.Line, typeNameEnd)
	} else {
		// path [5] = type
		p.addLocationSpan(append(copyPath(path), 5),
			typeStartLine, typeStartCol, typeTok.Line, typeStartCol+len(typeTok.Value))
	}

	// Name span - path [1] = name
	p.addLocationSpan(append(copyPath(path), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Number span - path [3] = number
	p.addLocationSpan(append(copyPath(path), 3),
		numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))

	return field, nil
}

func (p *parser) parseEnum(path []int32) (*descriptorpb.EnumDescriptorProto, error) {
	startTok := p.tok.Next() // consume "enum"
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, err
	}

	e := &descriptorpb.EnumDescriptorProto{
		Name: proto.String(nameTok.Value),
	}

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, err
	}

	// Add enum declaration and name spans BEFORE values (C++ order)
	enumLocIdx := p.addLocationPlaceholder(path)
	p.addLocationSpan(append(copyPath(path), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	var valueIdx int32
	for p.tok.Peek().Value != "}" {
		if p.tok.Peek().Value == "option" || p.tok.Peek().Value == "reserved" {
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
			continue
		}

		valNameTok, err := p.tok.ExpectIdent()
		if err != nil {
			return nil, err
		}
		if _, err := p.tok.Expect("="); err != nil {
			return nil, err
		}

		// Handle negative numbers
		negative := false
		if p.tok.Peek().Value == "-" {
			p.tok.Next()
			negative = true
		}

		valNumTok, err := p.tok.ExpectInt()
		if err != nil {
			return nil, err
		}

		// Optional options [deprecated = true]
		if p.tok.Peek().Value == "[" {
			p.skipBracketedOptions()
		}

		endValTok, err := p.tok.Expect(";")
		if err != nil {
			return nil, err
		}
		p.trackEnd(endValTok)

		num, _ := strconv.ParseInt(valNumTok.Value, 0, 32)
		if negative {
			num = -num
		}
		e.Value = append(e.Value, &descriptorpb.EnumValueDescriptorProto{
			Name:   proto.String(valNameTok.Value),
			Number: proto.Int32(int32(num)),
		})

		// Source code info for enum value
		valuePath := append(copyPath(path), 2, valueIdx)
		p.addLocationSpan(valuePath, valNameTok.Line, valNameTok.Column, endValTok.Line, endValTok.Column+1)
		// Value name - path [1]
		p.addLocationSpan(append(copyPath(valuePath), 1),
			valNameTok.Line, valNameTok.Column, valNameTok.Line, valNameTok.Column+len(valNameTok.Value))
		// Value number - path [2]
		p.addLocationSpan(append(copyPath(valuePath), 2),
			valNumTok.Line, valNumTok.Column, valNumTok.Line, valNumTok.Column+len(valNumTok.Value))

		valueIdx++
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update enum declaration span
	p.locations[enumLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	return e, nil
}

func (p *parser) parseService(path []int32) (*descriptorpb.ServiceDescriptorProto, error) {
	startTok := p.tok.Next() // consume "service"
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, err
	}

	svc := &descriptorpb.ServiceDescriptorProto{
		Name: proto.String(nameTok.Value),
	}

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, err
	}

	// Add service declaration and name spans BEFORE methods (C++ order)
	svcLocIdx := p.addLocationPlaceholder(path)
	p.addLocationSpan(append(copyPath(path), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	var methodIdx int32
	for p.tok.Peek().Value != "}" {
		if p.tok.Peek().Value == "option" {
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
			continue
		}
		method, err := p.parseMethod(append(copyPath(path), 2, methodIdx))
		if err != nil {
			return nil, err
		}
		svc.Method = append(svc.Method, method)
		methodIdx++
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update service declaration span
	p.locations[svcLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	return svc, nil
}

func (p *parser) parseMethod(path []int32) (*descriptorpb.MethodDescriptorProto, error) {
	startTok := p.tok.Next() // consume "rpc"
	if startTok.Value != "rpc" {
		return nil, fmt.Errorf("line %d:%d: expected 'rpc', got %q", startTok.Line+1, startTok.Column+1, startTok.Value)
	}

	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, err
	}

	if _, err := p.tok.Expect("("); err != nil {
		return nil, err
	}

	// Input type - may be "stream" qualified
	var clientStreaming bool
	var clientStreamTok tokenizer.Token
	if p.tok.Peek().Value == "stream" {
		clientStreamTok = p.tok.Next()
		clientStreaming = true
	}
	inputTok := p.tok.Next()
	inputType := inputTok.Value
	for p.tok.Peek().Value == "." {
		p.tok.Next()
		part := p.tok.Next()
		inputType += "." + part.Value
	}
	inputEndCol := inputTok.Column + len(inputType)

	if _, err := p.tok.Expect(")"); err != nil {
		return nil, err
	}

	if _, err := p.tok.Expect("returns"); err != nil {
		return nil, err
	}

	if _, err := p.tok.Expect("("); err != nil {
		return nil, err
	}

	// Output type
	var serverStreaming bool
	var serverStreamTok tokenizer.Token
	if p.tok.Peek().Value == "stream" {
		serverStreamTok = p.tok.Next()
		serverStreaming = true
	}
	outputTok := p.tok.Next()
	outputType := outputTok.Value
	for p.tok.Peek().Value == "." {
		p.tok.Next()
		part := p.tok.Next()
		outputType += "." + part.Value
	}
	outputEndCol := outputTok.Column + len(outputType)

	if _, err := p.tok.Expect(")"); err != nil {
		return nil, err
	}

	// Method might end with ; or { ... }
	var endTok tokenizer.Token
	if p.tok.Peek().Value == "{" {
		p.tok.Next()
		depth := 1
		for depth > 0 {
			t := p.tok.Next()
			if t.Value == "{" {
				depth++
			} else if t.Value == "}" {
				depth--
				if depth == 0 {
					endTok = t
				}
			}
		}
	} else {
		endTok, err = p.tok.Expect(";")
		if err != nil {
			return nil, err
		}
	}
	p.trackEnd(endTok)

	method := &descriptorpb.MethodDescriptorProto{
		Name:       proto.String(nameTok.Value),
		InputType:  proto.String(inputType),
		OutputType: proto.String(outputType),
	}
	if clientStreaming {
		method.ClientStreaming = proto.Bool(true)
	}
	if serverStreaming {
		method.ServerStreaming = proto.Bool(true)
	}

	// Source code info — ordered by position in source
	p.addLocationSpan(path, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.addLocationSpan(append(copyPath(path), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))
	if clientStreaming {
		p.addLocationSpan(append(copyPath(path), 5),
			clientStreamTok.Line, clientStreamTok.Column, clientStreamTok.Line, clientStreamTok.Column+len("stream"))
	}
	p.addLocationSpan(append(copyPath(path), 2),
		inputTok.Line, inputTok.Column, inputTok.Line, inputEndCol)
	if serverStreaming {
		p.addLocationSpan(append(copyPath(path), 6),
			serverStreamTok.Line, serverStreamTok.Column, serverStreamTok.Line, serverStreamTok.Column+len("stream"))
	}
	p.addLocationSpan(append(copyPath(path), 3),
		outputTok.Line, outputTok.Column, outputTok.Line, outputEndCol)

	return method, nil
}

func (p *parser) parseOneof(msgPath []int32, oneofIdx int32, fieldIdx *int32) ([]*descriptorpb.FieldDescriptorProto, *descriptorpb.OneofDescriptorProto, error) {
	startTok := p.tok.Next() // consume "oneof"
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, nil, err
	}

	oneofPath := append(copyPath(msgPath), 8, oneofIdx)

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, nil, err
	}

	// Add oneof declaration and name spans BEFORE fields (C++ order)
	oneofLocIdx := p.addLocationPlaceholder(oneofPath)
	p.addLocationSpan(append(copyPath(oneofPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	var fields []*descriptorpb.FieldDescriptorProto
	for p.tok.Peek().Value != "}" {
		if p.tok.Peek().Value == "option" {
			if err := p.skipStatement(); err != nil {
				return nil, nil, err
			}
			continue
		}
		fieldPath := append(copyPath(msgPath), 2, *fieldIdx)
		field, err := p.parseField(fieldPath)
		if err != nil {
			return nil, nil, err
		}
		field.OneofIndex = proto.Int32(oneofIdx)
		fields = append(fields, field)
		*fieldIdx++
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update oneof declaration span
	p.locations[oneofLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	decl := &descriptorpb.OneofDescriptorProto{
		Name: proto.String(nameTok.Value),
	}

	return fields, decl, nil
}

func (p *parser) parseMapField(msgPath []int32, fieldIdx, nestedMsgIdx int32) (*descriptorpb.FieldDescriptorProto, *descriptorpb.DescriptorProto, error) {
	fieldPath := append(copyPath(msgPath), 2, fieldIdx)

	mapTok := p.tok.Next() // consume "map"
	startLine, startCol := mapTok.Line, mapTok.Column

	if _, err := p.tok.Expect("<"); err != nil {
		return nil, nil, err
	}

	keyTypeTok := p.tok.Next()
	keyType, ok := builtinTypes[keyTypeTok.Value]
	if !ok {
		return nil, nil, fmt.Errorf("invalid map key type: %s", keyTypeTok.Value)
	}

	if _, err := p.tok.Expect(","); err != nil {
		return nil, nil, err
	}

	valTypeTok := p.tok.Next()
	var valType descriptorpb.FieldDescriptorProto_Type
	var valTypeName string
	if bt, ok := builtinTypes[valTypeTok.Value]; ok {
		valType = bt
	} else {
		valTypeName = valTypeTok.Value
		for p.tok.Peek().Value == "." {
			p.tok.Next()
			part := p.tok.Next()
			valTypeName += "." + part.Value
		}
		valType = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	}

	gtTok, err := p.tok.Expect(">")
	if err != nil {
		return nil, nil, err
	}
	typeNameEndCol := gtTok.Column + 1

	// Field name
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, nil, err
	}

	if _, err := p.tok.Expect("="); err != nil {
		return nil, nil, err
	}

	numTok, err := p.tok.ExpectInt()
	if err != nil {
		return nil, nil, err
	}
	num, _ := strconv.ParseInt(numTok.Value, 0, 32)

	if p.tok.Peek().Value == "[" {
		p.skipBracketedOptions()
	}

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return nil, nil, err
	}
	p.trackEnd(endTok)

	// Build entry type name
	entryName := toCamelCase(nameTok.Value) + "Entry"

	// Create synthetic entry message
	entry := &descriptorpb.DescriptorProto{
		Name: proto.String(entryName),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("key"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     keyType.Enum(),
				JsonName: proto.String("key"),
			},
			{
				Name:     proto.String("value"),
				Number:   proto.Int32(2),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				JsonName: proto.String("value"),
			},
		},
		Options: &descriptorpb.MessageOptions{
			MapEntry: proto.Bool(true),
		},
	}

	if valTypeName != "" {
		entry.Field[1].Type = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
		entry.Field[1].TypeName = proto.String(valTypeName)
	} else {
		entry.Field[1].Type = valType.Enum()
	}

	// The field itself references the entry type
	field := &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(nameTok.Value),
		Number:   proto.Int32(int32(num)),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(entryName), // will be resolved later
		JsonName: proto.String(tokenizer.ToJSONName(nameTok.Value)),
	}

	// Source code info
	p.addLocationSpan(fieldPath, startLine, startCol, endTok.Line, endTok.Column+1)
	p.addLocationSpan(append(copyPath(fieldPath), 6),
		startLine, startCol, mapTok.Line, typeNameEndCol)
	p.addLocationSpan(append(copyPath(fieldPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))
	p.addLocationSpan(append(copyPath(fieldPath), 3),
		numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))

	return field, entry, nil
}

func (p *parser) parseFileOption(fd *descriptorpb.FileDescriptorProto) error {
	return p.skipStatement()
}

func (p *parser) skipStatement() error {
	depth := 0
	for {
		tok := p.tok.Next()
		if tok.Type == tokenizer.TokenEOF {
			return fmt.Errorf("unexpected EOF")
		}
		p.trackEnd(tok)
		if tok.Value == "{" {
			depth++
		} else if tok.Value == "}" {
			if depth > 0 {
				depth--
			} else {
				return nil
			}
		} else if tok.Value == ";" && depth == 0 {
			return nil
		}
	}
}

func (p *parser) skipBracketedOptions() {
	depth := 1
	p.tok.Next() // consume "["
	for depth > 0 {
		tok := p.tok.Next()
		if tok.Value == "[" {
			depth++
		} else if tok.Value == "]" {
			depth--
		}
	}
}

func (p *parser) addLocationPlaceholder(path []int32) int {
	pathCopy := make([]int32, len(path))
	copy(pathCopy, path)
	idx := len(p.locations)
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: pathCopy,
	})
	return idx
}

func (p *parser) addLocationSpan(path []int32, startLine, startCol, endLine, endCol int) {
	pathCopy := make([]int32, len(path))
	copy(pathCopy, path)
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: pathCopy,
		Span: multiSpan(startLine, startCol, endLine, endCol),
	})
}

func (p *parser) trackEnd(tok tokenizer.Token) {
	endLine := tok.Line
	endCol := tok.Column + len(tok.Value)
	if endLine > p.lastLine || (endLine == p.lastLine && endCol > p.lastCol) {
		p.lastLine = endLine
		p.lastCol = endCol
	}
}

func multiSpan(startLine, startCol, endLine, endCol int) []int32 {
	if startLine == endLine {
		return []int32{int32(startLine), int32(startCol), int32(endCol)}
	}
	return []int32{int32(startLine), int32(startCol), int32(endLine), int32(endCol)}
}

func copyPath(path []int32) []int32 {
	c := make([]int32, len(path))
	copy(c, path)
	return c
}

// toCamelCase converts a snake_case name to CamelCase for map entry types.
func toCamelCase(name string) string {
	var result strings.Builder
	upper := true
	for _, r := range name {
		if r == '_' {
			upper = true
			continue
		}
		if upper {
			result.WriteRune(rune(strings.ToUpper(string(r))[0]))
			upper = false
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

var builtinTypes = map[string]descriptorpb.FieldDescriptorProto_Type{
	"double":   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
	"float":    descriptorpb.FieldDescriptorProto_TYPE_FLOAT,
	"int64":    descriptorpb.FieldDescriptorProto_TYPE_INT64,
	"uint64":   descriptorpb.FieldDescriptorProto_TYPE_UINT64,
	"int32":    descriptorpb.FieldDescriptorProto_TYPE_INT32,
	"fixed64":  descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
	"fixed32":  descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
	"bool":     descriptorpb.FieldDescriptorProto_TYPE_BOOL,
	"string":   descriptorpb.FieldDescriptorProto_TYPE_STRING,
	"bytes":    descriptorpb.FieldDescriptorProto_TYPE_BYTES,
	"uint32":   descriptorpb.FieldDescriptorProto_TYPE_UINT32,
	"sfixed32": descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
	"sfixed64": descriptorpb.FieldDescriptorProto_TYPE_SFIXED64,
	"sint32":   descriptorpb.FieldDescriptorProto_TYPE_SINT32,
	"sint64":   descriptorpb.FieldDescriptorProto_TYPE_SINT64,
}

// ResolveTypes resolves type references in the file descriptor.
func ResolveTypes(fd *descriptorpb.FileDescriptorProto) {
	pkg := fd.GetPackage()
	prefix := ""
	if pkg != "" {
		prefix = "." + pkg
	}

	types := map[string]descriptorpb.FieldDescriptorProto_Type{}
	collectTypes(fd.GetMessageType(), prefix, types)
	for _, e := range fd.GetEnumType() {
		types[prefix+"."+e.GetName()] = descriptorpb.FieldDescriptorProto_TYPE_ENUM
	}

	resolveMessageFields(fd.GetMessageType(), prefix, types)

	for _, svc := range fd.GetService() {
		for _, m := range svc.GetMethod() {
			m.InputType = proto.String(resolveTypeName(m.GetInputType(), prefix, types))
			m.OutputType = proto.String(resolveTypeName(m.GetOutputType(), prefix, types))
		}
	}
}

func collectTypes(msgs []*descriptorpb.DescriptorProto, prefix string, types map[string]descriptorpb.FieldDescriptorProto_Type) {
	for _, msg := range msgs {
		fullName := prefix + "." + msg.GetName()
		types[fullName] = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
		for _, e := range msg.GetEnumType() {
			types[fullName+"."+e.GetName()] = descriptorpb.FieldDescriptorProto_TYPE_ENUM
		}
		collectTypes(msg.GetNestedType(), fullName, types)
	}
}

func resolveMessageFields(msgs []*descriptorpb.DescriptorProto, prefix string, types map[string]descriptorpb.FieldDescriptorProto_Type) {
	for _, msg := range msgs {
		msgPrefix := prefix + "." + msg.GetName()
		for _, f := range msg.GetField() {
			if f.TypeName != nil {
				resolved := resolveTypeName(f.GetTypeName(), msgPrefix, types)
				f.TypeName = proto.String(resolved)
				if tp, ok := types[resolved]; ok {
					f.Type = tp.Enum()
				}
			}
		}
		resolveMessageFields(msg.GetNestedType(), msgPrefix, types)
	}
}

func resolveTypeName(name string, scope string, types map[string]descriptorpb.FieldDescriptorProto_Type) string {
	if strings.HasPrefix(name, ".") {
		return name
	}

	s := scope
	for s != "" {
		candidate := s + "." + name
		if _, ok := types[candidate]; ok {
			return candidate
		}
		lastDot := strings.LastIndex(s, ".")
		if lastDot < 0 {
			break
		}
		s = s[:lastDot]
	}

	candidate := "." + name
	if _, ok := types[candidate]; ok {
		return candidate
	}

	return "." + name
}
