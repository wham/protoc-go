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
	syntax    string // "proto2" or "proto3"
}

// ParseFile parses a .proto file and returns a FileDescriptorProto.
func ParseFile(filename string, content string) (*descriptorpb.FileDescriptorProto, error) {
	p := &parser{tok: tokenizer.New(content)}

	fd := &descriptorpb.FileDescriptorProto{
		Name: proto.String(filename),
	}

	// Record file-level span — will be updated at the end
	fileLocIdx := p.addLocationPlaceholder(nil)

	// Record first token position for file-level span (C++ starts at first non-comment token)
	firstTok := p.tok.Peek()
	fileStartLine := firstTok.Line
	fileStartCol := firstTok.Column

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

	// Update file-level span using first token start and last real token end
	p.locations[fileLocIdx].Span = multiSpan(fileStartLine, fileStartCol, p.lastLine, p.lastCol)

	fd.SourceCodeInfo = &descriptorpb.SourceCodeInfo{
		Location: p.locations,
	}

	return fd, nil
}

func (p *parser) parseSyntax(fd *descriptorpb.FileDescriptorProto) error {
	firstIdx := p.tok.CurrentIndex()
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

	// proto2 files omit the syntax field; proto3 sets it explicitly
	if valTok.Value != "proto2" {
		fd.Syntax = proto.String(valTok.Value)
	}
	p.syntax = valTok.Value
	p.trackEnd(endTok)
	// path [12] = syntax field in FileDescriptorProto
	p.addLocationSpan([]int32{12}, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.attachComments(len(p.locations)-1, firstIdx)

	return nil
}

func (p *parser) parsePackage(fd *descriptorpb.FileDescriptorProto) error {
	firstIdx := p.tok.CurrentIndex()
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
	p.attachComments(len(p.locations)-1, firstIdx)
	return nil
}

func (p *parser) parseImport(fd *descriptorpb.FileDescriptorProto) error {
	startTok := p.tok.Next() // consume "import"

	// Check for "public" or "weak"
	isPublic := false
	var publicTok tokenizer.Token
	if p.tok.Peek().Value == "public" {
		publicTok = p.tok.Next()
		isPublic = true
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

	depIdx := int32(len(fd.Dependency))
	fd.Dependency = append(fd.Dependency, pathTok.Value)

	// Source code info for the import statement: path [3, depIdx]
	p.addLocationSpan([]int32{3, depIdx}, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	if isPublic {
		pubIdx := int32(len(fd.PublicDependency))
		fd.PublicDependency = append(fd.PublicDependency, depIdx)
		// Source code info for public keyword: path [10, pubIdx]
		p.addLocationSpan([]int32{10, pubIdx}, publicTok.Line, publicTok.Column, publicTok.Line, publicTok.Column+len(publicTok.Value))
	}

	return nil
}

func (p *parser) parseMessage(path []int32) (*descriptorpb.DescriptorProto, error) {
	firstIdx := p.tok.CurrentIndex()
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
	p.attachComments(msgLocIdx, firstIdx)
	p.addLocationSpan(append(copyPath(path), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	var fieldIdx, nestedMsgIdx, nestedEnumIdx, oneofIdx int32
	var reservedRangeIdx, reservedNameIdx int32
	var extensionRangeIdx int32

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
		case "option":
			if err := p.parseMessageOption(msg, path); err != nil {
				return nil, err
			}
		case "extensions":
			if err := p.parseExtensionRange(msg, path, &extensionRangeIdx); err != nil {
				return nil, err
			}
		default:
			field, err := p.parseField(append(copyPath(path), 2, fieldIdx))
			if err != nil {
				return nil, err
			}
			if field.Proto3Optional != nil && *field.Proto3Optional {
				syntheticName := "_" + field.GetName()
				field.OneofIndex = proto.Int32(oneofIdx)
				msg.OneofDecl = append(msg.OneofDecl, &descriptorpb.OneofDescriptorProto{
					Name: proto.String(syntheticName),
				})
				oneofIdx++
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

func (p *parser) parseExtensionRange(msg *descriptorpb.DescriptorProto, msgPath []int32, rangeIdx *int32) error {
	startTok := p.tok.Next() // consume "extensions"
	stmtPath := append(copyPath(msgPath), 5) // field 5 = extension_range
	startCount := *rangeIdx

	for {
		numTok, err := p.tok.ExpectInt()
		if err != nil {
			return err
		}
		startNum, _ := strconv.ParseInt(numTok.Value, 0, 32)
		endNum := startNum + 1
		endSpanLine, endSpanCol := numTok.Line, numTok.Column+len(numTok.Value)
		endNumLine, endNumCol, endNumLen := numTok.Line, numTok.Column, len(numTok.Value)

		if p.tok.Peek().Value == "to" {
			p.tok.Next()
			if p.tok.Peek().Value == "max" {
				maxTok := p.tok.Next()
				endNum = 536870912 // kMaxRangeSentinel (2^29)
				endSpanLine = maxTok.Line
				endSpanCol = maxTok.Column + len(maxTok.Value)
				endNumLine = maxTok.Line
				endNumCol = maxTok.Column
				endNumLen = len(maxTok.Value)
			} else {
				endNumTok, err := p.tok.ExpectInt()
				if err != nil {
					return err
				}
				e, _ := strconv.ParseInt(endNumTok.Value, 0, 32)
				endNum = e + 1
				endSpanLine = endNumTok.Line
				endSpanCol = endNumTok.Column + len(endNumTok.Value)
				endNumLine = endNumTok.Line
				endNumCol = endNumTok.Column
				endNumLen = len(endNumTok.Value)
			}
		}

		msg.ExtensionRange = append(msg.ExtensionRange, &descriptorpb.DescriptorProto_ExtensionRange{
			Start: proto.Int32(int32(startNum)),
			End:   proto.Int32(int32(endNum)),
		})

		rangePath := append(copyPath(stmtPath), *rangeIdx)
		p.addLocationSpan(rangePath, numTok.Line, numTok.Column, endSpanLine, endSpanCol)
		p.addLocationSpan(append(copyPath(rangePath), 1), numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))
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
	p.addLocationSpan(stmtPath, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	count := int(*rangeIdx - startCount)
	stmtLoc := p.locations[len(p.locations)-1]
	copy(p.locations[len(p.locations)-count*3:], p.locations[len(p.locations)-count*3-1:len(p.locations)-1])
	p.locations[len(p.locations)-count*3-1] = stmtLoc

	return nil
}

func (p *parser) parseMessageOption(msg *descriptorpb.DescriptorProto, msgPath []int32) error {
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if _, err := p.tok.Expect("="); err != nil {
		return err
	}

	valTok := p.tok.Next()
	p.trackEnd(valTok)

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)

	if msg.Options == nil {
		msg.Options = &descriptorpb.MessageOptions{}
	}

	var fieldNum int32
	switch optName {
	case "deprecated":
		msg.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 3
	default:
		return nil
	}

	// Source code info: [msgPath..., 7] for options, [msgPath..., 7, fieldNum] for specific option
	optPath := append(copyPath(msgPath), 7)
	span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: optPath,
		Span: span,
	})
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: append(copyPath(optPath), fieldNum),
		Span: span,
	})

	return nil
}

func (p *parser) parseField(path []int32) (*descriptorpb.FieldDescriptorProto, error) {
	field := &descriptorpb.FieldDescriptorProto{}
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Peek()
	startLine := startTok.Line
	startCol := startTok.Column

	// Check for label (required/optional/repeated)
	var labelTok *tokenizer.Token
	switch startTok.Value {
	case "required":
		lt := p.tok.Next()
		labelTok = &lt
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum()
	case "optional":
		lt := p.tok.Next()
		labelTok = &lt
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
		if p.syntax == "proto3" {
			field.Proto3Optional = proto.Bool(true)
		}
	case "repeated":
		lt := p.tok.Next()
		labelTok = &lt
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	default:
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
	var optionLocs []*descriptorpb.SourceCodeInfo_Location
	if p.tok.Peek().Value == "[" {
		optionLocs = p.parseFieldOptions(field, path)
	}

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return nil, err
	}
	p.trackEnd(endTok)

	// Source code info — field declaration span
	p.addLocationSpan(path, startLine, startCol, endTok.Line, endTok.Column+1)
	fieldLocIdx := len(p.locations) - 1
	p.attachComments(fieldLocIdx, firstIdx)

	// Label span (for any explicit label keyword)
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

	// Option source code info (after number, matching C++ order)
	p.locations = append(p.locations, optionLocs...)

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
		if p.tok.Peek().Value == "option" {
			if err := p.parseEnumOption(e, path); err != nil {
				return nil, err
			}
			continue
		}
		if p.tok.Peek().Value == "reserved" {
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
		var minusTok *tokenizer.Token
		if p.tok.Peek().Value == "-" {
			mt := p.tok.Next()
			minusTok = &mt
			negative = true
		}

		valNumTok, err := p.tok.ExpectInt()
		if err != nil {
			return nil, err
		}

		// Optional options [deprecated = true]
		var enumValOpts *descriptorpb.EnumValueOptions
		var hasOpts bool
		var optsBracketStartLine, optsBracketStartCol, optsBracketEndCol int
		type enumValOptInfo struct {
			fieldNum             int32
			nameStartCol, endCol int
		}
		var parsedEnumValOpts []enumValOptInfo

		if p.tok.Peek().Value == "[" {
			bracketTok := p.tok.Next() // consume "["
			p.trackEnd(bracketTok)
			hasOpts = true
			optsBracketStartLine = bracketTok.Line
			optsBracketStartCol = bracketTok.Column

			for {
				optNameTok := p.tok.Next()
				p.trackEnd(optNameTok)
				optName := optNameTok.Value

				if _, err := p.tok.Expect("="); err != nil {
					return nil, err
				}

				optValTok := p.tok.Next()
				p.trackEnd(optValTok)

				if enumValOpts == nil {
					enumValOpts = &descriptorpb.EnumValueOptions{}
				}

				var fieldNum int32
				switch optName {
				case "deprecated":
					enumValOpts.Deprecated = proto.Bool(optValTok.Value == "true")
					fieldNum = 1
				}

				if fieldNum != 0 {
					endCol := optValTok.Column + len(optValTok.Value)
					parsedEnumValOpts = append(parsedEnumValOpts, enumValOptInfo{
						fieldNum:     fieldNum,
						nameStartCol: optNameTok.Column,
						endCol:       endCol,
					})
				}

				if p.tok.Peek().Value == "," {
					p.tok.Next() // consume ","
				} else {
					break
				}
			}

			closeBracket, err := p.tok.Expect("]")
			if err != nil {
				return nil, err
			}
			p.trackEnd(closeBracket)
			optsBracketEndCol = closeBracket.Column
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
		evd := &descriptorpb.EnumValueDescriptorProto{
			Name:   proto.String(valNameTok.Value),
			Number: proto.Int32(int32(num)),
		}
		if enumValOpts != nil {
			evd.Options = enumValOpts
		}
		e.Value = append(e.Value, evd)

		// Source code info for enum value
		valuePath := append(copyPath(path), 2, valueIdx)
		p.addLocationSpan(valuePath, valNameTok.Line, valNameTok.Column, endValTok.Line, endValTok.Column+1)
		// Value name - path [1]
		p.addLocationSpan(append(copyPath(valuePath), 1),
			valNameTok.Line, valNameTok.Column, valNameTok.Line, valNameTok.Column+len(valNameTok.Value))
		// Value number - path [2]
		numStartCol := valNumTok.Column
		if minusTok != nil {
			numStartCol = minusTok.Column
		}
		p.addLocationSpan(append(copyPath(valuePath), 2),
			valNumTok.Line, numStartCol, valNumTok.Line, valNumTok.Column+len(valNumTok.Value))

		// Source code info for enum value options
		if hasOpts {
			optPath := append(copyPath(valuePath), 3)
			p.addLocationSpan(optPath, optsBracketStartLine, optsBracketStartCol,
				optsBracketStartLine, optsBracketEndCol+1)
			for _, oi := range parsedEnumValOpts {
				p.addLocationSpan(append(copyPath(optPath), oi.fieldNum),
					optsBracketStartLine, oi.nameStartCol, optsBracketStartLine, oi.endCol)
			}
		}

		valueIdx++
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update enum declaration span
	p.locations[enumLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	return e, nil
}

func (p *parser) parseEnumOption(e *descriptorpb.EnumDescriptorProto, enumPath []int32) error {
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if _, err := p.tok.Expect("="); err != nil {
		return err
	}

	valTok := p.tok.Next()
	p.trackEnd(valTok)

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)

	if e.Options == nil {
		e.Options = &descriptorpb.EnumOptions{}
	}

	var fieldNum int32
	switch optName {
	case "allow_alias":
		e.Options.AllowAlias = proto.Bool(valTok.Value == "true")
		fieldNum = 2
	case "deprecated":
		e.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 3
	default:
		return nil
	}

	// Source code info: [enumPath..., 3] for options, [enumPath..., 3, fieldNum] for specific option
	optPath := append(copyPath(enumPath), 3)
	span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: optPath,
		Span: span,
	})
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: append(copyPath(optPath), fieldNum),
		Span: span,
	})

	return nil
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
			if err := p.parseServiceOption(svc, path); err != nil {
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

func (p *parser) parseServiceOption(svc *descriptorpb.ServiceDescriptorProto, svcPath []int32) error {
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if _, err := p.tok.Expect("="); err != nil {
		return err
	}

	valTok := p.tok.Next()
	p.trackEnd(valTok)

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)

	if svc.Options == nil {
		svc.Options = &descriptorpb.ServiceOptions{}
	}

	var fieldNum int32
	switch optName {
	case "deprecated":
		svc.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 33
	default:
		return nil
	}

	optPath := append(copyPath(svcPath), 3)
	span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: optPath,
		Span: span,
	})
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: append(copyPath(optPath), fieldNum),
		Span: span,
	})

	return nil
}

func (p *parser) parseMethodOption(method *descriptorpb.MethodDescriptorProto, methodPath []int32) error {
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if _, err := p.tok.Expect("="); err != nil {
		return err
	}

	valTok := p.tok.Next()
	p.trackEnd(valTok)

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)

	if method.Options == nil {
		method.Options = &descriptorpb.MethodOptions{}
	}

	var fieldNum int32
	switch optName {
	case "deprecated":
		method.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 33
	default:
		return nil
	}

	optPath := append(copyPath(methodPath), 4)
	span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: optPath,
		Span: span,
	})
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: append(copyPath(optPath), fieldNum),
		Span: span,
	})

	return nil
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

	// Source code info — ordered by position in source (before options)
	methodLocIdx := p.addLocationPlaceholder(path)
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

	var endTok tokenizer.Token
	if p.tok.Peek().Value == "{" {
		p.tok.Next()
		for p.tok.Peek().Value != "}" {
			if p.tok.Peek().Value == "option" {
				if err := p.parseMethodOption(method, path); err != nil {
					return nil, err
				}
			} else {
				if err := p.skipStatement(); err != nil {
					return nil, err
				}
			}
		}
		endTok = p.tok.Next() // consume "}"
	} else {
		endTok, err = p.tok.Expect(";")
		if err != nil {
			return nil, err
		}
	}
	p.trackEnd(endTok)

	// Update method declaration span
	p.locations[methodLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

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
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if _, err := p.tok.Expect("="); err != nil {
		return err
	}

	valTok := p.tok.Next()
	p.trackEnd(valTok)

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)

	if fd.Options == nil {
		fd.Options = &descriptorpb.FileOptions{}
	}

	// Map option name to FileOptions field number
	var fieldNum int32
	switch optName {
	case "java_package":
		fd.Options.JavaPackage = proto.String(valTok.Value)
		fieldNum = 1
	case "java_outer_classname":
		fd.Options.JavaOuterClassname = proto.String(valTok.Value)
		fieldNum = 8
	case "java_multiple_files":
		fd.Options.JavaMultipleFiles = proto.Bool(valTok.Value == "true")
		fieldNum = 10
	case "go_package":
		fd.Options.GoPackage = proto.String(valTok.Value)
		fieldNum = 11
	case "optimize_for":
		switch valTok.Value {
		case "SPEED":
			fd.Options.OptimizeFor = descriptorpb.FileOptions_SPEED.Enum()
		case "CODE_SIZE":
			fd.Options.OptimizeFor = descriptorpb.FileOptions_CODE_SIZE.Enum()
		case "LITE_RUNTIME":
			fd.Options.OptimizeFor = descriptorpb.FileOptions_LITE_RUNTIME.Enum()
		default:
			return fmt.Errorf("line %d:%d: unknown optimize_for value %q", valTok.Line+1, valTok.Column+1, valTok.Value)
		}
		fieldNum = 9
	case "cc_generic_services":
		fd.Options.CcGenericServices = proto.Bool(valTok.Value == "true")
		fieldNum = 16
	case "java_generic_services":
		fd.Options.JavaGenericServices = proto.Bool(valTok.Value == "true")
		fieldNum = 17
	case "py_generic_services":
		fd.Options.PyGenericServices = proto.Bool(valTok.Value == "true")
		fieldNum = 18
	case "deprecated":
		fd.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 23
	case "cc_enable_arenas":
		fd.Options.CcEnableArenas = proto.Bool(valTok.Value == "true")
		fieldNum = 31
	case "php_namespace":
		fd.Options.PhpNamespace = proto.String(valTok.Value)
		fieldNum = 41
	case "php_class_prefix":
		fd.Options.PhpClassPrefix = proto.String(valTok.Value)
		fieldNum = 40
	case "php_metadata_namespace":
		fd.Options.PhpMetadataNamespace = proto.String(valTok.Value)
		fieldNum = 44
	case "ruby_package":
		fd.Options.RubyPackage = proto.String(valTok.Value)
		fieldNum = 45
	case "objc_class_prefix":
		fd.Options.ObjcClassPrefix = proto.String(valTok.Value)
		fieldNum = 36
	case "csharp_namespace":
		fd.Options.CsharpNamespace = proto.String(valTok.Value)
		fieldNum = 37
	case "swift_prefix":
		fd.Options.SwiftPrefix = proto.String(valTok.Value)
		fieldNum = 39
	default:
		// Unknown option — store as uninterpreted option (skip for now)
		return nil
	}

	// Source code info: [8] for the option statement, [8, fieldNum] for the specific option
	span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: []int32{8},
		Span: span,
	})
	p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
		Path: []int32{8, fieldNum},
		Span: span,
	})

	return nil
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

// parseFieldOptions parses [option = value, ...] on a field declaration.
// Returns deferred source code info locations to be appended after field spans.
func (p *parser) parseFieldOptions(field *descriptorpb.FieldDescriptorProto, fieldPath []int32) []*descriptorpb.SourceCodeInfo_Location {
	bracketTok := p.tok.Next() // consume "["
	var optLocs []*descriptorpb.SourceCodeInfo_Location

	addLoc := func(path []int32, startLine, startCol, endLine, endCol int) {
		optLocs = append(optLocs, &descriptorpb.SourceCodeInfo_Location{
			Path: path,
			Span: multiSpan(startLine, startCol, endLine, endCol),
		})
	}

	for {
		optNameTok := p.tok.Next()
		optName := optNameTok.Value

		// Consume "="
		p.tok.Next()

		// Handle negative values for default
		negative := false
		if optName == "default" && p.tok.Peek().Value == "-" {
			p.tok.Next()
			negative = true
		}

		valTok := p.tok.Next()
		valEnd := valTok.Column + len(valTok.Value)
		if valTok.Type == tokenizer.TokenString {
			valEnd += 2 // account for quotes stripped by tokenizer
		}

		switch optName {
		case "default":
			defVal := valTok.Value
			if negative {
				defVal = "-" + defVal
			}
			field.DefaultValue = proto.String(defVal)
		case "json_name":
			field.JsonName = proto.String(valTok.Value)
		case "deprecated":
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.Deprecated = proto.Bool(valTok.Value == "true")
		case "packed":
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.Packed = proto.Bool(valTok.Value == "true")
		case "lazy":
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.Lazy = proto.Bool(valTok.Value == "true")
		case "jstype":
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			switch valTok.Value {
			case "JS_NORMAL":
				field.Options.Jstype = descriptorpb.FieldOptions_JS_NORMAL.Enum()
			case "JS_STRING":
				field.Options.Jstype = descriptorpb.FieldOptions_JS_STRING.Enum()
			case "JS_NUMBER":
				field.Options.Jstype = descriptorpb.FieldOptions_JS_NUMBER.Enum()
			}
		case "ctype":
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			switch valTok.Value {
			case "STRING":
				field.Options.Ctype = descriptorpb.FieldOptions_STRING.Enum()
			case "CORD":
				field.Options.Ctype = descriptorpb.FieldOptions_CORD.Enum()
			case "STRING_PIECE":
				field.Options.Ctype = descriptorpb.FieldOptions_STRING_PIECE.Enum()
			}
		}

		// Build source code info for this option
		switch optName {
		case "default":
			// default_value is field 7 of FieldDescriptorProto (not under options)
			addLoc(append(copyPath(fieldPath), 7),
				valTok.Line, valTok.Column, valTok.Line, valEnd)
		case "json_name":
			// json_name goes to path [10] (field 10 of FieldDescriptorProto)
			addLoc(append(copyPath(fieldPath), 10),
				optNameTok.Line, optNameTok.Column, valTok.Line, valEnd)
			addLoc(append(copyPath(fieldPath), 10),
				valTok.Line, valTok.Column, valTok.Line, valEnd)
		case "deprecated":
			addLoc(append(copyPath(fieldPath), 8, 3),
				optNameTok.Line, optNameTok.Column, valTok.Line, valEnd)
		case "packed":
			addLoc(append(copyPath(fieldPath), 8, 2),
				optNameTok.Line, optNameTok.Column, valTok.Line, valEnd)
		case "lazy":
			addLoc(append(copyPath(fieldPath), 8, 5),
				optNameTok.Line, optNameTok.Column, valTok.Line, valEnd)
		case "jstype":
			addLoc(append(copyPath(fieldPath), 8, 6),
				optNameTok.Line, optNameTok.Column, valTok.Line, valEnd)
		case "ctype":
			addLoc(append(copyPath(fieldPath), 8, 1),
				optNameTok.Line, optNameTok.Column, valTok.Line, valEnd)
		}

		// Check for comma (more options) or closing bracket
		next := p.tok.Peek()
		if next.Value == "," {
			p.tok.Next()
		} else if next.Value == "]" {
			break
		} else {
			break
		}
	}

	closeTok := p.tok.Next() // consume "]"

	// Build final result: options container first, then individual options
	var result []*descriptorpb.SourceCodeInfo_Location

	// Options container span [fieldPath..., 8]
	containerLoc := &descriptorpb.SourceCodeInfo_Location{
		Path: append(copyPath(fieldPath), 8),
		Span: multiSpan(bracketTok.Line, bracketTok.Column, closeTok.Line, closeTok.Column+1),
	}
	result = append(result, containerLoc)

	// Then individual option locations
	result = append(result, optLocs...)

	return result
}

func unquoteString(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
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

// attachComments attaches leading/trailing/detached comments to a location.
// locIdx: the index of the location in p.locations
// firstTokenIdx: the token index of the first token of the declaration (for leading/detached)
// The trailing comment comes from the next token after the declaration's terminator.
func (p *parser) attachComments(locIdx int, firstTokenIdx int) {
	if locIdx < 0 || locIdx >= len(p.locations) {
		return
	}
	loc := p.locations[locIdx]

	// Leading and detached comments from the first token of the declaration
	cd := p.tok.CommentsAt(firstTokenIdx)
	if cd.Leading != "" {
		loc.LeadingComments = proto.String(cd.Leading)
	}
	for _, d := range cd.Detached {
		loc.LeadingDetachedComments = append(loc.LeadingDetachedComments, d)
	}

	// Trailing comment: PrevTrailing of the NEXT token (after the terminator)
	nextIdx := p.tok.CurrentIndex()
	nextCd := p.tok.CommentsAt(nextIdx)
	if nextCd.PrevTrailing != "" {
		loc.TrailingComments = proto.String(nextCd.PrevTrailing)
	}
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
// allFiles maps filename to parsed FileDescriptorProto for cross-file resolution.
func ResolveTypes(fd *descriptorpb.FileDescriptorProto, allFiles map[string]*descriptorpb.FileDescriptorProto) {
	pkg := fd.GetPackage()
	prefix := ""
	if pkg != "" {
		prefix = "." + pkg
	}

	types := map[string]descriptorpb.FieldDescriptorProto_Type{}

	// Collect types from this file
	collectTypes(fd.GetMessageType(), prefix, types)
	for _, e := range fd.GetEnumType() {
		types[prefix+"."+e.GetName()] = descriptorpb.FieldDescriptorProto_TYPE_ENUM
	}

	// Collect types from imported files (direct dependencies)
	if allFiles != nil {
		for _, dep := range fd.GetDependency() {
			if depFd, ok := allFiles[dep]; ok {
				collectImportedTypes(depFd, types)
			}
		}
		// Collect types from transitively public-imported files
		visited := map[string]bool{fd.GetName(): true}
		for _, dep := range fd.GetDependency() {
			collectPublicImportTypes(dep, allFiles, types, visited)
		}
	}

	resolveMessageFields(fd.GetMessageType(), prefix, types)

	for _, svc := range fd.GetService() {
		for _, m := range svc.GetMethod() {
			m.InputType = proto.String(resolveTypeName(m.GetInputType(), prefix, types))
			m.OutputType = proto.String(resolveTypeName(m.GetOutputType(), prefix, types))
		}
	}
}

// collectImportedTypes collects all types defined in a file for import resolution.
func collectImportedTypes(fd *descriptorpb.FileDescriptorProto, types map[string]descriptorpb.FieldDescriptorProto_Type) {
	pkg := fd.GetPackage()
	prefix := ""
	if pkg != "" {
		prefix = "." + pkg
	}
	collectTypes(fd.GetMessageType(), prefix, types)
	for _, e := range fd.GetEnumType() {
		types[prefix+"."+e.GetName()] = descriptorpb.FieldDescriptorProto_TYPE_ENUM
	}
}

// collectPublicImportTypes transitively collects types from public imports.
func collectPublicImportTypes(filename string, allFiles map[string]*descriptorpb.FileDescriptorProto, types map[string]descriptorpb.FieldDescriptorProto_Type, visited map[string]bool) {
	if visited[filename] {
		return
	}
	visited[filename] = true

	fd, ok := allFiles[filename]
	if !ok {
		return
	}

	// For each public dependency, collect its types and recurse
	for _, pubIdx := range fd.GetPublicDependency() {
		deps := fd.GetDependency()
		if int(pubIdx) < len(deps) {
			pubDep := deps[pubIdx]
			if pubFd, ok := allFiles[pubDep]; ok {
				collectImportedTypes(pubFd, types)
			}
			collectPublicImportTypes(pubDep, allFiles, types, visited)
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
