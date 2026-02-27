// Package parser implements parsing of .proto files to FileDescriptorProtos.
// This mirrors C++ google::protobuf::compiler::Parser from compiler/parser.cc.
package parser

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/wham/protoc-go/io/tokenizer"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// errBreakOneof is a sentinel error used when angle bracket syntax in a custom
// oneof option causes the parser to exit the oneof block (matching C++ protoc
// error recovery behavior).
var errBreakOneof = errors.New("break oneof")

// MultiError is an error containing multiple pre-formatted error messages.
type MultiError struct {
	Errors []string
}

func (e *MultiError) Error() string {
	return strings.Join(e.Errors, "\n")
}

// invalidOctalError checks if a number token is an invalid octal literal (starts with 0,
// contains 8 or 9). Returns the error string with the position of the first invalid digit,
// or empty string if not an invalid octal.
func invalidOctalError(filename string, tok tokenizer.Token) string {
	v := tok.Value
	if len(v) < 2 || v[0] != '0' || v[1] == 'x' || v[1] == 'X' {
		return ""
	}
	for i := 1; i < len(v); i++ {
		if v[i] == '8' || v[i] == '9' {
			return fmt.Sprintf("%s:%d:%d: Numbers starting with leading zero must be in octal.", filename, tok.Line+1, tok.Column+1+i)
		}
	}
	return ""
}

// parseIntLenient wraps strconv.ParseInt but treats bare "0x"/"0X" as 0,
// matching C++ protoc's ParseInteger which returns 0 for hex prefix with no digits.
func parseIntLenient(s string, base int, bitSize int) (int64, error) {
	if s == "0x" || s == "0X" {
		return 0, nil
	}
	return strconv.ParseInt(s, base, bitSize)
}

type parser struct {
	tok       *tokenizer.Tokenizer
	locations []*descriptorpb.SourceCodeInfo_Location
	lastLine  int
	lastCol   int
	syntax              string // "proto2" or "proto3"
	syntaxParsed        bool
	hadNonSyntaxStmt    bool
	packageParsed       bool
	seenFileOptions     map[string]bool
	seenImports         map[string]bool
	filename     string
	errors       []string
	inOneof      bool
	explicitJsonNames   map[*descriptorpb.FieldDescriptorProto]bool
	customFileOptions   []CustomFileOption
	customFieldOptions    []CustomFieldOption
	customMessageOptions  []CustomMessageOption
	customServiceOptions  []CustomServiceOption
	customMethodOptions   []CustomMethodOption
	customEnumOptions     []CustomEnumOption
	customEnumValueOptions []CustomEnumValueOption
	customOneofOptions     []CustomOneofOption
	customExtRangeOptions  []CustomExtRangeOption
}

// ParseResult holds the result of parsing a .proto file.
// CustomFileOption represents a parenthesized custom file option that needs
// post-parse resolution against extension definitions.
type CustomFileOption struct {
	ParenName string          // e.g., "(my_file_option)"
	InnerName string          // e.g., "my_file_option"
	SubFieldPath []string     // e.g., ["inner", "name"] for option (my_opt).inner.name = ...
	Value     string          // the option value (scalar)
	ValueType tokenizer.TokenType // token type of value
	Negative  bool            // true if value was preceded by '-'
	SCIIndex  int             // index in SCI locations for [8, fieldNum] entry
	NameTok   tokenizer.Token // position of "(" for error reporting
	AggregateFields []AggregateField // non-nil for aggregate (message literal) values
	AggregateBraceTok tokenizer.Token // position of "{" for aggregate error reporting
}

// AggregateField is a key-value pair inside an aggregate option value.
type AggregateField struct {
	Name        string
	Value       string
	ValueType   tokenizer.TokenType
	Negative    bool               // true if value was preceded by '-'
	Positive    bool               // true if value was preceded by '+'
	SubFields   []AggregateField   // nested message literal fields
	IsExtension bool               // true if name was bracketed [ext.name]
}

// CustomFieldOption represents a parenthesized custom option on a field
// (e.g., [(my_ext) = "value"]) that needs post-parse resolution.
type CustomFieldOption struct {
	ParenName       string                              // e.g., "(my_ext)"
	InnerName       string                              // e.g., "my_ext"
	SubFieldPath    []string                            // e.g., ["name"] for [(ext).name = ...]
	Value           string                              // the option value
	ValueType       tokenizer.TokenType                 // token type of value
	Field           *descriptorpb.FieldDescriptorProto  // the field this option is on
	NameTok         tokenizer.Token                     // position of "(" for error reporting
	ValTok          tokenizer.Token                     // position of value for error reporting
	AggregateFields []AggregateField                    // non-nil for aggregate values
	AggregateBraceTok tokenizer.Token                   // position of "{" for aggregate error reporting
	Negative        bool                                // value preceded by '-'
	SCILoc          *descriptorpb.SourceCodeInfo_Location // SCI entry to update with resolved field number
}

// CustomMessageOption represents a parenthesized custom option on a message
// (e.g., option (my_msg_label) = "primary";) that needs post-parse resolution.
type CustomMessageOption struct {
	ParenName       string                              // e.g., "(my_msg_label)"
	InnerName       string                              // e.g., "my_msg_label"
	SubFieldPath    []string                            // e.g., ["label"] for option (my_opt).label = ...
	Value           string                              // the option value
	ValueType       tokenizer.TokenType                 // token type of value
	Message         *descriptorpb.DescriptorProto       // the message this option is on
	NameTok         tokenizer.Token                     // position of "(" for error reporting
	AggregateFields []AggregateField                    // non-nil for aggregate values
	AggregateBraceTok tokenizer.Token                   // position of "{" for aggregate error reporting
	Negative        bool                                // value preceded by '-'
	SCILoc          *descriptorpb.SourceCodeInfo_Location // SCI entry to update with resolved field number
}

// CustomServiceOption represents a parenthesized custom option on a service
// (e.g., option (service_label) = "primary";) that needs post-parse resolution.
type CustomServiceOption struct {
	ParenName       string
	InnerName       string
	SubFieldPath    []string
	Value           string
	ValueType       tokenizer.TokenType
	Service         *descriptorpb.ServiceDescriptorProto
	NameTok         tokenizer.Token
	AggregateFields []AggregateField
	AggregateBraceTok tokenizer.Token
	Negative        bool
	SCILoc          *descriptorpb.SourceCodeInfo_Location
}

// CustomMethodOption represents a parenthesized custom option on a method
// (e.g., option (auth_role) = "admin";) that needs post-parse resolution.
type CustomMethodOption struct {
	ParenName       string
	InnerName       string
	SubFieldPath    []string
	Value           string
	ValueType       tokenizer.TokenType
	Method          *descriptorpb.MethodDescriptorProto
	NameTok         tokenizer.Token
	AggregateFields []AggregateField
	AggregateBraceTok tokenizer.Token
	Negative        bool
	SCILoc          *descriptorpb.SourceCodeInfo_Location
}

// CustomEnumOption represents a parenthesized custom option on an enum
// (e.g., option (enum_label) = "status_tracker";) that needs post-parse resolution.
type CustomEnumOption struct {
	ParenName       string
	InnerName       string
	SubFieldPath    []string
	Value           string
	ValueType       tokenizer.TokenType
	Enum            *descriptorpb.EnumDescriptorProto
	NameTok         tokenizer.Token
	AggregateFields []AggregateField
	AggregateBraceTok tokenizer.Token
	Negative        bool
	SCILoc          *descriptorpb.SourceCodeInfo_Location
}

// CustomEnumValueOption represents a parenthesized custom option on an enum value
// (e.g., HIGH = 1 [(display_name) = "High Priority"]) that needs post-parse resolution.
type CustomEnumValueOption struct {
	ParenName       string
	InnerName       string
	Value           string
	ValueType       tokenizer.TokenType
	EnumValue       *descriptorpb.EnumValueDescriptorProto
	NameTok         tokenizer.Token
	AggregateFields []AggregateField
	AggregateBraceTok tokenizer.Token
	Negative        bool
	SCILoc          *descriptorpb.SourceCodeInfo_Location
	SubFieldPath    []string
}

// CustomOneofOption represents a parenthesized custom option on a oneof
// (e.g., option (oneof_label) = "primary";) that needs post-parse resolution.
type CustomOneofOption struct {
	ParenName       string
	InnerName       string
	Value           string
	ValueType       tokenizer.TokenType
	Oneof           *descriptorpb.OneofDescriptorProto
	NameTok         tokenizer.Token
	AggregateFields []AggregateField
	AggregateBraceTok tokenizer.Token
	Negative        bool
	SCILoc          *descriptorpb.SourceCodeInfo_Location
	SubFieldPath    []string
}

// CustomExtRangeOption represents a parenthesized custom option on an extension range
// (e.g., extensions 100 to 199 [(my_annotation) = "annotated"];) that needs post-parse resolution.
type CustomExtRangeOption struct {
	ParenName       string
	InnerName       string
	SubFieldPath    []string
	Value           string
	ValueType       tokenizer.TokenType
	Ranges          []*descriptorpb.DescriptorProto_ExtensionRange // ranges this option applies to
	NameTok         tokenizer.Token
	ValEndLine      int
	ValEndCol       int
	AggregateFields []AggregateField
	AggregateBraceTok tokenizer.Token
	Negative        bool
	SCILocs         []*descriptorpb.SourceCodeInfo_Location // one SCI entry per range
}

type ParseResult struct {
	FD                      *descriptorpb.FileDescriptorProto
	ExplicitJsonNames       map[*descriptorpb.FieldDescriptorProto]bool
	CustomFileOptions       []CustomFileOption
	CustomFieldOptions      []CustomFieldOption
	CustomMessageOptions    []CustomMessageOption
	CustomServiceOptions    []CustomServiceOption
	CustomMethodOptions     []CustomMethodOption
	CustomEnumOptions       []CustomEnumOption
	CustomEnumValueOptions  []CustomEnumValueOption
	CustomOneofOptions      []CustomOneofOption
	CustomExtRangeOptions   []CustomExtRangeOption
}

// ParseFile parses a .proto file and returns a ParseResult.
func ParseFile(filename string, content string) (*ParseResult, error) {
	p := &parser{tok: tokenizer.New(content), filename: filename, syntax: "proto2", seenFileOptions: map[string]bool{}, seenImports: map[string]bool{}, explicitJsonNames: map[*descriptorpb.FieldDescriptorProto]bool{}}

	// If the tokenizer has errors, we still parse to collect parser errors too
	// (C++ protoc interleaves tokenizer and parser errors)

	fd := &descriptorpb.FileDescriptorProto{
		Name: proto.String(filename),
	}

	// Record file-level span — will be updated at the end
	fileLocIdx := p.addLocationPlaceholder(nil)

	// Record first token position for file-level span (C++ starts at first non-comment token)
	firstTok := p.tok.Peek()
	fileStartLine := firstTok.Line
	fileStartCol := firstTok.Column

	var parseErr error
	for p.tok.Peek().Type != tokenizer.TokenEOF {
		tok := p.tok.Peek()

		// Track if any non-syntax/edition statement has been seen
		if tok.Value != "syntax" && tok.Value != "edition" && tok.Value != ";" {
			p.hadNonSyntaxStmt = true
		}

		switch tok.Value {
		case "syntax":
			if p.syntaxParsed || p.hadNonSyntaxStmt {
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected top-level statement (e.g. \"message\").", p.filename, tok.Line+1, tok.Column+1))
				// Skip until semicolon (error recovery)
				for p.tok.Peek().Type != tokenizer.TokenEOF && p.tok.Peek().Value != ";" {
					p.tok.Next()
				}
				if p.tok.Peek().Value == ";" {
					p.tok.Next()
				}
				continue
			}
			if err := p.parseSyntax(fd); err != nil {
				parseErr = err
				break
			}
		case "edition":
			if p.syntaxParsed || p.hadNonSyntaxStmt {
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected top-level statement (e.g. \"message\").", p.filename, tok.Line+1, tok.Column+1))
				for p.tok.Peek().Type != tokenizer.TokenEOF && p.tok.Peek().Value != ";" {
					p.tok.Next()
				}
				if p.tok.Peek().Value == ";" {
					p.tok.Next()
				}
				continue
			}
			if err := p.parseEdition(fd); err != nil {
				parseErr = err
				break
			}
		case "package":
			if err := p.parsePackage(fd); err != nil {
				parseErr = err
				break
			}
		case "import":
			if err := p.parseImport(fd); err != nil {
				parseErr = err
				break
			}
		case "message":
			msgIdx := int32(len(fd.MessageType))
			msg, err := p.parseMessage([]int32{4, msgIdx})
			if err != nil {
				parseErr = err
				break
			}
			fd.MessageType = append(fd.MessageType, msg)
		case "enum":
			enumIdx := int32(len(fd.EnumType))
			e, err := p.parseEnum([]int32{5, enumIdx})
			if err != nil {
				parseErr = err
				break
			}
			fd.EnumType = append(fd.EnumType, e)
		case "service":
			svcIdx := int32(len(fd.Service))
			svc, err := p.parseService([]int32{6, svcIdx})
			if err != nil {
				parseErr = err
				break
			}
			fd.Service = append(fd.Service, svc)
		case "option":
			if err := p.parseFileOption(fd); err != nil {
				parseErr = err
				break
			}
		case "extend":
			if err := p.parseExtend(fd); err != nil {
				parseErr = err
				break
			}
		case ";":
			// Empty statement — consume and continue (C++ protoc allows these)
			semi := p.tok.Next()
			p.trackEnd(semi)
		case "}":
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected top-level statement (e.g. \"message\").", p.filename, tok.Line+1, tok.Column+1))
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Unmatched \"}\".", p.filename, tok.Line+1, tok.Column+1))
			p.tok.Next()
		default:
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected top-level statement (e.g. \"message\").", p.filename, tok.Line+1, tok.Column+1))
			p.skipStatementCpp()
			continue
		}
		if parseErr != nil {
			break
		}
	}

	// If there are tokenizer errors, merge them with any parser errors
	if len(p.tok.Errors) > 0 {
		var allErrs []posError
		for _, te := range p.tok.Errors {
			msg := fmt.Sprintf("%s:%d:%d: %s", p.filename, te.Line+1, te.Column+1, te.Message)
			var notesMsgs []string
			for _, n := range te.Notes {
				notesMsgs = append(notesMsgs, fmt.Sprintf("%s:%d:%d: %s", p.filename, n.Line+1, n.Column+1, n.Message))
			}
			allErrs = append(allErrs, posError{te.Line, te.Column, msg, notesMsgs})
		}
		if parseErr != nil {
			// Parse the line:col from the error string
			errStr := parseErr.Error()
			eLine, eCol := parseErrorPos(errStr)
			allErrs = append(allErrs, posError{eLine, eCol, fmt.Sprintf("%s:%s", p.filename, errStr), nil})
		}
		for _, e := range p.errors {
			eLine, eCol := parseErrorPosFromFormatted(e, p.filename)
			allErrs = append(allErrs, posError{eLine, eCol, e, nil})
		}
		// Sort by position
		sortPosErrors(allErrs)
		var msgs []string
		for _, e := range allErrs {
			msgs = append(msgs, e.msg)
			msgs = append(msgs, e.notes...)
		}
		return nil, &MultiError{Errors: msgs}
	}

	if parseErr != nil {
		if len(p.errors) > 0 {
			// Merge p.errors with parseErr, sorted by position
			var allErrs []posError
			errStr := parseErr.Error()
			eLine, eCol := parseErrorPos(errStr)
			allErrs = append(allErrs, posError{eLine, eCol, fmt.Sprintf("%s:%s", p.filename, errStr), nil})
			for _, e := range p.errors {
				el, ec := parseErrorPosFromFormatted(e, p.filename)
				allErrs = append(allErrs, posError{el, ec, e, nil})
			}
			sortPosErrors(allErrs)
			var msgs []string
			for _, e := range allErrs {
				msgs = append(msgs, e.msg)
				msgs = append(msgs, e.notes...)
			}
			return nil, &MultiError{Errors: msgs}
		}
		return nil, parseErr
	}

	// Update file-level span using first token start and last real token end
	p.locations[fileLocIdx].Span = multiSpan(fileStartLine, fileStartCol, p.lastLine, p.lastCol)

	fd.SourceCodeInfo = &descriptorpb.SourceCodeInfo{
		Location: p.locations,
	}

	if len(p.errors) > 0 {
		return nil, &MultiError{Errors: p.errors}
	}

	return &ParseResult{FD: fd, ExplicitJsonNames: p.explicitJsonNames, CustomFileOptions: p.customFileOptions, CustomFieldOptions: p.customFieldOptions, CustomMessageOptions: p.customMessageOptions, CustomServiceOptions: p.customServiceOptions, CustomMethodOptions: p.customMethodOptions, CustomEnumOptions: p.customEnumOptions, CustomEnumValueOptions: p.customEnumValueOptions, CustomOneofOptions: p.customOneofOptions, CustomExtRangeOptions: p.customExtRangeOptions}, nil
}

// parseErrorPos extracts 0-based line/col from an error string formatted as "line:col: message"
func parseErrorPos(s string) (int, int) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) >= 2 {
		line, e1 := strconv.Atoi(parts[0])
		col, e2 := strconv.Atoi(parts[1])
		if e1 == nil && e2 == nil {
			return line - 1, col - 1
		}
	}
	return 0, 0
}

// parseErrorPosFromFormatted extracts 0-based line/col from "filename:line:col: message"
func parseErrorPosFromFormatted(s string, filename string) (int, int) {
	prefix := filename + ":"
	if strings.HasPrefix(s, prefix) {
		return parseErrorPos(s[len(prefix):])
	}
	return 0, 0
}

type posError struct {
	line, col int
	msg       string
	notes     []string
}

func sortPosErrors(errs []posError) {
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].line != errs[j].line {
			return errs[i].line < errs[j].line
		}
		return errs[i].col < errs[j].col
	})
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
	// Concatenate adjacent string tokens (C++ protoc allows this)
	for p.tok.Peek().Type == tokenizer.TokenString {
		next := p.tok.Next()
		p.trackEnd(next)
		valTok.Value += next.Value
	}
	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}

	if valTok.Value != "proto2" && valTok.Value != "proto3" {
		return fmt.Errorf("%d:%d: Unrecognized syntax identifier \"%s\".  This parser only recognizes \"proto2\" and \"proto3\".", valTok.Line+1, valTok.Column+1, valTok.Value)
	}

	// proto2 files omit the syntax field; proto3 sets it explicitly
	if valTok.Value != "proto2" {
		fd.Syntax = proto.String(valTok.Value)
	}
	p.syntax = valTok.Value
	p.syntaxParsed = true
	p.trackEnd(endTok)
	// path [12] = syntax field in FileDescriptorProto
	p.addLocationSpan([]int32{12}, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.attachComments(len(p.locations)-1, firstIdx)

	return nil
}

var editionMap = map[string]descriptorpb.Edition{
	"2023": descriptorpb.Edition_EDITION_2023,
	"2024": descriptorpb.Edition_EDITION_2024,
}

func (p *parser) parseEdition(fd *descriptorpb.FileDescriptorProto) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "edition"
	if _, err := p.tok.Expect("="); err != nil {
		return err
	}
	valTok, err := p.tok.ExpectString()
	if err != nil {
		return err
	}
	// Concatenate adjacent string tokens (C++ protoc allows this)
	for p.tok.Peek().Type == tokenizer.TokenString {
		next := p.tok.Next()
		p.trackEnd(next)
		valTok.Value += next.Value
	}
	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}

	edEnum, ok := editionMap[valTok.Value]
	if !ok {
		return fmt.Errorf("%d:%d: unknown edition %q", valTok.Line+1, valTok.Column+1, valTok.Value)
	}
	if edEnum > descriptorpb.Edition_EDITION_2024 {
		return fmt.Errorf("%d:%d: Edition %s is later than the maximum supported edition 2024", startTok.Line+1, startTok.Column+1, valTok.Value)
	}

	fd.Syntax = proto.String("editions")
	fd.Edition = edEnum.Enum()
	p.syntax = "editions"
	p.syntaxParsed = true
	p.trackEnd(endTok)
	// path [12] = syntax field in FileDescriptorProto (used for edition declaration too)
	p.addLocationSpan([]int32{12}, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.attachComments(len(p.locations)-1, firstIdx)

	return nil
}

func (p *parser) parsePackage(fd *descriptorpb.FileDescriptorProto) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "package"
	if p.packageParsed {
		return fmt.Errorf("%d:%d: Multiple package definitions.", startTok.Line+1, startTok.Column+1)
	}
	p.packageParsed = true
	nameTok := p.tok.Next() // package name (may contain dots)
	if nameTok.Type != tokenizer.TokenIdent {
		return fmt.Errorf("%d:%d: Expected identifier.", nameTok.Line+1, nameTok.Column+1)
	}
	name := nameTok.Value
	for p.tok.Peek().Value == "." {
		p.tok.Next() // consume "."
		part := p.tok.Next()
		if part.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Expected identifier.", part.Line+1, part.Column+1)
		}
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
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "import"

	// Check for "public" or "weak"
	isPublic := false
	isWeak := false
	var publicTok tokenizer.Token
	var weakTok tokenizer.Token
	if p.tok.Peek().Value == "public" {
		publicTok = p.tok.Next()
		isPublic = true
	} else if p.tok.Peek().Value == "weak" {
		weakTok = p.tok.Next()
		isWeak = true
	}

	pathTok, err := p.tok.ExpectString()
	if err != nil {
		return err
	}
	// Adjacent string literal concatenation (like C/C++)
	importPath := pathTok.Value
	for p.tok.Peek().Type == tokenizer.TokenString {
		nextTok := p.tok.Next()
		importPath += nextTok.Value
	}
	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)

	if p.seenImports[importPath] {
		return fmt.Errorf("%d:%d: Import \"%s\" was listed twice.", startTok.Line+1, startTok.Column+1, importPath)
	}
	p.seenImports[importPath] = true

	depIdx := int32(len(fd.Dependency))
	fd.Dependency = append(fd.Dependency, importPath)

	// Source code info for the import statement: path [3, depIdx]
	p.addLocationSpan([]int32{3, depIdx}, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.attachComments(len(p.locations)-1, firstIdx)

	if isPublic {
		pubIdx := int32(len(fd.PublicDependency))
		fd.PublicDependency = append(fd.PublicDependency, depIdx)
		// Source code info for public keyword: path [10, pubIdx]
		p.addLocationSpan([]int32{10, pubIdx}, publicTok.Line, publicTok.Column, publicTok.Line, publicTok.Column+len(publicTok.Value))
	}

	if isWeak {
		weakIdx := int32(len(fd.WeakDependency))
		fd.WeakDependency = append(fd.WeakDependency, depIdx)
		// Source code info for weak keyword: path [11, weakIdx]
		p.addLocationSpan([]int32{11, weakIdx}, weakTok.Line, weakTok.Column, weakTok.Line, weakTok.Column+len(weakTok.Value))
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
	var nestedExtIdx int32
	seenMsgOptions := map[string]bool{}
	// Track fields needing synthetic oneofs (deferred until after declared oneofs)
	type syntheticOneof struct {
		field *descriptorpb.FieldDescriptorProto
		name  string
	}
	var syntheticOneofs []syntheticOneof

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
			fields, nestedTypes, decl, err := p.parseOneof(path, oneofIdx, &fieldIdx, &nestedMsgIdx)
			if err != nil {
				if errors.Is(err, errBreakOneof) {
					continue
				}
				return nil, err
			}
			msg.OneofDecl = append(msg.OneofDecl, decl)
			msg.Field = append(msg.Field, fields...)
			msg.NestedType = append(msg.NestedType, nestedTypes...)
			oneofIdx++
		case "map":
			if p.tok.PeekAt(1).Value == "<" {
				field, entry, err := p.parseMapField(path, fieldIdx, nestedMsgIdx)
				if err != nil {
					return nil, err
				}
				msg.Field = append(msg.Field, field)
				msg.NestedType = append(msg.NestedType, entry)
				fieldIdx++
				nestedMsgIdx++
			} else {
				field, err := p.parseField(append(copyPath(path), 2, fieldIdx))
				if err != nil {
					return nil, err
				}
				if field.Proto3Optional != nil && *field.Proto3Optional {
					syntheticOneofs = append(syntheticOneofs, syntheticOneof{
						field: field,
						name:  "_" + field.GetName(),
					})
				}
				msg.Field = append(msg.Field, field)
				fieldIdx++
			}
		case "reserved":
			if err := p.parseMessageReserved(msg, path, &reservedRangeIdx, &reservedNameIdx); err != nil {
				return nil, err
			}
		case "option":
			if err := p.parseMessageOption(msg, path, seenMsgOptions); err != nil {
				return nil, err
			}
		case "extend":
			if err := p.parseNestedExtend(msg, path, &nestedExtIdx, &nestedMsgIdx); err != nil {
				return nil, err
			}
		case "extensions":
			if err := p.parseExtensionRange(msg, path, &extensionRangeIdx); err != nil {
				return nil, err
			}
		case ";":
			p.tok.Next() // consume empty statement
		default:
			// Check if this is a group field: label followed by "group"
			if isGroupField(tok.Value, p.tok.PeekAt(1).Value) {
				field, nested, err := p.parseGroupField(path, fieldIdx, nestedMsgIdx)
				if err != nil {
					return nil, err
				}
				msg.Field = append(msg.Field, field)
				msg.NestedType = append(msg.NestedType, nested)
				fieldIdx++
				nestedMsgIdx++
			} else {
				field, err := p.parseField(append(copyPath(path), 2, fieldIdx))
				if err != nil {
					return nil, err
				}
				if field.Proto3Optional != nil && *field.Proto3Optional {
					syntheticOneofs = append(syntheticOneofs, syntheticOneof{
						field: field,
						name:  "_" + field.GetName(),
					})
				}
				msg.Field = append(msg.Field, field)
				fieldIdx++
			}
		}
	}

	// Append synthetic oneofs after all declared oneofs (C++ protoc ordering)
	for _, so := range syntheticOneofs {
		so.field.OneofIndex = proto.Int32(oneofIdx)
		msg.OneofDecl = append(msg.OneofDecl, &descriptorpb.OneofDescriptorProto{
			Name: proto.String(so.name),
		})
		oneofIdx++
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update message declaration span
	p.locations[msgLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	// Fix extension range max when message_set_wire_format is set but extensions
	// were parsed before the option (C++ protoc resolves max post-hoc).
	if msg.GetOptions().GetMessageSetWireFormat() {
		for _, er := range msg.ExtensionRange {
			if er.GetEnd() == 536870912 {
				er.End = proto.Int32(2147483647)
			}
		}
	}

	return msg, nil
}

func (p *parser) parseMessageReserved(msg *descriptorpb.DescriptorProto, msgPath []int32, rangeIdx, nameIdx *int32) error {
	firstIdx := p.tok.CurrentIndex()
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
			nameVal := nameTok.Value
			nameEndLine, nameEndCol := nameTok.Line, nameTok.Column+nameTok.RawLen
			// Adjacent string concatenation
			for p.tok.Peek().Type == tokenizer.TokenString {
				nextStr := p.tok.Next()
				nameVal += nextStr.Value
				nameEndLine = nextStr.Line
				nameEndCol = nextStr.Column + nextStr.RawLen
			}
			msg.ReservedName = append(msg.ReservedName, nameVal)

			// Source code info for individual reserved name
			p.addLocationSpan(append(copyPath(stmtPath), *nameIdx),
				nameTok.Line, nameTok.Column, nameEndLine, nameEndCol)
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
		p.attachComments(len(p.locations)-int(*nameIdx)-1, firstIdx)
	} else if p.tok.Peek().Type == tokenizer.TokenIdent && p.syntax == "editions" {
		// Editions supports bare identifier reserved names: reserved foo, bar;
		stmtPath := append(copyPath(msgPath), 10) // field 10 = reserved_name
		for {
			nameTok := p.tok.Next() // consume identifier
			nameVal := nameTok.Value
			msg.ReservedName = append(msg.ReservedName, nameVal)

			// Source code info for individual reserved name
			p.addLocationSpan(append(copyPath(stmtPath), *nameIdx),
				nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))
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
		p.attachComments(len(p.locations)-int(*nameIdx)-1, firstIdx)
	} else {
		// reserved 2, 15, 9 to 11;
		stmtPath := append(copyPath(msgPath), 9) // field 9 = reserved_range
		startCount := *rangeIdx

		// Check if the first token is valid for a number range
		if pk := p.tok.Peek(); pk.Type != tokenizer.TokenInt {
			if pk.Type == tokenizer.TokenIdent {
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Reserved names must be string literals. (Only editions supports identifiers.)", p.filename, pk.Line+1, pk.Column+1))
			} else {
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected field name or number range.", p.filename, pk.Line+1, pk.Column+1))
			}
			p.skipToToken(";")
			return nil
		}

		for {
			numTok, err := p.tok.ExpectInt()
			if err != nil {
				return err
			}
			startNum, parseErr := parseIntLenient(numTok.Value, 0, 64)
			if parseErr != nil || startNum > math.MaxInt32 || startNum < math.MinInt32 {
				return fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
			}
			endNum := startNum + 1 // exclusive, single number
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
					e, parseErr := parseIntLenient(endNumTok.Value, 0, 64)
					if parseErr != nil || e > math.MaxInt32 || e < math.MinInt32 {
						return fmt.Errorf("%d:%d: Integer out of range.", endNumTok.Line+1, endNumTok.Column+1)
					}
					endNum = e + 1 // exclusive
					endSpanLine = endNumTok.Line
					endSpanCol = endNumTok.Column + len(endNumTok.Value)
					endNumLine = endNumTok.Line
					endNumCol = endNumTok.Column
					endNumLen = len(endNumTok.Value)
				}
			}

			if endNum <= startNum {
				return fmt.Errorf("%d:%d: Reserved range end number must be greater than start number.", numTok.Line+1, numTok.Column+1)
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
		p.attachComments(len(p.locations)-count*3-1, firstIdx)
	}
	return nil
}

func (p *parser) parseExtensionRange(msg *descriptorpb.DescriptorProto, msgPath []int32, rangeIdx *int32) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "extensions"
	stmtPath := append(copyPath(msgPath), 5) // field 5 = extension_range
	startCount := *rangeIdx
	locsBefore := len(p.locations)

	for {
		numTok, err := p.tok.ExpectInt()
		if err != nil {
			return err
		}
		startNum, parseErr := parseIntLenient(numTok.Value, 0, 64)
		if parseErr != nil || startNum > math.MaxInt32 || startNum < math.MinInt32 {
			return fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
		}
		endNum := startNum + 1
		endSpanLine, endSpanCol := numTok.Line, numTok.Column+len(numTok.Value)
		endNumLine, endNumCol, endNumLen := numTok.Line, numTok.Column, len(numTok.Value)

		if p.tok.Peek().Value == "to" {
			p.tok.Next()
			if p.tok.Peek().Value == "max" {
				maxTok := p.tok.Next()
				if msg.GetOptions().GetMessageSetWireFormat() {
					endNum = 2147483647 // INT32_MAX for message_set_wire_format
				} else {
					endNum = 536870912 // kMaxRangeSentinel (2^29)
				}
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
				e, parseErr := parseIntLenient(endNumTok.Value, 0, 64)
				if parseErr != nil || e > math.MaxInt32 || e < math.MinInt32 {
					return fmt.Errorf("%d:%d: Integer out of range.", endNumTok.Line+1, endNumTok.Column+1)
				}
				endNum = e + 1
				endSpanLine = endNumTok.Line
				endSpanCol = endNumTok.Column + len(endNumTok.Value)
				endNumLine = endNumTok.Line
				endNumCol = endNumTok.Column
				endNumLen = len(endNumTok.Value)
			}
		}

		if endNum <= startNum {
			return fmt.Errorf("%d:%d: Extension range end number must be greater than start number.", numTok.Line+1, numTok.Column+1)
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

	// Parse extension range options [verification = UNVERIFIED, declaration = { ... }, ...]
	if p.tok.Peek().Value == "[" {
		openTok := p.tok.Next() // consume "["
		type extRangeOpt struct {
			fieldNum int32
			nameTok  tokenizer.Token
			valTok   tokenizer.Token
		}
		type declSCI struct {
			nameTok      tokenizer.Token
			closeBraceTok tokenizer.Token
		}
		var parsedOpts []extRangeOpt
		var declarations []*descriptorpb.ExtensionRangeOptions_Declaration
		var declSCIs []declSCI
		for {
			nameTok := p.tok.Next()
			if nameTok.Value == "(" {
				// Parenthesized custom option, e.g., (my_annotation)
				fullName, err := p.parseParenthesizedOptionName(nameTok)
				if err != nil {
					return err
				}

				var custOpt CustomExtRangeOption
				custOpt.ParenName = fullName
				inner := fullName
				if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
					inner = inner[1 : len(inner)-1]
				}
				custOpt.InnerName = inner
				custOpt.NameTok = nameTok

				// Parse sub-field path before "="
				for p.tok.Peek().Value == "." {
					p.tok.Next() // consume "."
					subTok := p.tok.Next()
					custOpt.SubFieldPath = append(custOpt.SubFieldPath, subTok.Value)
				}

				if _, err := p.tok.Expect("="); err != nil {
					return err
				}

				if p.tok.Peek().Value == "{" {
					braceTok := p.tok.Next()
					custOpt.AggregateBraceTok = braceTok
					agg, aggErr := p.consumeAggregate()
					if aggErr != nil {
						return fmt.Errorf("%d:%d: %s", braceTok.Line+1, braceTok.Column+1, aggErr.Error())
					}
					custOpt.AggregateFields = agg
					closeBrace := p.tok.Next() // consume "}"
					custOpt.ValEndLine = closeBrace.Line
					custOpt.ValEndCol = closeBrace.Column + 1
				} else if p.tok.Peek().Value == "<" {
					// Angle bracket rejection
					angleTok := p.tok.Next()
					p.errors = append(p.errors, fmt.Sprintf("%d:%d: Expected option value.", angleTok.Line+1, angleTok.Column+1))
					depth := 1
					for depth > 0 {
						t := p.tok.Next()
						if t.Value == "<" {
							depth++
						} else if t.Value == ">" {
							depth--
						}
					}
				} else {
					negative := false
					if p.tok.Peek().Value == "-" {
						p.tok.Next()
						negative = true
					}
					valTok := p.tok.Next()
					custOpt.AggregateBraceTok = valTok
					custOpt.Value = valTok.Value
					custOpt.ValueType = valTok.Type
					custOpt.Negative = negative
					endCol := valTok.Column + len(valTok.Value)
					if valTok.RawLen > 0 {
						endCol = valTok.Column + valTok.RawLen
					}
					// Adjacent string concatenation
					for valTok.Type == tokenizer.TokenString && p.tok.Peek().Type == tokenizer.TokenString {
						nextStr := p.tok.Next()
						custOpt.Value += nextStr.Value
						endCol = nextStr.Column + len(nextStr.Value)
						if nextStr.RawLen > 0 {
							endCol = nextStr.Column + nextStr.RawLen
						}
						valTok = nextStr
					}
					custOpt.ValEndLine = valTok.Line
					custOpt.ValEndCol = endCol
				}

				// Collect ranges this option applies to
				for i := startCount; i < *rangeIdx; i++ {
					if msg.ExtensionRange[i].Options == nil {
						msg.ExtensionRange[i].Options = &descriptorpb.ExtensionRangeOptions{}
					}
					custOpt.Ranges = append(custOpt.Ranges, msg.ExtensionRange[i])
				}
				p.customExtRangeOptions = append(p.customExtRangeOptions, custOpt)
			} else {
			if _, err := p.tok.Expect("="); err != nil {
				return err
			}
			if p.tok.Peek().Value == "{" {
				openBrace := p.tok.Next()
				if nameTok.Value == "declaration" {
					decl := &descriptorpb.ExtensionRangeOptions_Declaration{}
					closeBrace := p.parseDeclarationLiteral(decl, openBrace)
					declarations = append(declarations, decl)
					declSCIs = append(declSCIs, declSCI{nameTok, closeBrace})
				} else {
					// Skip unknown message literal
					depth := 1
					var lastTok tokenizer.Token
					for depth > 0 {
						lastTok = p.tok.Next()
						if lastTok.Value == "{" {
							depth++
						} else if lastTok.Value == "}" {
							depth--
						}
					}
					_ = lastTok
				}
			} else {
				valTok := p.tok.Next()
				if nameTok.Value == "verification" {
					var v descriptorpb.ExtensionRangeOptions_VerificationState
					switch valTok.Value {
					case "UNVERIFIED":
						v = descriptorpb.ExtensionRangeOptions_UNVERIFIED
					case "DECLARATION":
						v = descriptorpb.ExtensionRangeOptions_DECLARATION
					}
					for i := startCount; i < *rangeIdx; i++ {
						if msg.ExtensionRange[i].Options == nil {
							msg.ExtensionRange[i].Options = &descriptorpb.ExtensionRangeOptions{}
						}
						msg.ExtensionRange[i].Options.Verification = &v
					}
					parsedOpts = append(parsedOpts, extRangeOpt{3, nameTok, valTok})
				}
			}
			} // end else (non-parenthesized)
			if p.tok.Peek().Value == "," {
				p.tok.Next()
			} else {
				break
			}
		}
		closeTok, err := p.tok.Expect("]")
		if err != nil {
			return err
		}
		// Set declarations on each range
		if len(declarations) > 0 {
			for i := startCount; i < *rangeIdx; i++ {
				if msg.ExtensionRange[i].Options == nil {
					msg.ExtensionRange[i].Options = &descriptorpb.ExtensionRangeOptions{}
				}
				msg.ExtensionRange[i].Options.Declaration = declarations
			}
		}
		// Add SCI for each range's options
		for i := startCount; i < *rangeIdx; i++ {
			rangePath := append(copyPath(stmtPath), i)
			optsPath := append(copyPath(rangePath), 3) // field 3 = options
			p.addLocationSpan(optsPath, openTok.Line, openTok.Column, closeTok.Line, closeTok.Column+1)
			for _, opt := range parsedOpts {
				optPath := append(copyPath(optsPath), opt.fieldNum)
				p.addLocationSpan(optPath, opt.nameTok.Line, opt.nameTok.Column, opt.valTok.Line, opt.valTok.Column+len(opt.valTok.Value))
			}
			// SCI for custom ext range options (placeholder field number 0, resolved post-parse)
			for j := range p.customExtRangeOptions {
				co := &p.customExtRangeOptions[j]
				if len(co.Ranges) == 0 || co.Ranges[0] != msg.ExtensionRange[startCount] {
					continue // not from this statement
				}
				sciPath := append(copyPath(optsPath), 0) // placeholder
				for range co.SubFieldPath {
					sciPath = append(sciPath, 0)
				}
				loc := p.addLocationSpanReturn(sciPath, co.NameTok.Line, co.NameTok.Column, co.ValEndLine, co.ValEndCol)
				co.SCILocs = append(co.SCILocs, loc)
			}
			// SCI for declaration entries (field 2 = declaration in ExtensionRangeOptions)
			for j, ds := range declSCIs {
				declPath := append(copyPath(optsPath), 2, int32(j)) // field 2 = declaration, index j
				p.addLocationSpan(declPath, ds.nameTok.Line, ds.nameTok.Column, ds.closeBraceTok.Line, ds.closeBraceTok.Column+1)
			}
		}
	}

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)
	p.addLocationSpan(stmtPath, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	stmtLoc := p.locations[len(p.locations)-1]
	copy(p.locations[locsBefore+1:], p.locations[locsBefore:len(p.locations)-1])
	p.locations[locsBefore] = stmtLoc
	p.attachComments(locsBefore, firstIdx)

	return nil
}

// parseDeclarationLiteral parses the body of a declaration message literal
// (after the opening `{`) and returns the closing `}` token.
func (p *parser) parseDeclarationLiteral(decl *descriptorpb.ExtensionRangeOptions_Declaration, openBrace tokenizer.Token) tokenizer.Token {
	for p.tok.Peek().Value != "}" {
		fieldName := p.tok.Next()
		p.tok.Next() // consume ":"
		switch fieldName.Value {
		case "number":
			valTok := p.tok.Next()
			num, _ := strconv.ParseInt(valTok.Value, 0, 32)
			decl.Number = proto.Int32(int32(num))
		case "full_name":
			valTok := p.tok.Next()
			decl.FullName = proto.String(valTok.Value)
		case "type":
			valTok := p.tok.Next()
			decl.Type = proto.String(valTok.Value)
		case "reserved":
			valTok := p.tok.Next()
			decl.Reserved = proto.Bool(valTok.Value == "true")
		case "repeated":
			valTok := p.tok.Next()
			decl.Repeated = proto.Bool(valTok.Value == "true")
		default:
			p.tok.Next() // skip unknown field value
		}
		// consume optional comma or semicolon separator
		if p.tok.Peek().Value == "," || p.tok.Peek().Value == ";" {
			p.tok.Next()
		}
	}
	return p.tok.Next() // consume "}"
}

func (p *parser) parseExtend(fd *descriptorpb.FileDescriptorProto) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "extend"

	// Parse extendee type name
	extNameTok := p.tok.Next()
	extendeeName := extNameTok.Value
	extNameStartLine, extNameStartCol := extNameTok.Line, extNameTok.Column
	extNameEndTok := extNameTok
	if extendeeName == "." {
		part := p.tok.Next()
		extendeeName += part.Value
		extNameEndTok = part
	}
	for p.tok.Peek().Value == "." {
		p.tok.Next()
		part := p.tok.Next()
		extendeeName += "." + part.Value
		extNameEndTok = part
	}
	extNameEndLine := extNameEndTok.Line
	extNameEndCol := extNameEndTok.Column + len(extNameEndTok.Value)

	if _, err := p.tok.Expect("{"); err != nil {
		return err
	}

	// Empty extend block — produce errors matching C++ protoc
	if p.tok.Peek().Value == "}" {
		closeTok := p.tok.Peek()
		if p.syntax == "proto2" {
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected \"required\", \"optional\", or \"repeated\".", p.filename, closeTok.Line+1, closeTok.Column+1))
		}
		p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected type name.", p.filename, closeTok.Line+1, closeTok.Column+1))
	}

	// Placeholder for extend block span [7]
	blockPath := []int32{7}
	blockLocIdx := p.addLocationPlaceholder(blockPath)
	p.attachComments(blockLocIdx, firstIdx)

	var endTok tokenizer.Token
	for p.tok.Peek().Value != "}" {
		// Reject map fields in extend blocks
		peek := p.tok.Peek()
		if peek.Value == "map" && p.tok.PeekAt(1).Value == "<" {
			p.tok.Next() // consume "map"
			ltTok := p.tok.Next() // consume "<"
			return fmt.Errorf("%d:%d: Map fields are not allowed to be extensions.", ltTok.Line+1, ltTok.Column+1)
		}

		extIdx := int32(len(fd.Extension))
		fieldPath := []int32{7, extIdx}

		// Check if this is a group field in extend block
		if isGroupField(peek.Value, p.tok.PeekAt(1).Value) {
			nestedMsgIdx := int32(len(fd.MessageType))
			field, nested, err := p.parseGroupFieldInExtend(fieldPath, []int32{4, nestedMsgIdx}, extendeeName, extNameStartLine, extNameStartCol, extNameEndLine, extNameEndCol)
			if err != nil {
				return err
			}
			fd.Extension = append(fd.Extension, field)
			fd.MessageType = append(fd.MessageType, nested)
			continue
		}

		// Track location count before parseField to insert extendee span right after field span
		locCountBefore := len(p.locations)

		field, err := p.parseField(fieldPath)
		if err != nil {
			return err
		}
		field.Extendee = proto.String(extendeeName)

		// Insert extendee source code info right after field declaration span (index locCountBefore)
		// to match C++ ordering: field, extendee, label, type, name, number
		extendeeLoc := &descriptorpb.SourceCodeInfo_Location{
			Path: append(copyPath(fieldPath), 2),
			Span: multiSpan(extNameStartLine, extNameStartCol, extNameEndLine, extNameEndCol),
		}
		insertIdx := locCountBefore + 1
		p.locations = append(p.locations, nil)
		copy(p.locations[insertIdx+1:], p.locations[insertIdx:])
		p.locations[insertIdx] = extendeeLoc

		fd.Extension = append(fd.Extension, field)
	}

	endTok = p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update extend block span
	p.locations[blockLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	return nil
}

// parseGroupFieldInExtend parses a group field inside an extend block.
// The extension field goes to fd.Extension, the nested message to fd.MessageType (or msg.NestedType).
func (p *parser) parseGroupFieldInExtend(fieldPath, nestedPath []int32, extendeeName string, extNameStartLine, extNameStartCol, extNameEndLine, extNameEndCol int) (*descriptorpb.FieldDescriptorProto, *descriptorpb.DescriptorProto, error) {
	firstIdx := p.tok.CurrentIndex()
	field := &descriptorpb.FieldDescriptorProto{}

	// Label
	labelTok := p.tok.Next()
	switch labelTok.Value {
	case "required":
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum()
	case "optional":
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	case "repeated":
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	}

	// "group" keyword
	groupTok := p.tok.Next()

	// Group name (must start with uppercase)
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, nil, err
	}
	groupName := nameTok.Value
	if len(groupName) == 0 || groupName[0] < 'A' || groupName[0] > 'Z' {
		return nil, nil, fmt.Errorf("%d:%d: Group names must start with a capital letter.", nameTok.Line+1, nameTok.Column+1)
	}
	fieldName := strings.ToLower(groupName)

	field.Name = proto.String(fieldName)
	field.JsonName = proto.String(tokenizer.ToJSONName(fieldName))
	field.Type = descriptorpb.FieldDescriptorProto_TYPE_GROUP.Enum()
	field.TypeName = proto.String(groupName)
	field.Extendee = proto.String(extendeeName)

	// = number
	if _, err := p.tok.Expect("="); err != nil {
		return nil, nil, err
	}
	numTok, err := p.tok.ExpectInt()
	if err != nil {
		return nil, nil, err
	}
	num, parseErr := parseIntLenient(numTok.Value, 0, 64)
	if parseErr != nil || num > math.MaxInt32 || num < math.MinInt32 {
		return nil, nil, fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
	}
	field.Number = proto.Int32(int32(num))

	// Optional field options [deprecated = true, etc.]
	var optionLocs []*descriptorpb.SourceCodeInfo_Location
	if p.tok.Peek().Value == "[" {
		var err error
		optionLocs, err = p.parseFieldOptions(field, fieldPath)
		if err != nil {
			return nil, nil, err
		}
	}

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, nil, err
	}

	// SCI: field placeholder
	fieldLocIdx := p.addLocationPlaceholder(fieldPath)
	p.attachComments(fieldLocIdx, firstIdx)

	// SCI: extendee (right after field span)
	p.addLocationSpan(append(copyPath(fieldPath), 2),
		extNameStartLine, extNameStartCol, extNameEndLine, extNameEndCol)

	// SCI: label
	p.addLocationSpan(append(copyPath(fieldPath), 4),
		labelTok.Line, labelTok.Column, labelTok.Line, labelTok.Column+len(labelTok.Value))

	// SCI: type ("group" keyword)
	p.addLocationSpan(append(copyPath(fieldPath), 5),
		groupTok.Line, groupTok.Column, groupTok.Line, groupTok.Column+len(groupTok.Value))

	// SCI: name
	p.addLocationSpan(append(copyPath(fieldPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// SCI: number
	p.addLocationSpan(append(copyPath(fieldPath), 3),
		numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))

	// Append deferred option SCI locations after number span
	p.locations = append(p.locations, optionLocs...)

	// SCI: nested message placeholder
	nestedLocIdx := p.addLocationPlaceholder(nestedPath)

	// SCI: nested message name
	p.addLocationSpan(append(copyPath(nestedPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// SCI: type_name
	p.addLocationSpan(append(copyPath(fieldPath), 6),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Parse group body (full message body: fields, enums, nested messages, etc.)
	nested := &descriptorpb.DescriptorProto{
		Name: proto.String(groupName),
	}
	if err := p.parseGroupBody(nested, nestedPath); err != nil {
		return nil, nil, err
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update field and nested type spans
	groupSpan := multiSpan(labelTok.Line, labelTok.Column, endTok.Line, endTok.Column+1)
	p.locations[fieldLocIdx].Span = groupSpan
	p.locations[nestedLocIdx].Span = groupSpan

	return field, nested, nil
}

func (p *parser) parseNestedExtend(msg *descriptorpb.DescriptorProto, msgPath []int32, extIdx *int32, nestedMsgIdx *int32) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "extend"

	// Parse extendee type name
	extNameTok := p.tok.Next()
	extendeeName := extNameTok.Value
	extNameStartLine, extNameStartCol := extNameTok.Line, extNameTok.Column
	extNameEndTok := extNameTok
	if extendeeName == "." {
		part := p.tok.Next()
		extendeeName += part.Value
		extNameEndTok = part
	}
	for p.tok.Peek().Value == "." {
		p.tok.Next()
		part := p.tok.Next()
		extendeeName += "." + part.Value
		extNameEndTok = part
	}
	extNameEndLine := extNameEndTok.Line
	extNameEndCol := extNameEndTok.Column + len(extNameEndTok.Value)

	if _, err := p.tok.Expect("{"); err != nil {
		return err
	}

	// Empty extend block — produce errors matching C++ protoc
	if p.tok.Peek().Value == "}" {
		closeTok := p.tok.Peek()
		if p.syntax == "proto2" {
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected \"required\", \"optional\", or \"repeated\".", p.filename, closeTok.Line+1, closeTok.Column+1))
		}
		p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected type name.", p.filename, closeTok.Line+1, closeTok.Column+1))
	}

	// Placeholder for extend block span — field 6 = extension in DescriptorProto
	blockPath := append(copyPath(msgPath), 6)
	blockLocIdx := p.addLocationPlaceholder(blockPath)
	p.attachComments(blockLocIdx, firstIdx)

	var endTok tokenizer.Token
	for p.tok.Peek().Value != "}" {
		// Reject map fields in extend blocks
		peek := p.tok.Peek()
		if peek.Value == "map" && p.tok.PeekAt(1).Value == "<" {
			p.tok.Next() // consume "map"
			ltTok := p.tok.Next() // consume "<"
			return fmt.Errorf("%d:%d: Map fields are not allowed to be extensions.", ltTok.Line+1, ltTok.Column+1)
		}

		fieldPath := append(copyPath(msgPath), 6, *extIdx)

		// Check if this is a group field in extend block
		if isGroupField(peek.Value, p.tok.PeekAt(1).Value) {
			nestedPath := append(copyPath(msgPath), 3, *nestedMsgIdx)
			field, nested, err := p.parseGroupFieldInExtend(fieldPath, nestedPath, extendeeName, extNameStartLine, extNameStartCol, extNameEndLine, extNameEndCol)
			if err != nil {
				return err
			}
			msg.Extension = append(msg.Extension, field)
			msg.NestedType = append(msg.NestedType, nested)
			*extIdx++
			*nestedMsgIdx++
			continue
		}

		locCountBefore := len(p.locations)

		field, err := p.parseField(fieldPath)
		if err != nil {
			return err
		}
		field.Extendee = proto.String(extendeeName)

		// Insert extendee source code info right after field declaration span
		extendeeLoc := &descriptorpb.SourceCodeInfo_Location{
			Path: append(copyPath(fieldPath), 2),
			Span: multiSpan(extNameStartLine, extNameStartCol, extNameEndLine, extNameEndCol),
		}
		insertIdx := locCountBefore + 1
		p.locations = append(p.locations, nil)
		copy(p.locations[insertIdx+1:], p.locations[insertIdx:])
		p.locations[insertIdx] = extendeeLoc

		msg.Extension = append(msg.Extension, field)
		*extIdx++
	}

	endTok = p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update extend block span
	p.locations[blockLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	return nil
}

func (p *parser) parseMessageOption(msg *descriptorpb.DescriptorProto, msgPath []int32, seenOptions map[string]bool) error {
	firstIdx := p.tok.CurrentIndex()
	_ = firstIdx
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if optName == "(" {
		fullName, err := p.parseParenthesizedOptionName(nameTok)
		if err != nil {
			return err
		}
		// Extract inner name (strip parens)
		inner := fullName
		if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
			inner = inner[1 : len(inner)-1]
		}

		// Handle sub-field path: option (name).sub1.sub2... = value;
		var subFieldPath []string
		for p.tok.Peek().Value == "." {
			dotTok := p.tok.Next()
			p.trackEnd(dotTok)
			subTok := p.tok.Next()
			p.trackEnd(subTok)
			subFieldPath = append(subFieldPath, subTok.Value)
		}

		// Consume "="
		if _, err := p.tok.Expect("="); err != nil {
			return err
		}

		var custOpt CustomMessageOption
		custOpt.ParenName = fullName
		custOpt.InnerName = inner
		custOpt.SubFieldPath = subFieldPath
		custOpt.NameTok = nameTok
		custOpt.Message = msg

		// Reject angle bracket aggregate syntax and positive sign
		if p.tok.Peek().Value == "<" || p.tok.Peek().Value == "+" {
			rejTok := p.tok.Next()
			return fmt.Errorf("%d:%d: Expected option value.", rejTok.Line+1, rejTok.Column+1)
		}

		// Read value
		if p.tok.Peek().Value == "-" {
			p.tok.Next()
			custOpt.Negative = true
		}

		if p.tok.Peek().Value == "{" {
			brTok := p.tok.Next() // consume '{'
			p.trackEnd(brTok)
			custOpt.AggregateBraceTok = brTok
			var aggErr error
			custOpt.AggregateFields, aggErr = p.consumeAggregate()
			if aggErr != nil {
				return fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", brTok.Line+1, brTok.Column+1, inner, aggErr.Error())
			}
			closeTok := p.tok.Next() // consume '}'
			p.trackEnd(closeTok)
		} else {
			valTok := p.tok.Next()
			p.trackEnd(valTok)
			custOpt.AggregateBraceTok = valTok
			val := valTok.Value
			if custOpt.Negative {
				val = "-" + val
			}
			custOpt.Value = val
			custOpt.ValueType = valTok.Type
			// Handle string concatenation
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					next := p.tok.Next()
					p.trackEnd(next)
					custOpt.Value += next.Value
				}
			}
		}

		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)

		if msg.Options == nil {
			msg.Options = &descriptorpb.MessageOptions{}
		}

		// SCI: [msgPath..., 7] for options statement, [msgPath..., 7, 0, ...] placeholder
		optPath := append(copyPath(msgPath), 7)
		span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: optPath,
			Span: span,
		})
		sciPath := append(copyPath(optPath), 0)
		if len(subFieldPath) > 0 {
			sciPath = make([]int32, len(optPath)+1+len(subFieldPath))
			copy(sciPath, optPath)
			// remaining elements will be resolved post-parse
		}
		sciLoc := &descriptorpb.SourceCodeInfo_Location{
			Path: sciPath,
			Span: span,
		}
		p.locations = append(p.locations, sciLoc)
		p.attachComments(len(p.locations)-1, firstIdx)
		custOpt.SCILoc = sciLoc

		p.customMessageOptions = append(p.customMessageOptions, custOpt)
		return nil
	}

	// Handle dotted option names like features.field_presence
	var featSubField string
	if optName == "features" && p.tok.Peek().Value == "." {
		dotTok := p.tok.Next()
		p.trackEnd(dotTok)
		subTok := p.tok.Next()
		p.trackEnd(subTok)
		featSubField = subTok.Value
		optName = "features." + featSubField
	}

	if seenOptions[optName] {
		return fmt.Errorf("%d:%d: Option \"%s\" was already set.", nameTok.Line+1, nameTok.Column+1, optName)
	}
	seenOptions[optName] = true

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

	validateMsgBool := func(name string) error {
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.MessageOptions.%s\".", valTok.Line+1, valTok.Column+1, name)
		}
		if valTok.Value != "true" && valTok.Value != "false" {
			return fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.MessageOptions.%s\".", valTok.Line+1, valTok.Column+1, name)
		}
		return nil
	}

	var fieldNum int32
	switch optName {
	case "deprecated":
		if err := validateMsgBool("deprecated"); err != nil {
			return err
		}
		msg.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 3
	case "no_standard_descriptor_accessor":
		if err := validateMsgBool("no_standard_descriptor_accessor"); err != nil {
			return err
		}
		msg.Options.NoStandardDescriptorAccessor = proto.Bool(valTok.Value == "true")
		fieldNum = 2
	case "message_set_wire_format":
		if err := validateMsgBool("message_set_wire_format"); err != nil {
			return err
		}
		msg.Options.MessageSetWireFormat = proto.Bool(valTok.Value == "true")
		fieldNum = 1
	case "deprecated_legacy_json_field_conflicts":
		if err := validateMsgBool("deprecated_legacy_json_field_conflicts"); err != nil {
			return err
		}
		msg.Options.DeprecatedLegacyJsonFieldConflicts = proto.Bool(valTok.Value == "true")
		fieldNum = 11
	case "map_entry":
		return fmt.Errorf("%d:%d: map_entry should not be set explicitly. Use map<KeyType, ValueType> instead.", nameTok.Line+1, nameTok.Column+1)
	default:
		if featSubField != "" {
			if msg.Options.Features == nil {
				msg.Options.Features = &descriptorpb.FeatureSet{}
			}
			if valTok.Type != tokenizer.TokenIdent {
				return fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.MessageOptions.features.%s\".", valTok.Line+1, valTok.Column+1, featSubField)
			}
			var featFieldNum int32
			switch featSubField {
			case "field_presence":
				v, ok := descriptorpb.FeatureSet_FieldPresence_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.MessageOptions.features.field_presence\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				msg.Options.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
				featFieldNum = 1
			case "enum_type":
				v, ok := descriptorpb.FeatureSet_EnumType_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.MessageOptions.features.enum_type\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				msg.Options.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
				featFieldNum = 2
			case "repeated_field_encoding":
				v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.MessageOptions.features.repeated_field_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				msg.Options.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
				featFieldNum = 3
			case "utf8_validation":
				v, ok := descriptorpb.FeatureSet_Utf8Validation_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.MessageOptions.features.utf8_validation\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				msg.Options.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
				featFieldNum = 4
			case "message_encoding":
				v, ok := descriptorpb.FeatureSet_MessageEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.MessageOptions.features.message_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				msg.Options.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
				featFieldNum = 5
			case "json_format":
				v, ok := descriptorpb.FeatureSet_JsonFormat_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.MessageOptions.features.json_format\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				msg.Options.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
				featFieldNum = 6
			default:
				return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
			}
			// SCI: [msgPath..., 7] for options statement, [msgPath..., 7, 12, featFieldNum] for specific feature
			optPath := append(copyPath(msgPath), 7)
			span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: optPath,
				Span: span,
			})
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: append(copyPath(optPath), 12, featFieldNum),
				Span: span,
			})
			p.attachComments(len(p.locations)-1, firstIdx)
			return nil
		}
		return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
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
	p.attachComments(len(p.locations)-1, firstIdx)

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
		if p.syntax == "editions" {
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Label \"required\" is not supported in editions, use features.field_presence = LEGACY_REQUIRED.", p.filename, lt.Line+1, lt.Column+1))
		}
	case "optional":
		lt := p.tok.Next()
		labelTok = &lt
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
		if p.syntax == "proto3" {
			field.Proto3Optional = proto.Bool(true)
		}
		if p.syntax == "editions" {
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Label \"optional\" is not supported in editions. By default, all singular fields have presence unless features.field_presence is set.", p.filename, lt.Line+1, lt.Column+1))
		}
	case "repeated":
		lt := p.tok.Next()
		labelTok = &lt
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	default:
		if p.syntax == "proto2" && !p.inOneof {
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected \"required\", \"optional\", or \"repeated\".", p.filename, startTok.Line+1, startTok.Column+1))
		}
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	}

	// Type
	typeTok := p.tok.Next()
	typeStartLine, typeStartCol := typeTok.Line, typeTok.Column

	// Reject labels on map fields
	if typeTok.Value == "map" && p.tok.Peek().Value == "<" && labelTok != nil {
		angleTok := p.tok.Peek()
		return nil, fmt.Errorf("%d:%d: Field labels (required/optional/repeated) are not allowed on map fields.", angleTok.Line+1, angleTok.Column+1)
	}

	typeEndTok := typeTok
	if builtinType, ok := builtinTypes[typeTok.Value]; ok {
		field.Type = builtinType.Enum()
	} else {
		// Message or enum reference (may start with "." for FQN)
		typeName := typeTok.Value
		if typeName == "." {
			part := p.tok.Next()
			typeName += part.Value
			typeEndTok = part
		}
		for p.tok.Peek().Value == "." {
			p.tok.Next()
			part := p.tok.Next()
			typeName += "." + part.Value
			typeEndTok = part
		}
		field.TypeName = proto.String(typeName)
	}

	// Name
	nameTok := p.tok.Next()
	if nameTok.Type != tokenizer.TokenIdent {
		return nil, fmt.Errorf("%d:%d: Expected field name.", nameTok.Line+1, nameTok.Column+1)
	}
	field.Name = proto.String(nameTok.Value)
	field.JsonName = proto.String(tokenizer.ToJSONName(nameTok.Value))

	// = number
	if eqTok := p.tok.Peek(); eqTok.Value != "=" {
		return nil, fmt.Errorf("%d:%d: Missing field number.", eqTok.Line+1, eqTok.Column+1)
	}
	p.tok.Next() // consume "="
	if p.tok.Peek().Type != tokenizer.TokenInt {
		bad := p.tok.Next()
		return nil, fmt.Errorf("%d:%d: Expected field number.", bad.Line+1, bad.Column+1)
	}
	numTok, err := p.tok.ExpectInt()
	if err != nil {
		return nil, err
	}
	num, parseErr := parseIntLenient(numTok.Value, 0, 64)
	if parseErr != nil || num > math.MaxInt32 || num < math.MinInt32 {
		intErr := fmt.Sprintf("%s:%d:%d: Integer out of range.", p.filename, numTok.Line+1, numTok.Column+1)
		if octalErr := invalidOctalError(p.filename, numTok); octalErr != "" {
			return nil, &MultiError{Errors: []string{octalErr, intErr}}
		}
		return nil, fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
	}
	field.Number = proto.Int32(int32(num))

	// Optional field options [deprecated = true, etc.]
	var optionLocs []*descriptorpb.SourceCodeInfo_Location
	if p.tok.Peek().Value == "[" {
		var err error
		optionLocs, err = p.parseFieldOptions(field, path)
		if err != nil {
			return nil, err
		}
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
		typeNameEnd := typeEndTok.Column + len(typeEndTok.Value)
		p.addLocationSpan(append(copyPath(path), 6),
			typeStartLine, typeStartCol, typeEndTok.Line, typeNameEnd)
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

func isGroupField(tok1, tok2 string) bool {
	switch tok1 {
	case "required", "optional", "repeated":
		return tok2 == "group"
	}
	return false
}

// parseGroupFieldInOneof parses a group field inside a oneof block (no label).
// "group" Name "=" Number "{" fields... "}"
func (p *parser) parseGroupFieldInOneof(msgPath []int32, fieldIdx, nestedMsgIdx int32) (*descriptorpb.FieldDescriptorProto, *descriptorpb.DescriptorProto, error) {
	fieldPath := append(copyPath(msgPath), 2, fieldIdx)
	nestedPath := append(copyPath(msgPath), 3, nestedMsgIdx)

	firstIdx := p.tok.CurrentIndex()
	field := &descriptorpb.FieldDescriptorProto{}
	field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()

	// "group" keyword
	groupTok := p.tok.Next()

	// Group name (must start with uppercase)
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, nil, err
	}
	groupName := nameTok.Value
	if len(groupName) == 0 || groupName[0] < 'A' || groupName[0] > 'Z' {
		return nil, nil, fmt.Errorf("%d:%d: Group names must start with a capital letter.", nameTok.Line+1, nameTok.Column+1)
	}
	fieldName := strings.ToLower(groupName)

	field.Name = proto.String(fieldName)
	field.JsonName = proto.String(tokenizer.ToJSONName(fieldName))
	field.Type = descriptorpb.FieldDescriptorProto_TYPE_GROUP.Enum()
	field.TypeName = proto.String(groupName)

	// = number
	if _, err := p.tok.Expect("="); err != nil {
		return nil, nil, err
	}
	numTok, err := p.tok.ExpectInt()
	if err != nil {
		return nil, nil, err
	}
	num, parseErr := parseIntLenient(numTok.Value, 0, 64)
	if parseErr != nil || num > math.MaxInt32 || num < math.MinInt32 {
		return nil, nil, fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
	}
	field.Number = proto.Int32(int32(num))

	// Optional field options [deprecated = true, etc.]
	var optionLocs []*descriptorpb.SourceCodeInfo_Location
	if p.tok.Peek().Value == "[" {
		var err error
		optionLocs, err = p.parseFieldOptions(field, fieldPath)
		if err != nil {
			return nil, nil, err
		}
	}

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, nil, err
	}

	// Source code info for the field
	fieldLocIdx := p.addLocationPlaceholder(fieldPath)
	p.attachComments(fieldLocIdx, firstIdx)

	// No label span for oneof group fields

	// Type span (the "group" keyword) — path [5] = type
	p.addLocationSpan(append(copyPath(fieldPath), 5),
		groupTok.Line, groupTok.Column, groupTok.Line, groupTok.Column+len(groupTok.Value))

	// Name span — path [1] = name
	p.addLocationSpan(append(copyPath(fieldPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Number span — path [3] = number
	p.addLocationSpan(append(copyPath(fieldPath), 3),
		numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))

	// Append deferred option SCI locations after number span
	p.locations = append(p.locations, optionLocs...)

	// Nested message type placeholder
	nestedLocIdx := p.addLocationPlaceholder(nestedPath)

	// Nested type name span
	p.addLocationSpan(append(copyPath(nestedPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Type name span — path [6] = type_name (same span as group name)
	p.addLocationSpan(append(copyPath(fieldPath), 6),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Parse group body (full message body: fields, enums, nested messages, etc.)
	nested := &descriptorpb.DescriptorProto{
		Name: proto.String(groupName),
	}
	if err := p.parseGroupBody(nested, nestedPath); err != nil {
		return nil, nil, err
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update field and nested type spans
	groupSpan := multiSpan(groupTok.Line, groupTok.Column, endTok.Line, endTok.Column+1)
	p.locations[fieldLocIdx].Span = groupSpan
	p.locations[nestedLocIdx].Span = groupSpan

	return field, nested, nil
}

// parseGroupBody parses the contents of a group body (between { and }),
// handling all message body constructs: fields, enums, nested messages, etc.
func (p *parser) parseGroupBody(nested *descriptorpb.DescriptorProto, nestedPath []int32) error {
	var fieldIdx, nestedMsgIdx, nestedEnumIdx, oneofIdx int32
	var reservedRangeIdx, reservedNameIdx int32
	var extensionRangeIdx int32
	var nestedExtIdx int32
	seenMsgOptions := map[string]bool{}
	type syntheticOneof struct {
		field *descriptorpb.FieldDescriptorProto
		name  string
	}
	var syntheticOneofs []syntheticOneof

	for p.tok.Peek().Value != "}" {
		tok := p.tok.Peek()

		switch tok.Value {
		case "message":
			msg, err := p.parseMessage(append(copyPath(nestedPath), 3, nestedMsgIdx))
			if err != nil {
				return err
			}
			nested.NestedType = append(nested.NestedType, msg)
			nestedMsgIdx++
		case "enum":
			e, err := p.parseEnum(append(copyPath(nestedPath), 4, nestedEnumIdx))
			if err != nil {
				return err
			}
			nested.EnumType = append(nested.EnumType, e)
			nestedEnumIdx++
		case "oneof":
			fields, nestedTypes, decl, err := p.parseOneof(nestedPath, oneofIdx, &fieldIdx, &nestedMsgIdx)
			if err != nil {
				if errors.Is(err, errBreakOneof) {
					continue
				}
				return err
			}
			nested.OneofDecl = append(nested.OneofDecl, decl)
			nested.Field = append(nested.Field, fields...)
			nested.NestedType = append(nested.NestedType, nestedTypes...)
			oneofIdx++
		case "map":
			if p.tok.PeekAt(1).Value == "<" {
				field, entry, err := p.parseMapField(nestedPath, fieldIdx, nestedMsgIdx)
				if err != nil {
					return err
				}
				nested.Field = append(nested.Field, field)
				nested.NestedType = append(nested.NestedType, entry)
				fieldIdx++
				nestedMsgIdx++
			} else {
				field, err := p.parseField(append(copyPath(nestedPath), 2, fieldIdx))
				if err != nil {
					return err
				}
				if field.Proto3Optional != nil && *field.Proto3Optional {
					syntheticOneofs = append(syntheticOneofs, syntheticOneof{
						field: field,
						name:  "_" + field.GetName(),
					})
				}
				nested.Field = append(nested.Field, field)
				fieldIdx++
			}
		case "reserved":
			if err := p.parseMessageReserved(nested, nestedPath, &reservedRangeIdx, &reservedNameIdx); err != nil {
				return err
			}
		case "option":
			if err := p.parseMessageOption(nested, nestedPath, seenMsgOptions); err != nil {
				return err
			}
		case "extend":
			if err := p.parseNestedExtend(nested, nestedPath, &nestedExtIdx, &nestedMsgIdx); err != nil {
				return err
			}
		case "extensions":
			if err := p.parseExtensionRange(nested, nestedPath, &extensionRangeIdx); err != nil {
				return err
			}
		case ";":
			p.tok.Next()
		default:
			if isGroupField(tok.Value, p.tok.PeekAt(1).Value) {
				field, nestedType, err := p.parseGroupField(nestedPath, fieldIdx, nestedMsgIdx)
				if err != nil {
					return err
				}
				nested.Field = append(nested.Field, field)
				nested.NestedType = append(nested.NestedType, nestedType)
				fieldIdx++
				nestedMsgIdx++
			} else {
				field, err := p.parseField(append(copyPath(nestedPath), 2, fieldIdx))
				if err != nil {
					return err
				}
				if field.Proto3Optional != nil && *field.Proto3Optional {
					syntheticOneofs = append(syntheticOneofs, syntheticOneof{
						field: field,
						name:  "_" + field.GetName(),
					})
				}
				nested.Field = append(nested.Field, field)
				fieldIdx++
			}
		}
	}

	for _, so := range syntheticOneofs {
		so.field.OneofIndex = proto.Int32(oneofIdx)
		nested.OneofDecl = append(nested.OneofDecl, &descriptorpb.OneofDescriptorProto{
			Name: proto.String(so.name),
		})
		oneofIdx++
	}

	if nested.GetOptions().GetMessageSetWireFormat() {
		for _, er := range nested.ExtensionRange {
			if er.GetEnd() == 536870912 {
				er.End = proto.Int32(2147483647)
			}
		}
	}

	return nil
}

// parseGroupField parses a group field declaration: label "group" Name "=" Number "{" fields... "}"
// Returns the group field and the nested message type.
func (p *parser) parseGroupField(msgPath []int32, fieldIdx, nestedMsgIdx int32) (*descriptorpb.FieldDescriptorProto, *descriptorpb.DescriptorProto, error) {
	fieldPath := append(copyPath(msgPath), 2, fieldIdx)
	nestedPath := append(copyPath(msgPath), 3, nestedMsgIdx)

	firstIdx := p.tok.CurrentIndex()
	field := &descriptorpb.FieldDescriptorProto{}

	// Label
	labelTok := p.tok.Next()
	switch labelTok.Value {
	case "required":
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum()
	case "optional":
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	case "repeated":
		field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	}

	// "group" keyword
	groupTok := p.tok.Next()

	// Group name (must start with uppercase)
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, nil, err
	}
	groupName := nameTok.Value
	if len(groupName) == 0 || groupName[0] < 'A' || groupName[0] > 'Z' {
		return nil, nil, fmt.Errorf("%d:%d: Group names must start with a capital letter.", nameTok.Line+1, nameTok.Column+1)
	}
	fieldName := strings.ToLower(groupName)

	field.Name = proto.String(fieldName)
	field.JsonName = proto.String(tokenizer.ToJSONName(fieldName))
	field.Type = descriptorpb.FieldDescriptorProto_TYPE_GROUP.Enum()
	field.TypeName = proto.String(groupName)

	// = number
	if _, err := p.tok.Expect("="); err != nil {
		return nil, nil, err
	}
	numTok, err := p.tok.ExpectInt()
	if err != nil {
		return nil, nil, err
	}
	num, parseErr := parseIntLenient(numTok.Value, 0, 64)
	if parseErr != nil || num > math.MaxInt32 || num < math.MinInt32 {
		return nil, nil, fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
	}
	field.Number = proto.Int32(int32(num))

	// Optional field options [deprecated = true, etc.]
	var optionLocs []*descriptorpb.SourceCodeInfo_Location
	if p.tok.Peek().Value == "[" {
		var err error
		optionLocs, err = p.parseFieldOptions(field, fieldPath)
		if err != nil {
			return nil, nil, err
		}
	}

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, nil, err
	}

	// Source code info for the field
	fieldLocIdx := p.addLocationPlaceholder(fieldPath)
	p.attachComments(fieldLocIdx, firstIdx)

	// Label span
	p.addLocationSpan(append(copyPath(fieldPath), 4),
		labelTok.Line, labelTok.Column, labelTok.Line, labelTok.Column+len(labelTok.Value))

	// Type span (the "group" keyword) — path [5] = type
	p.addLocationSpan(append(copyPath(fieldPath), 5),
		groupTok.Line, groupTok.Column, groupTok.Line, groupTok.Column+len(groupTok.Value))

	// Name span — path [1] = name (points to the group name in source, e.g., "Result")
	p.addLocationSpan(append(copyPath(fieldPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Number span — path [3] = number
	p.addLocationSpan(append(copyPath(fieldPath), 3),
		numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))

	// Append deferred option SCI locations after number span
	p.locations = append(p.locations, optionLocs...)

	// Nested message type placeholder
	nestedLocIdx := p.addLocationPlaceholder(nestedPath)

	// Nested type name span
	p.addLocationSpan(append(copyPath(nestedPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Type name span — path [6] = type_name (same span as group name)
	p.addLocationSpan(append(copyPath(fieldPath), 6),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	// Parse group body (full message body: fields, enums, nested messages, etc.)
	nested := &descriptorpb.DescriptorProto{
		Name: proto.String(groupName),
	}
	if err := p.parseGroupBody(nested, nestedPath); err != nil {
		return nil, nil, err
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update field and nested type spans
	groupSpan := multiSpan(labelTok.Line, labelTok.Column, endTok.Line, endTok.Column+1)
	p.locations[fieldLocIdx].Span = groupSpan
	p.locations[nestedLocIdx].Span = groupSpan

	return field, nested, nil
}

func (p *parser) parseEnum(path []int32) (*descriptorpb.EnumDescriptorProto, error) {
	firstIdx := p.tok.CurrentIndex()
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
	var reservedRangeIdx, reservedNameIdx int32
	seenEnumOptions := map[string]bool{}
	for p.tok.Peek().Value != "}" {
		if p.tok.Peek().Value == ";" {
			p.tok.Next() // consume empty statement
			continue
		}
		if p.tok.Peek().Value == "option" {
			if err := p.parseEnumOption(e, path, seenEnumOptions); err != nil {
				return nil, err
			}
			continue
		}
		if p.tok.Peek().Value == "reserved" {
			if err := p.parseEnumReserved(e, path, &reservedRangeIdx, &reservedNameIdx); err != nil {
				return nil, err
			}
			continue
		}

		valFirstIdx := p.tok.CurrentIndex()
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
		var optsBracketStartLine, optsBracketStartCol, optsBracketEndLine, optsBracketEndCol int
		type enumValOptInfo struct {
			fieldNum                          int32
			nameStartLine, nameStartCol       int
			endLine, endCol                   int
			featFieldNum                      int32
		}
		var parsedEnumValOpts []enumValOptInfo
		var pendingCustEnumValOpts []CustomEnumValueOption
		bracketSkipped := false

		if p.tok.Peek().Value == "[" {
			bracketTok := p.tok.Next() // consume "["
			p.trackEnd(bracketTok)
			hasOpts = true
			optsBracketStartLine = bracketTok.Line
			optsBracketStartCol = bracketTok.Column
			seenEnumValOpts := map[string]bool{}

			for {
				optNameTok := p.tok.Next()
				p.trackEnd(optNameTok)
				optName := optNameTok.Value

				// Handle parenthesized custom option names: [(name) = value]
				if optName == "(" {
					fullName, err := p.parseParenthesizedOptionName(optNameTok)
					if err != nil {
						return nil, err
					}

					inner := fullName
					if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
						inner = inner[1 : len(inner)-1]
					}

					// Handle sub-field path: [(name).sub1.sub2 = value]
					var subFieldPath []string
					for p.tok.Peek().Value == "." {
						dotTok := p.tok.Next()
						p.trackEnd(dotTok)
						subTok := p.tok.Next()
						p.trackEnd(subTok)
						subFieldPath = append(subFieldPath, subTok.Value)
					}

					if _, err := p.tok.Expect("="); err != nil {
						return nil, err
					}

					var custOpt CustomEnumValueOption
					custOpt.ParenName = fullName
					custOpt.InnerName = inner
					custOpt.SubFieldPath = subFieldPath
					custOpt.NameTok = optNameTok

					// Reject angle bracket aggregate syntax and positive sign
					if p.tok.Peek().Value == "<" || p.tok.Peek().Value == "+" {
						rejTok := p.tok.Next()
						p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected option value.", p.filename, rejTok.Line+1, rejTok.Column+1))
						p.skipToToken("]")
						bracketSkipped = true
						break
					}

					neg := false
					if p.tok.Peek().Value == "-" {
						p.tok.Next()
						neg = true
					}
					custOpt.Negative = neg

					if p.tok.Peek().Value == "{" {
						brTok := p.tok.Next() // consume '{'
						p.trackEnd(brTok)
						aggFields, aggErr := p.consumeAggregate()
						if aggErr != nil {
							return nil, fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", brTok.Line+1, brTok.Column+1, inner, aggErr.Error())
						}
						closeTok := p.tok.Next() // consume '}'
						p.trackEnd(closeTok)
						custOpt.AggregateFields = aggFields
						custOpt.AggregateBraceTok = brTok
					} else {
						valTok := p.tok.Next()
						p.trackEnd(valTok)
						custOpt.AggregateBraceTok = valTok
						custOpt.Value = valTok.Value
						// Adjacent string concatenation
						if valTok.Type == tokenizer.TokenString {
							for p.tok.Peek().Type == tokenizer.TokenString {
								nextTok := p.tok.Next()
								p.trackEnd(nextTok)
								custOpt.Value += nextTok.Value
							}
						}
						if neg {
							custOpt.Value = "-" + custOpt.Value
						}
						custOpt.ValueType = valTok.Type
					}

					endTok2 := p.tok.Peek()
					optEndLine := endTok2.Line
					optEndCol := endTok2.Column

					if enumValOpts == nil {
						enumValOpts = &descriptorpb.EnumValueOptions{}
					}

					custOpt.SCILoc = &descriptorpb.SourceCodeInfo_Location{
						Path: []int32{0}, // placeholder path, updated during SCI emission
						Span: multiSpan(optNameTok.Line, optNameTok.Column, optEndLine, optEndCol),
					}

					pendingCustEnumValOpts = append(pendingCustEnumValOpts, custOpt)
					hasOpts = true

					next := p.tok.Peek()
					if next.Value == "," {
						p.tok.Next()
						if p.tok.Peek().Value == "]" {
							return nil, fmt.Errorf("%d:%d: Expected identifier.", p.tok.Peek().Line+1, p.tok.Peek().Column+1)
						}
						continue
					}
					if next.Value == "]" {
						break
					}
					break
				}

				// Handle dotted option names like features.enum_type
				var featSubField string
				if optName == "features" && p.tok.Peek().Value == "." {
					p.tok.Next() // consume "."
					subTok := p.tok.Next()
					featSubField = subTok.Value
					optName = "features." + featSubField
				}

				if seenEnumValOpts[optName] {
					return nil, fmt.Errorf("%d:%d: Option \"%s\" was already set.", optNameTok.Line+1, optNameTok.Column+1, optName)
				}
				seenEnumValOpts[optName] = true

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
					if optValTok.Type != tokenizer.TokenIdent {
						return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.EnumValueOptions.deprecated\".", optValTok.Line+1, optValTok.Column+1)
					}
					if optValTok.Value != "true" && optValTok.Value != "false" {
						return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.EnumValueOptions.deprecated\".", optValTok.Line+1, optValTok.Column+1)
					}
					enumValOpts.Deprecated = proto.Bool(optValTok.Value == "true")
					fieldNum = 1
				case "debug_redact":
					if optValTok.Type != tokenizer.TokenIdent {
						return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.EnumValueOptions.debug_redact\".", optValTok.Line+1, optValTok.Column+1)
					}
					if optValTok.Value != "true" && optValTok.Value != "false" {
						return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.EnumValueOptions.debug_redact\".", optValTok.Line+1, optValTok.Column+1)
					}
					enumValOpts.DebugRedact = proto.Bool(optValTok.Value == "true")
					fieldNum = 3
				default:
					if featSubField != "" {
						if enumValOpts.Features == nil {
							enumValOpts.Features = &descriptorpb.FeatureSet{}
						}
						if optValTok.Type != tokenizer.TokenIdent {
							return nil, fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.EnumValueOptions.features.%s\".", optValTok.Line+1, optValTok.Column+1, featSubField)
						}
						var featFieldNum int32
						switch featSubField {
						case "field_presence":
							v, ok := descriptorpb.FeatureSet_FieldPresence_value[optValTok.Value]
							if !ok {
								return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.EnumValueOptions.features.field_presence\".", optValTok.Line+1, optValTok.Column+1, optValTok.Value)
							}
							enumValOpts.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
							featFieldNum = 1
						case "enum_type":
							v, ok := descriptorpb.FeatureSet_EnumType_value[optValTok.Value]
							if !ok {
								return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.EnumValueOptions.features.enum_type\".", optValTok.Line+1, optValTok.Column+1, optValTok.Value)
							}
							enumValOpts.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
							featFieldNum = 2
						case "repeated_field_encoding":
							v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[optValTok.Value]
							if !ok {
								return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.EnumValueOptions.features.repeated_field_encoding\".", optValTok.Line+1, optValTok.Column+1, optValTok.Value)
							}
							enumValOpts.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
							featFieldNum = 3
						case "utf8_validation":
							v, ok := descriptorpb.FeatureSet_Utf8Validation_value[optValTok.Value]
							if !ok {
								return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.EnumValueOptions.features.utf8_validation\".", optValTok.Line+1, optValTok.Column+1, optValTok.Value)
							}
							enumValOpts.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
							featFieldNum = 4
						case "message_encoding":
							v, ok := descriptorpb.FeatureSet_MessageEncoding_value[optValTok.Value]
							if !ok {
								return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.EnumValueOptions.features.message_encoding\".", optValTok.Line+1, optValTok.Column+1, optValTok.Value)
							}
							enumValOpts.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
							featFieldNum = 5
						case "json_format":
							v, ok := descriptorpb.FeatureSet_JsonFormat_value[optValTok.Value]
							if !ok {
								return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.EnumValueOptions.features.json_format\".", optValTok.Line+1, optValTok.Column+1, optValTok.Value)
							}
							enumValOpts.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
							featFieldNum = 6
						default:
							return nil, fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", optNameTok.Line+1, optNameTok.Column+1, optName)
						}
						endCol := optValTok.Column + len(optValTok.Value)
						parsedEnumValOpts = append(parsedEnumValOpts, enumValOptInfo{
							fieldNum:      2,
							nameStartLine: optNameTok.Line,
							nameStartCol:  optNameTok.Column,
							endLine:       optValTok.Line,
							endCol:        endCol,
							featFieldNum:  featFieldNum,
						})
					} else {
						return nil, fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", optNameTok.Line+1, optNameTok.Column+1, optName)
					}
				}

				if fieldNum != 0 {
					endCol := optValTok.Column + len(optValTok.Value)
					parsedEnumValOpts = append(parsedEnumValOpts, enumValOptInfo{
						fieldNum:      fieldNum,
						nameStartLine: optNameTok.Line,
						nameStartCol:  optNameTok.Column,
						endLine:       optValTok.Line,
						endCol:        endCol,
					})
				}

				if p.tok.Peek().Value == "," {
					p.tok.Next() // consume ","
					if p.tok.Peek().Value == "]" {
						tok := p.tok.Peek()
						return nil, fmt.Errorf("%d:%d: Expected identifier.", tok.Line+1, tok.Column+1)
					}
				} else {
					break
				}
			}

			if !bracketSkipped {
				closeBracket, err := p.tok.Expect("]")
				if err != nil {
					return nil, err
				}
				p.trackEnd(closeBracket)
				optsBracketEndLine = closeBracket.Line
				optsBracketEndCol = closeBracket.Column
			}
		}

		endValTok, err := p.tok.Expect(";")
		if err != nil {
			return nil, err
		}
		p.trackEnd(endValTok)

		num, parseErr := parseIntLenient(valNumTok.Value, 0, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("%d:%d: Integer out of range.", valNumTok.Line+1, valNumTok.Column+1)
		}
		if negative {
			num = -num
		}
		if num > math.MaxInt32 || num < math.MinInt32 {
			return nil, fmt.Errorf("%d:%d: Integer out of range.", valNumTok.Line+1, valNumTok.Column+1)
		}
		evd := &descriptorpb.EnumValueDescriptorProto{
			Name:   proto.String(valNameTok.Value),
			Number: proto.Int32(int32(num)),
		}
		if enumValOpts != nil {
			evd.Options = enumValOpts
		}
		// Wire up pending custom enum value options
		for i := range pendingCustEnumValOpts {
			pendingCustEnumValOpts[i].EnumValue = evd
			if evd.Options == nil {
				evd.Options = &descriptorpb.EnumValueOptions{}
			}
		}
		p.customEnumValueOptions = append(p.customEnumValueOptions, pendingCustEnumValOpts...)
		e.Value = append(e.Value, evd)

		// Source code info for enum value
		valuePath := append(copyPath(path), 2, valueIdx)
		valueLocIdx := p.addLocationPlaceholder(valuePath)
		p.locations[valueLocIdx].Span = multiSpan(valNameTok.Line, valNameTok.Column, endValTok.Line, endValTok.Column+1)
		p.attachComments(valueLocIdx, valFirstIdx)
		// Value name - path [1]
		p.addLocationSpan(append(copyPath(valuePath), 1),
			valNameTok.Line, valNameTok.Column, valNameTok.Line, valNameTok.Column+len(valNameTok.Value))
		// Value number - path [2]
		numStartLine := valNumTok.Line
		numStartCol := valNumTok.Column
		if minusTok != nil {
			numStartLine = minusTok.Line
			numStartCol = minusTok.Column
		}
		p.addLocationSpan(append(copyPath(valuePath), 2),
			numStartLine, numStartCol, valNumTok.Line, valNumTok.Column+len(valNumTok.Value))

		// Source code info for enum value options
		if hasOpts {
			optPath := append(copyPath(valuePath), 3)
			p.addLocationSpan(optPath, optsBracketStartLine, optsBracketStartCol,
				optsBracketEndLine, optsBracketEndCol+1)
			for _, oi := range parsedEnumValOpts {
				if oi.featFieldNum != 0 {
					p.addLocationSpan(append(copyPath(optPath), oi.fieldNum, oi.featFieldNum),
						oi.nameStartLine, oi.nameStartCol, oi.endLine, oi.endCol)
				} else {
					p.addLocationSpan(append(copyPath(optPath), oi.fieldNum),
						oi.nameStartLine, oi.nameStartCol, oi.endLine, oi.endCol)
				}
			}
			// Add deferred custom enum value option SCI entries
			for i := range pendingCustEnumValOpts {
				sciLoc := pendingCustEnumValOpts[i].SCILoc
				basePath := append(copyPath(optPath), 0) // placeholder field num, resolved later
				if len(pendingCustEnumValOpts[i].SubFieldPath) > 0 {
					sciPath := make([]int32, len(basePath)+len(pendingCustEnumValOpts[i].SubFieldPath))
					copy(sciPath, basePath)
					sciLoc.Path = sciPath
				} else {
					sciLoc.Path = basePath
				}
				p.locations = append(p.locations, sciLoc)
			}
		}

		valueIdx++
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Validate allow_alias: if set to true, there must be at least one duplicate value
	if e.GetOptions().GetAllowAlias() {
		usedValues := make(map[int32]bool)
		hasDuplicates := false
		for _, v := range e.GetValue() {
			if usedValues[v.GetNumber()] {
				hasDuplicates = true
				break
			}
			usedValues[v.GetNumber()] = true
		}
		if !hasDuplicates {
			nextTok := p.tok.Peek()
			return nil, fmt.Errorf("%d:%d: \"%s\" declares support for enum aliases but no enum values share field numbers. Please remove the unnecessary 'option allow_alias = true;' declaration.",
				nextTok.Line+1, nextTok.Column+1, e.GetName())
		}
	}

	// Update enum declaration span
	p.locations[enumLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)

	p.attachComments(enumLocIdx, firstIdx)

	return e, nil
}

func (p *parser) parseEnumOption(e *descriptorpb.EnumDescriptorProto, enumPath []int32, seenOptions map[string]bool) error {
	firstIdx := p.tok.CurrentIndex()
	_ = firstIdx
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if optName == "(" {
		fullName, err := p.parseParenthesizedOptionName(nameTok)
		if err != nil {
			return err
		}
		// Extract inner name (strip parens)
		inner := fullName
		if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
			inner = inner[1 : len(inner)-1]
		}

		// Handle sub-field path: option (name).sub1.sub2... = value;
		var subFieldPath []string
		for p.tok.Peek().Value == "." {
			dotTok := p.tok.Next()
			p.trackEnd(dotTok)
			subTok := p.tok.Next()
			p.trackEnd(subTok)
			subFieldPath = append(subFieldPath, subTok.Value)
		}

		// Consume "="
		if _, err := p.tok.Expect("="); err != nil {
			return err
		}

		var custOpt CustomEnumOption
		custOpt.ParenName = fullName
		custOpt.InnerName = inner
		custOpt.SubFieldPath = subFieldPath
		custOpt.NameTok = nameTok
		custOpt.Enum = e

		// Reject angle bracket aggregate syntax and positive sign
		if p.tok.Peek().Value == "<" || p.tok.Peek().Value == "+" {
			rejTok := p.tok.Next()
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected option value.", p.filename, rejTok.Line+1, rejTok.Column+1))
			p.skipStatementCpp()
			return nil
		}

		// Read value
		if p.tok.Peek().Value == "-" {
			p.tok.Next()
			custOpt.Negative = true
		}

		if p.tok.Peek().Value == "{" {
			brTok := p.tok.Next() // consume '{'
			p.trackEnd(brTok)
			custOpt.AggregateBraceTok = brTok
			var aggErr error
			custOpt.AggregateFields, aggErr = p.consumeAggregate()
			if aggErr != nil {
				return fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", brTok.Line+1, brTok.Column+1, inner, aggErr.Error())
			}
			closeTok := p.tok.Next() // consume '}'
			p.trackEnd(closeTok)
		} else {
			valTok := p.tok.Next()
			p.trackEnd(valTok)
			custOpt.AggregateBraceTok = valTok
			val := valTok.Value
			if custOpt.Negative {
				val = "-" + val
			}
			custOpt.Value = val
			custOpt.ValueType = valTok.Type
			// Handle string concatenation
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					next := p.tok.Next()
					p.trackEnd(next)
					custOpt.Value += next.Value
				}
			}
		}

		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)

		if e.Options == nil {
			e.Options = &descriptorpb.EnumOptions{}
		}

		// SCI: [enumPath..., 3] for options statement, [enumPath..., 3, 0] placeholder
		optPath := append(copyPath(enumPath), 3)
		span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: optPath,
			Span: span,
		})
		sciPath := append(copyPath(optPath), 0) // placeholder field num
		if len(subFieldPath) > 0 {
			sciPath = make([]int32, len(optPath)+1+len(subFieldPath))
			copy(sciPath, optPath)
			// remaining elements will be resolved post-parse
		}
		sciLoc := &descriptorpb.SourceCodeInfo_Location{
			Path: sciPath,
			Span: span,
		}
		p.locations = append(p.locations, sciLoc)
		p.attachComments(len(p.locations)-1, firstIdx)
		custOpt.SCILoc = sciLoc

		p.customEnumOptions = append(p.customEnumOptions, custOpt)
		return nil
	}

	// Handle dotted option names like features.enum_type
	var featSubField string
	if optName == "features" && p.tok.Peek().Value == "." {
		dotTok := p.tok.Next()
		p.trackEnd(dotTok)
		subTok := p.tok.Next()
		p.trackEnd(subTok)
		featSubField = subTok.Value
		optName = "features." + featSubField
	}

	if seenOptions[optName] {
		return fmt.Errorf("%d:%d: Option \"%s\" was already set.", nameTok.Line+1, nameTok.Column+1, optName)
	}
	seenOptions[optName] = true

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
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.EnumOptions.allow_alias\".", valTok.Line+1, valTok.Column+1)
		}
		if valTok.Value != "true" && valTok.Value != "false" {
			return fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.EnumOptions.allow_alias\".", valTok.Line+1, valTok.Column+1)
		}
		e.Options.AllowAlias = proto.Bool(valTok.Value == "true")
		fieldNum = 2
	case "deprecated":
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.EnumOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
		}
		if valTok.Value != "true" && valTok.Value != "false" {
			return fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.EnumOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
		}
		e.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 3
	case "deprecated_legacy_json_field_conflicts":
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.EnumOptions.deprecated_legacy_json_field_conflicts\".", valTok.Line+1, valTok.Column+1)
		}
		if valTok.Value != "true" && valTok.Value != "false" {
			return fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.EnumOptions.deprecated_legacy_json_field_conflicts\".", valTok.Line+1, valTok.Column+1)
		}
		e.Options.DeprecatedLegacyJsonFieldConflicts = proto.Bool(valTok.Value == "true")
		fieldNum = 6
	default:
		if featSubField != "" {
			if e.Options.Features == nil {
				e.Options.Features = &descriptorpb.FeatureSet{}
			}
			if valTok.Type != tokenizer.TokenIdent {
				return fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.EnumOptions.features.%s\".", valTok.Line+1, valTok.Column+1, featSubField)
			}
			var featFieldNum int32
			switch featSubField {
			case "field_presence":
				v, ok := descriptorpb.FeatureSet_FieldPresence_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.EnumOptions.features.field_presence\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				e.Options.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
				featFieldNum = 1
			case "enum_type":
				v, ok := descriptorpb.FeatureSet_EnumType_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.EnumOptions.features.enum_type\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				e.Options.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
				featFieldNum = 2
			case "repeated_field_encoding":
				v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.EnumOptions.features.repeated_field_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				e.Options.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
				featFieldNum = 3
			case "utf8_validation":
				v, ok := descriptorpb.FeatureSet_Utf8Validation_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.EnumOptions.features.utf8_validation\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				e.Options.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
				featFieldNum = 4
			case "message_encoding":
				v, ok := descriptorpb.FeatureSet_MessageEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.EnumOptions.features.message_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				e.Options.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
				featFieldNum = 5
			case "json_format":
				v, ok := descriptorpb.FeatureSet_JsonFormat_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.EnumOptions.features.json_format\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				e.Options.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
				featFieldNum = 6
			default:
				return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
			}
			// SCI: [enumPath..., 3] for options statement, [enumPath..., 3, 7, featFieldNum] for specific feature
			optPath := append(copyPath(enumPath), 3)
			span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: optPath,
				Span: span,
			})
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: append(copyPath(optPath), 7, featFieldNum),
				Span: span,
			})
			p.attachComments(len(p.locations)-1, firstIdx)
			return nil
		}
		return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
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
	p.attachComments(len(p.locations)-1, firstIdx)

	return nil
}

func (p *parser) parseEnumReserved(e *descriptorpb.EnumDescriptorProto, enumPath []int32, rangeIdx, nameIdx *int32) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "reserved"

	if p.tok.Peek().Type == tokenizer.TokenString {
		// reserved "NAME1", "NAME2";
		stmtPath := append(copyPath(enumPath), 5) // field 5 = reserved_name
		for {
			nameTok, err := p.tok.ExpectString()
			if err != nil {
				return err
			}
			nameVal := nameTok.Value
			nameEndLine, nameEndCol := nameTok.Line, nameTok.Column+nameTok.RawLen
			// Adjacent string concatenation
			for p.tok.Peek().Type == tokenizer.TokenString {
				nextStr := p.tok.Next()
				nameVal += nextStr.Value
				nameEndLine = nextStr.Line
				nameEndCol = nextStr.Column + nextStr.RawLen
			}
			e.ReservedName = append(e.ReservedName, nameVal)

			p.addLocationSpan(append(copyPath(stmtPath), *nameIdx),
				nameTok.Line, nameTok.Column, nameEndLine, nameEndCol)
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
		p.addLocationSpan(stmtPath, startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		// Move statement span before individual names
		stmtLoc := p.locations[len(p.locations)-1]
		copy(p.locations[len(p.locations)-int(*nameIdx):], p.locations[len(p.locations)-int(*nameIdx)-1:len(p.locations)-1])
		p.locations[len(p.locations)-int(*nameIdx)-1] = stmtLoc
		p.attachComments(len(p.locations)-int(*nameIdx)-1, firstIdx)
	} else {
		// reserved 2, 3, 10 to 20;
		stmtPath := append(copyPath(enumPath), 4) // field 4 = reserved_range
		startCount := *rangeIdx

		// Check if the first token is an unquoted identifier (not allowed in proto2/proto3)
		if pk := p.tok.Peek(); pk.Type == tokenizer.TokenIdent && pk.Value != "max" && pk.Value != "-" {
			if p.syntax != "editions" {
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Reserved names must be string literals. (Only editions supports identifiers.)", p.filename, pk.Line+1, pk.Column+1))
				p.skipToToken(";")
				return nil
			}
		}

		for {
			// Handle negative numbers in enum reserved ranges
			startNeg := false
			var startMinusTok tokenizer.Token
			if p.tok.Peek().Value == "-" {
				startMinusTok = p.tok.Next()
				startNeg = true
			}
			numTok, err := p.tok.ExpectInt()
			if err != nil {
				return err
			}
			startNum, parseErr := parseIntLenient(numTok.Value, 0, 64)
			if parseErr != nil || startNum > math.MaxInt32 || startNum < math.MinInt32 {
				return fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
			}
			if startNeg {
				startNum = -startNum
				if startNum < math.MinInt32 {
					return fmt.Errorf("%d:%d: Integer out of range.", startMinusTok.Line+1, startMinusTok.Column+1)
				}
			}
			spanStartLine, spanStartCol := numTok.Line, numTok.Column
			if startNeg {
				spanStartLine = startMinusTok.Line
				spanStartCol = startMinusTok.Column
			}
			endNum := startNum // inclusive for enums
			endSpanLine, endSpanCol := numTok.Line, numTok.Column+len(numTok.Value)
			endNumLine, endNumCol, endNumLen := numTok.Line, numTok.Column, len(numTok.Value)
			if startNeg {
				// For single value (no "to"), C++ protoc sets end location to
				// start_token (the minus sign only). We match that here.
				endNumCol = startMinusTok.Column
				endNumLen = len(startMinusTok.Value) // just the "-" character
			}
			if p.tok.Peek().Value == "to" {
				p.tok.Next()
				if p.tok.Peek().Value == "max" {
					maxTok := p.tok.Next()
					endNum = 2147483647 // INT32_MAX for enum reserved
					endSpanLine = maxTok.Line
					endSpanCol = maxTok.Column + len(maxTok.Value)
					endNumLine = maxTok.Line
					endNumCol = maxTok.Column
					endNumLen = len(maxTok.Value)
				} else {
					// Handle negative end number
					endNeg := false
					var endMinusTok tokenizer.Token
					if p.tok.Peek().Value == "-" {
						endMinusTok = p.tok.Next()
						endNeg = true
					}
					endNumTok, err := p.tok.ExpectInt()
					if err != nil {
						return err
					}
					en, parseErr := parseIntLenient(endNumTok.Value, 0, 64)
					if parseErr != nil || en > math.MaxInt32 || en < math.MinInt32 {
						return fmt.Errorf("%d:%d: Integer out of range.", endNumTok.Line+1, endNumTok.Column+1)
					}
					if endNeg {
						en = -en
						if en < math.MinInt32 {
							return fmt.Errorf("%d:%d: Integer out of range.", endMinusTok.Line+1, endMinusTok.Column+1)
						}
					}
					endNum = en
					endSpanLine = endNumTok.Line
					endSpanCol = endNumTok.Column + len(endNumTok.Value)
					endNumLine = endNumTok.Line
					endNumCol = endNumTok.Column
					endNumLen = len(endNumTok.Value)
					if endNeg {
						endNumCol = endMinusTok.Column
						endNumLen = (endNumTok.Column + len(endNumTok.Value)) - endMinusTok.Column
					}
				}
			}

			if endNum < startNum {
				return fmt.Errorf("%d:%d: Reserved range end number must be greater than start number.", numTok.Line+1, numTok.Column+1)
			}

			e.ReservedRange = append(e.ReservedRange, &descriptorpb.EnumDescriptorProto_EnumReservedRange{
				Start: proto.Int32(int32(startNum)),
				End:   proto.Int32(int32(endNum)),
			})

			rangePath := append(copyPath(stmtPath), *rangeIdx)
			p.addLocationSpan(rangePath, spanStartLine, spanStartCol, endSpanLine, endSpanCol)
			p.addLocationSpan(append(copyPath(rangePath), 1), spanStartLine, spanStartCol, numTok.Line, numTok.Column+len(numTok.Value))
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
		// Move statement span before individual ranges
		count := int(*rangeIdx - startCount)
		stmtLoc := p.locations[len(p.locations)-1]
		copy(p.locations[len(p.locations)-count*3:], p.locations[len(p.locations)-count*3-1:len(p.locations)-1])
		p.locations[len(p.locations)-count*3-1] = stmtLoc
		p.attachComments(len(p.locations)-count*3-1, firstIdx)
	}
	return nil
}

func (p *parser) parseService(path []int32) (*descriptorpb.ServiceDescriptorProto, error) {
	firstIdx := p.tok.CurrentIndex()
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
	p.attachComments(svcLocIdx, firstIdx)
	p.addLocationSpan(append(copyPath(path), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	seenServiceOptions := map[string]bool{}
	var methodIdx int32
	for p.tok.Peek().Value != "}" {
		if p.tok.Peek().Value == ";" {
			p.tok.Next() // consume empty statement
			continue
		}
		if p.tok.Peek().Value == "option" {
			if err := p.parseServiceOption(svc, path, seenServiceOptions); err != nil {
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

func (p *parser) parseServiceOption(svc *descriptorpb.ServiceDescriptorProto, svcPath []int32, seenServiceOptions map[string]bool) error {
	firstIdx := p.tok.CurrentIndex()
	_ = firstIdx
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if optName == "(" {
		fullName, err := p.parseParenthesizedOptionName(nameTok)
		if err != nil {
			return err
		}
		inner := fullName
		if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
			inner = inner[1 : len(inner)-1]
		}

		// Handle sub-field path: option (name).sub1.sub2... = value;
		var subFieldPath []string
		for p.tok.Peek().Value == "." {
			dotTok := p.tok.Next()
			p.trackEnd(dotTok)
			subTok := p.tok.Next()
			p.trackEnd(subTok)
			subFieldPath = append(subFieldPath, subTok.Value)
		}

		if _, err := p.tok.Expect("="); err != nil {
			return err
		}

		// Reject angle bracket aggregate syntax and positive sign
		if p.tok.Peek().Value == "<" || p.tok.Peek().Value == "+" {
			rejTok := p.tok.Next()
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected option value.", p.filename, rejTok.Line+1, rejTok.Column+1))
			p.skipStatementCpp()
			return nil
		}

		var custOpt CustomServiceOption
		custOpt.ParenName = fullName
		custOpt.InnerName = inner
		custOpt.SubFieldPath = subFieldPath
		custOpt.NameTok = nameTok
		custOpt.Service = svc

		if p.tok.Peek().Value == "-" {
			p.tok.Next()
			custOpt.Negative = true
		}

		if p.tok.Peek().Value == "{" {
			brTok := p.tok.Next() // consume '{'
			p.trackEnd(brTok)
			custOpt.AggregateBraceTok = brTok
			var aggErr error
			custOpt.AggregateFields, aggErr = p.consumeAggregate()
			if aggErr != nil {
				return fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", brTok.Line+1, brTok.Column+1, inner, aggErr.Error())
			}
			closeTok := p.tok.Next() // consume '}'
			p.trackEnd(closeTok)
		} else {
			valTok := p.tok.Next()
			p.trackEnd(valTok)
			custOpt.AggregateBraceTok = valTok
			val := valTok.Value
			if custOpt.Negative {
				val = "-" + val
			}
			custOpt.Value = val
			custOpt.ValueType = valTok.Type
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					next := p.tok.Next()
					p.trackEnd(next)
					custOpt.Value += next.Value
				}
			}
		}

		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)

		if svc.Options == nil {
			svc.Options = &descriptorpb.ServiceOptions{}
		}

		optPath := append(copyPath(svcPath), 3)
		span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: optPath,
			Span: span,
		})
		sciPath := append(copyPath(optPath), 0)
		if len(subFieldPath) > 0 {
			sciPath = make([]int32, len(optPath)+1+len(subFieldPath))
			copy(sciPath, optPath)
			// remaining elements will be resolved post-parse
		}
		sciLoc := &descriptorpb.SourceCodeInfo_Location{
			Path: sciPath,
			Span: span,
		}
		p.locations = append(p.locations, sciLoc)
		p.attachComments(len(p.locations)-1, firstIdx)
		custOpt.SCILoc = sciLoc

		p.customServiceOptions = append(p.customServiceOptions, custOpt)
		return nil
	}

	// Handle dotted option names like features.json_format
	var featSubField string
	if optName == "features" && p.tok.Peek().Value == "." {
		dotTok := p.tok.Next()
		p.trackEnd(dotTok)
		subTok := p.tok.Next()
		p.trackEnd(subTok)
		featSubField = subTok.Value
		optName = "features." + featSubField
	}

	if seenServiceOptions[optName] {
		return fmt.Errorf("%d:%d: Option \"%s\" was already set.", nameTok.Line+1, nameTok.Column+1, optName)
	}
	seenServiceOptions[optName] = true

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
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.ServiceOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
		}
		if valTok.Value != "true" && valTok.Value != "false" {
			return fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.ServiceOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
		}
		svc.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 33
	default:
		if featSubField != "" {
			if svc.Options.Features == nil {
				svc.Options.Features = &descriptorpb.FeatureSet{}
			}
			if valTok.Type != tokenizer.TokenIdent {
				return fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.ServiceOptions.features.%s\".", valTok.Line+1, valTok.Column+1, featSubField)
			}
			var featFieldNum int32
			switch featSubField {
			case "field_presence":
				v, ok := descriptorpb.FeatureSet_FieldPresence_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.ServiceOptions.features.field_presence\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				svc.Options.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
				featFieldNum = 1
			case "enum_type":
				v, ok := descriptorpb.FeatureSet_EnumType_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.ServiceOptions.features.enum_type\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				svc.Options.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
				featFieldNum = 2
			case "repeated_field_encoding":
				v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.ServiceOptions.features.repeated_field_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				svc.Options.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
				featFieldNum = 3
			case "utf8_validation":
				v, ok := descriptorpb.FeatureSet_Utf8Validation_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.ServiceOptions.features.utf8_validation\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				svc.Options.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
				featFieldNum = 4
			case "message_encoding":
				v, ok := descriptorpb.FeatureSet_MessageEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.ServiceOptions.features.message_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				svc.Options.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
				featFieldNum = 5
			case "json_format":
				v, ok := descriptorpb.FeatureSet_JsonFormat_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.ServiceOptions.features.json_format\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				svc.Options.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
				featFieldNum = 6
			default:
				return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
			}
			optPath := append(copyPath(svcPath), 3)
			span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: optPath,
				Span: span,
			})
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: append(copyPath(optPath), 34, featFieldNum),
				Span: span,
			})
			p.attachComments(len(p.locations)-1, firstIdx)
			return nil
		}
		return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
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
	p.attachComments(len(p.locations)-1, firstIdx)

	return nil
}

func (p *parser) parseMethodOption(method *descriptorpb.MethodDescriptorProto, methodPath []int32, seenMethodOptions map[string]bool) error {
	firstIdx := p.tok.CurrentIndex()
	_ = firstIdx
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if optName == "(" {
		fullName, err := p.parseParenthesizedOptionName(nameTok)
		if err != nil {
			return err
		}
		inner := fullName
		if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
			inner = inner[1 : len(inner)-1]
		}

		// Handle sub-field path: option (name).sub1.sub2... = value;
		var subFieldPath []string
		for p.tok.Peek().Value == "." {
			dotTok := p.tok.Next()
			p.trackEnd(dotTok)
			subTok := p.tok.Next()
			p.trackEnd(subTok)
			subFieldPath = append(subFieldPath, subTok.Value)
		}

		if _, err := p.tok.Expect("="); err != nil {
			return err
		}

		var custOpt CustomMethodOption
		custOpt.ParenName = fullName
		custOpt.InnerName = inner
		custOpt.SubFieldPath = subFieldPath
		custOpt.NameTok = nameTok
		custOpt.Method = method

		// Reject angle bracket aggregate syntax and positive sign
		if p.tok.Peek().Value == "<" || p.tok.Peek().Value == "+" {
			rejTok := p.tok.Next()
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected option value.", p.filename, rejTok.Line+1, rejTok.Column+1))
			p.skipStatementCpp()
			return nil
		}

		if p.tok.Peek().Value == "-" {
			p.tok.Next()
			custOpt.Negative = true
		}

		if p.tok.Peek().Value == "{" {
			brTok := p.tok.Next() // consume '{'
			p.trackEnd(brTok)
			custOpt.AggregateBraceTok = brTok
			var aggErr error
			custOpt.AggregateFields, aggErr = p.consumeAggregate()
			if aggErr != nil {
				return fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", brTok.Line+1, brTok.Column+1, inner, aggErr.Error())
			}
			closeTok := p.tok.Next() // consume '}'
			p.trackEnd(closeTok)
		} else {
			valTok := p.tok.Next()
			p.trackEnd(valTok)
			custOpt.AggregateBraceTok = valTok
			val := valTok.Value
			if custOpt.Negative {
				val = "-" + val
			}
			custOpt.Value = val
			custOpt.ValueType = valTok.Type
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					next := p.tok.Next()
					p.trackEnd(next)
					custOpt.Value += next.Value
				}
			}
		}

		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)

		if method.Options == nil {
			method.Options = &descriptorpb.MethodOptions{}
		}

		optPath := append(copyPath(methodPath), 4)
		span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: optPath,
			Span: span,
		})
		sciPath := append(copyPath(optPath), 0)
		if len(subFieldPath) > 0 {
			sciPath = make([]int32, len(optPath)+1+len(subFieldPath))
			copy(sciPath, optPath)
		}
		sciLoc := &descriptorpb.SourceCodeInfo_Location{
			Path: sciPath,
			Span: span,
		}
		p.locations = append(p.locations, sciLoc)
		p.attachComments(len(p.locations)-1, firstIdx)
		custOpt.SCILoc = sciLoc

		p.customMethodOptions = append(p.customMethodOptions, custOpt)
		return nil
	}

	// Handle dotted option names like features.json_format
	var featSubField string
	if optName == "features" && p.tok.Peek().Value == "." {
		dotTok := p.tok.Next()
		p.trackEnd(dotTok)
		subTok := p.tok.Next()
		p.trackEnd(subTok)
		featSubField = subTok.Value
		optName = "features." + featSubField
	}

	if seenMethodOptions[optName] {
		return fmt.Errorf("%d:%d: Option \"%s\" was already set.", nameTok.Line+1, nameTok.Column+1, optName)
	}
	seenMethodOptions[optName] = true

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
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.MethodOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
		}
		if valTok.Value != "true" && valTok.Value != "false" {
			return fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.MethodOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
		}
		method.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 33
	case "idempotency_level":
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.MethodOptions.idempotency_level\".", valTok.Line+1, valTok.Column+1)
		}
		var lvl descriptorpb.MethodOptions_IdempotencyLevel
		switch valTok.Value {
		case "IDEMPOTENCY_UNKNOWN":
			lvl = descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
		case "NO_SIDE_EFFECTS":
			lvl = descriptorpb.MethodOptions_NO_SIDE_EFFECTS
		case "IDEMPOTENT":
			lvl = descriptorpb.MethodOptions_IDEMPOTENT
		default:
			return fmt.Errorf("%d:%d: Enum type \"google.protobuf.MethodOptions.IdempotencyLevel\" has no value named \"%s\" for option \"google.protobuf.MethodOptions.idempotency_level\".", valTok.Line+1, valTok.Column+1, valTok.Value)
		}
		method.Options.IdempotencyLevel = lvl.Enum()
		fieldNum = 34
	default:
		if featSubField != "" {
			if method.Options.Features == nil {
				method.Options.Features = &descriptorpb.FeatureSet{}
			}
			if valTok.Type != tokenizer.TokenIdent {
				return fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.MethodOptions.features.%s\".", valTok.Line+1, valTok.Column+1, featSubField)
			}
			var featFieldNum int32
			switch featSubField {
			case "field_presence":
				v, ok := descriptorpb.FeatureSet_FieldPresence_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.MethodOptions.features.field_presence\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				method.Options.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
				featFieldNum = 1
			case "enum_type":
				v, ok := descriptorpb.FeatureSet_EnumType_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.MethodOptions.features.enum_type\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				method.Options.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
				featFieldNum = 2
			case "repeated_field_encoding":
				v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.MethodOptions.features.repeated_field_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				method.Options.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
				featFieldNum = 3
			case "utf8_validation":
				v, ok := descriptorpb.FeatureSet_Utf8Validation_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.MethodOptions.features.utf8_validation\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				method.Options.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
				featFieldNum = 4
			case "message_encoding":
				v, ok := descriptorpb.FeatureSet_MessageEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.MethodOptions.features.message_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				method.Options.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
				featFieldNum = 5
			case "json_format":
				v, ok := descriptorpb.FeatureSet_JsonFormat_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.MethodOptions.features.json_format\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				method.Options.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
				featFieldNum = 6
			default:
				return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
			}
			optPath := append(copyPath(methodPath), 4)
			span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: optPath,
				Span: span,
			})
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: append(copyPath(optPath), 35, featFieldNum),
				Span: span,
			})
			p.attachComments(len(p.locations)-1, firstIdx)
			return nil
		}
		return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
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
	p.attachComments(len(p.locations)-1, firstIdx)

	return nil
}

func (p *parser) parseMethod(path []int32) (*descriptorpb.MethodDescriptorProto, error) {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "rpc"
	if startTok.Value != "rpc" {
		return nil, fmt.Errorf("%d:%d: Expected \"rpc\".", startTok.Line+1, startTok.Column+1)
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
		// After consuming "stream" keyword, next must be a type name
		if next := p.tok.Peek(); next.Type != tokenizer.TokenIdent && next.Value != "." {
			return nil, fmt.Errorf("%d:%d: Expected type name.", next.Line+1, next.Column+1)
		}
	}
	inputTok := p.tok.Next()
	inputEndTok := inputTok
	inputType := inputTok.Value
	if inputType == "." {
		part := p.tok.Next()
		inputType += part.Value
		inputEndTok = part
	}
	for p.tok.Peek().Value == "." {
		p.tok.Next()
		part := p.tok.Next()
		inputType += "." + part.Value
		inputEndTok = part
	}
	inputEndCol := inputEndTok.Column + len(inputEndTok.Value)

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
		// After consuming "stream" keyword, next must be a type name
		if next := p.tok.Peek(); next.Type != tokenizer.TokenIdent && next.Value != "." {
			return nil, fmt.Errorf("%d:%d: Expected type name.", next.Line+1, next.Column+1)
		}
	}
	outputTok := p.tok.Next()
	outputEndTok := outputTok
	outputType := outputTok.Value
	if outputType == "." {
		part := p.tok.Next()
		outputType += part.Value
		outputEndTok = part
	}
	for p.tok.Peek().Value == "." {
		p.tok.Next()
		part := p.tok.Next()
		outputType += "." + part.Value
		outputEndTok = part
	}
	outputEndCol := outputEndTok.Column + len(outputEndTok.Value)

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
		inputTok.Line, inputTok.Column, inputEndTok.Line, inputEndCol)
	if serverStreaming {
		p.addLocationSpan(append(copyPath(path), 6),
			serverStreamTok.Line, serverStreamTok.Column, serverStreamTok.Line, serverStreamTok.Column+len("stream"))
	}
	p.addLocationSpan(append(copyPath(path), 3),
		outputTok.Line, outputTok.Column, outputEndTok.Line, outputEndCol)

	var endTok tokenizer.Token
	if p.tok.Peek().Value == "{" {
		p.tok.Next()
		if method.Options == nil {
			method.Options = &descriptorpb.MethodOptions{}
		}
		seenMethodOptions := map[string]bool{}
		for p.tok.Peek().Value != "}" {
			if p.tok.Peek().Value == ";" {
				p.tok.Next()
				continue
			}
			if p.tok.Peek().Value == "option" {
				if err := p.parseMethodOption(method, path, seenMethodOptions); err != nil {
					return nil, err
				}
			} else {
				tok := p.tok.Peek()
				return nil, fmt.Errorf("%d:%d: Expected \"option\".", tok.Line+1, tok.Column+1)
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
	p.attachComments(methodLocIdx, firstIdx)

	return method, nil
}

func (p *parser) parseOneof(msgPath []int32, oneofIdx int32, fieldIdx *int32, nestedMsgIdx *int32) ([]*descriptorpb.FieldDescriptorProto, []*descriptorpb.DescriptorProto, *descriptorpb.OneofDescriptorProto, error) {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "oneof"
	nameTok, err := p.tok.ExpectIdent()
	if err != nil {
		return nil, nil, nil, err
	}

	oneofPath := append(copyPath(msgPath), 8, oneofIdx)

	if _, err := p.tok.Expect("{"); err != nil {
		return nil, nil, nil, err
	}

	// Add oneof declaration and name spans BEFORE fields (C++ order)
	oneofLocIdx := p.addLocationPlaceholder(oneofPath)
	p.addLocationSpan(append(copyPath(oneofPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))

	decl := &descriptorpb.OneofDescriptorProto{
		Name: proto.String(nameTok.Value),
	}

	var fields []*descriptorpb.FieldDescriptorProto
	var nestedTypes []*descriptorpb.DescriptorProto
	if p.tok.Peek().Value == "}" {
		tok := p.tok.Peek()
		return nil, nil, nil, fmt.Errorf("%d:%d: Expected type name.", tok.Line+1, tok.Column+1)
	}
	for p.tok.Peek().Value != "}" {
		if p.tok.Peek().Value == ";" {
			tok := p.tok.Peek()
			return nil, nil, nil, fmt.Errorf("%d:%d: Expected type name.", tok.Line+1, tok.Column+1)
		}
		if p.tok.Peek().Value == "option" {
			err := p.parseOneofOption(oneofPath, decl)
			if err != nil {
				if errors.Is(err, errBreakOneof) {
					return nil, nil, nil, errBreakOneof
				}
				return nil, nil, nil, err
			}
			continue
		}
		if p.tok.Peek().Value == "map" && p.tok.PeekAt(1).Value == "<" {
			p.tok.Next() // consume "map"
			ltTok := p.tok.Peek()
			return nil, nil, nil, fmt.Errorf("%d:%d: Map fields are not allowed in oneofs.", ltTok.Line+1, ltTok.Column+1)
		}
		if v := p.tok.Peek().Value; v == "required" || v == "optional" || v == "repeated" {
			tok := p.tok.Peek()
			return nil, nil, nil, fmt.Errorf("%d:%d: Fields in oneofs must not have labels (required / optional / repeated).", tok.Line+1, tok.Column+1)
		}
		if p.tok.Peek().Value == "group" {
			field, nested, err := p.parseGroupFieldInOneof(msgPath, *fieldIdx, *nestedMsgIdx)
			if err != nil {
				return nil, nil, nil, err
			}
			field.OneofIndex = proto.Int32(oneofIdx)
			fields = append(fields, field)
			nestedTypes = append(nestedTypes, nested)
			*fieldIdx++
			*nestedMsgIdx++
		} else {
			fieldPath := append(copyPath(msgPath), 2, *fieldIdx)
			p.inOneof = true
			field, err := p.parseField(fieldPath)
			p.inOneof = false
			if err != nil {
				return nil, nil, nil, err
			}
			field.OneofIndex = proto.Int32(oneofIdx)
			fields = append(fields, field)
			*fieldIdx++
		}
	}

	endTok := p.tok.Next() // consume "}"
	p.trackEnd(endTok)

	// Update oneof declaration span
	p.locations[oneofLocIdx].Span = multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
	p.attachComments(oneofLocIdx, firstIdx)

	return fields, nestedTypes, decl, nil
}

func (p *parser) parseOneofOption(oneofPath []int32, decl *descriptorpb.OneofDescriptorProto) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	if optName == "(" {
		fullName, err := p.parseParenthesizedOptionName(nameTok)
		if err != nil {
			return err
		}
		inner := fullName
		if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
			inner = inner[1 : len(inner)-1]
		}

		// Handle sub-field path: option (name).sub1.sub2... = value;
		var subFieldPath []string
		for p.tok.Peek().Value == "." {
			dotTok := p.tok.Next()
			p.trackEnd(dotTok)
			subTok := p.tok.Next()
			p.trackEnd(subTok)
			subFieldPath = append(subFieldPath, subTok.Value)
		}

		if _, err := p.tok.Expect("="); err != nil {
			return err
		}

		var custOpt CustomOneofOption
		custOpt.ParenName = fullName
		custOpt.InnerName = inner
		custOpt.SubFieldPath = subFieldPath
		custOpt.NameTok = nameTok
		custOpt.Oneof = decl

		// Reject angle bracket aggregate syntax and positive sign
		if p.tok.Peek().Value == "<" || p.tok.Peek().Value == "+" {
			rejTok := p.tok.Next()
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected option value.", p.filename, rejTok.Line+1, rejTok.Column+1))
			p.skipStatementCpp()
			return errBreakOneof
		}

		if p.tok.Peek().Value == "-" {
			p.tok.Next()
			custOpt.Negative = true
		}

		if p.tok.Peek().Value == "{" {
			brTok := p.tok.Next() // consume '{'
			p.trackEnd(brTok)
			custOpt.AggregateBraceTok = brTok
			var aggErr error
			custOpt.AggregateFields, aggErr = p.consumeAggregate()
			if aggErr != nil {
				return fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", brTok.Line+1, brTok.Column+1, inner, aggErr.Error())
			}
			closeTok := p.tok.Next() // consume '}'
			p.trackEnd(closeTok)
		} else {
			valTok := p.tok.Next()
			p.trackEnd(valTok)
			custOpt.AggregateBraceTok = valTok
			val := valTok.Value
			if custOpt.Negative {
				val = "-" + val
			}
			custOpt.Value = val
			custOpt.ValueType = valTok.Type
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					next := p.tok.Next()
					p.trackEnd(next)
					custOpt.Value += next.Value
				}
			}
		}

		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)

		if decl.Options == nil {
			decl.Options = &descriptorpb.OneofOptions{}
		}

		// SCI: [oneofPath..., 2] for options statement, [oneofPath..., 2, 0] placeholder
		optPath := append(copyPath(oneofPath), 2)
		span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: optPath,
			Span: span,
		})
		sciPath := append(copyPath(optPath), 0)
		if len(subFieldPath) > 0 {
			sciPath = make([]int32, len(optPath)+1+len(subFieldPath))
			copy(sciPath, optPath)
		}
		sciLoc := &descriptorpb.SourceCodeInfo_Location{
			Path: sciPath,
			Span: span,
		}
		p.locations = append(p.locations, sciLoc)
		p.attachComments(len(p.locations)-1, firstIdx)
		custOpt.SCILoc = sciLoc

		p.customOneofOptions = append(p.customOneofOptions, custOpt)
		return nil
	}

	var featSubField string
	if optName == "features" && p.tok.Peek().Value == "." {
		dotTok := p.tok.Next()
		p.trackEnd(dotTok)
		subTok := p.tok.Next()
		p.trackEnd(subTok)
		featSubField = subTok.Value
		optName = "features." + featSubField
	}

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

	if featSubField != "" {
		if decl.Options == nil {
			decl.Options = &descriptorpb.OneofOptions{}
		}
		if decl.Options.Features == nil {
			decl.Options.Features = &descriptorpb.FeatureSet{}
		}
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.OneofOptions.features.%s\".", valTok.Line+1, valTok.Column+1, featSubField)
		}
		var featFieldNum int32
		switch featSubField {
		case "field_presence":
			v, ok := descriptorpb.FeatureSet_FieldPresence_value[valTok.Value]
			if !ok {
				return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.OneofOptions.features.field_presence\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
			decl.Options.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
			featFieldNum = 1
		case "enum_type":
			v, ok := descriptorpb.FeatureSet_EnumType_value[valTok.Value]
			if !ok {
				return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.OneofOptions.features.enum_type\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
			decl.Options.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
			featFieldNum = 2
		case "repeated_field_encoding":
			v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[valTok.Value]
			if !ok {
				return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.OneofOptions.features.repeated_field_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
			decl.Options.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
			featFieldNum = 3
		case "utf8_validation":
			v, ok := descriptorpb.FeatureSet_Utf8Validation_value[valTok.Value]
			if !ok {
				return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.OneofOptions.features.utf8_validation\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
			decl.Options.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
			featFieldNum = 4
		case "message_encoding":
			v, ok := descriptorpb.FeatureSet_MessageEncoding_value[valTok.Value]
			if !ok {
				return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.OneofOptions.features.message_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
			decl.Options.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
			featFieldNum = 5
		case "json_format":
			v, ok := descriptorpb.FeatureSet_JsonFormat_value[valTok.Value]
			if !ok {
				return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.OneofOptions.features.json_format\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
			decl.Options.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
			featFieldNum = 6
		default:
			return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
		}
		// SCI: [oneofPath..., 2] for options statement, [oneofPath..., 2, 1, featFieldNum] for specific feature
		optPath := append(copyPath(oneofPath), 2)
		span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: optPath,
			Span: span,
		})
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: append(copyPath(optPath), 1, featFieldNum),
			Span: span,
		})
		p.attachComments(len(p.locations)-1, firstIdx)
		return nil
	}

	return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
}

func (p *parser) parseMapField(msgPath []int32, fieldIdx, nestedMsgIdx int32) (*descriptorpb.FieldDescriptorProto, *descriptorpb.DescriptorProto, error) {
	fieldPath := append(copyPath(msgPath), 2, fieldIdx)

	firstIdx := p.tok.CurrentIndex()
	mapTok := p.tok.Next() // consume "map"
	startLine, startCol := mapTok.Line, mapTok.Column

	if _, err := p.tok.Expect("<"); err != nil {
		return nil, nil, err
	}

	keyTypeTok := p.tok.Next()
	keyType, keyIsBuiltin := builtinTypes[keyTypeTok.Value]
	var keyTypeName string
	if !keyIsBuiltin {
		// Non-builtin key type (e.g., enum) — store as TYPE_MESSAGE initially,
		// type resolution will update to TYPE_ENUM if appropriate.
		keyType = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
		keyTypeName = keyTypeTok.Value
		if keyTypeName == "." {
			part := p.tok.Next()
			keyTypeName += part.Value
		}
		for p.tok.Peek().Value == "." {
			p.tok.Next()
			part := p.tok.Next()
			keyTypeName += "." + part.Value
		}
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
		if valTypeName == "." {
			part := p.tok.Next()
			valTypeName += part.Value
		}
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
	num, parseErr := parseIntLenient(numTok.Value, 0, 64)
	if parseErr != nil || num > math.MaxInt32 || num < math.MinInt32 {
		return nil, nil, fmt.Errorf("%d:%d: Integer out of range.", numTok.Line+1, numTok.Column+1)
	}

	// Build entry type name
	entryName := toCamelCase(nameTok.Value) + "Entry"

	// Create the field early so parseFieldOptions can set options on it
	field := &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(nameTok.Value),
		Number:   proto.Int32(int32(num)),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(entryName),
		JsonName: proto.String(tokenizer.ToJSONName(nameTok.Value)),
	}

	// Optional field options [deprecated = true, etc.]
	var optionLocs []*descriptorpb.SourceCodeInfo_Location
	if p.tok.Peek().Value == "[" {
		var err error
		optionLocs, err = p.parseFieldOptions(field, fieldPath)
		if err != nil {
			return nil, nil, err
		}
	}

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return nil, nil, err
	}
	p.trackEnd(endTok)

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

	if keyTypeName != "" {
		entry.Field[0].TypeName = proto.String(keyTypeName)
	}

	if valTypeName != "" {
		entry.Field[1].Type = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
		entry.Field[1].TypeName = proto.String(valTypeName)
	} else {
		entry.Field[1].Type = valType.Enum()
	}

	// Source code info
	p.addLocationSpan(fieldPath, startLine, startCol, endTok.Line, endTok.Column+1)
	fieldLocIdx := len(p.locations) - 1
	p.attachComments(fieldLocIdx, firstIdx)
	p.addLocationSpan(append(copyPath(fieldPath), 6),
		startLine, startCol, gtTok.Line, typeNameEndCol)
	p.addLocationSpan(append(copyPath(fieldPath), 1),
		nameTok.Line, nameTok.Column, nameTok.Line, nameTok.Column+len(nameTok.Value))
	p.addLocationSpan(append(copyPath(fieldPath), 3),
		numTok.Line, numTok.Column, numTok.Line, numTok.Column+len(numTok.Value))

	// Option source code info (after number, matching C++ order)
	p.locations = append(p.locations, optionLocs...)

	return field, entry, nil
}

func (p *parser) parseFileOption(fd *descriptorpb.FileDescriptorProto) error {
	firstIdx := p.tok.CurrentIndex()
	startTok := p.tok.Next() // consume "option"
	p.trackEnd(startTok)

	nameTok := p.tok.Next()
	p.trackEnd(nameTok)
	optName := nameTok.Value

	// Handle parenthesized custom option names: option (name) = value;
	if optName == "(" {
		fullName, err := p.parseParenthesizedOptionName(nameTok)
		if err != nil {
			return err
		}
		// Extract inner name (strip parens)
		innerName := fullName[1 : len(fullName)-1]

		// Handle sub-field path: option (name).sub1.sub2... = value;
		var subFieldPath []string
		for p.tok.Peek().Value == "." {
			dotTok := p.tok.Next()
			p.trackEnd(dotTok)
			subTok := p.tok.Next()
			p.trackEnd(subTok)
			subFieldPath = append(subFieldPath, subTok.Value)
		}

		if _, err := p.tok.Expect("="); err != nil {
			return err
		}
		valTok := p.tok.Next()
		p.trackEnd(valTok)

		// Reject angle bracket aggregate syntax and positive sign — C++ protoc doesn't support them for options
		if valTok.Value == "<" || valTok.Value == "+" {
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected option value.", p.filename, valTok.Line+1, valTok.Column+1))
			p.skipStatementCpp()
			return nil
		}

		var aggregateFields []AggregateField
		negative := false

		if valTok.Value == "-" {
			negative = true
			valTok = p.tok.Next()
			p.trackEnd(valTok)
		}

		if valTok.Value == "{" {
			// Aggregate (message literal) value
			var aggErr error
			aggregateFields, aggErr = p.consumeAggregate()
			if aggErr != nil {
				return fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", valTok.Line+1, valTok.Column+1, innerName, aggErr.Error())
			}
			closeTok, err := p.tok.Expect("}")
			if err != nil {
				return err
			}
			p.trackEnd(closeTok)
		} else {
			// Handle string concatenation
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					next := p.tok.Next()
					p.trackEnd(next)
					valTok.Value += next.Value
				}
			}
		}
		endTok, err := p.tok.Expect(";")
		if err != nil {
			return err
		}
		p.trackEnd(endTok)

		if fd.Options == nil {
			fd.Options = &descriptorpb.FileOptions{}
		}

		span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: []int32{8},
			Span: span,
		})
		sciIdx := len(p.locations)
		sciPath := []int32{8, 0}
		if len(subFieldPath) > 0 {
			sciPath = make([]int32, 2+len(subFieldPath))
			sciPath[0] = 8
			// remaining elements will be resolved later
		}
		p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
			Path: sciPath,
			Span: span,
		})
		p.attachComments(len(p.locations)-1, firstIdx)

		p.customFileOptions = append(p.customFileOptions, CustomFileOption{
			ParenName:         fullName,
			InnerName:         innerName,
			SubFieldPath:      subFieldPath,
			Value:             valTok.Value,
			ValueType:         valTok.Type,
			Negative:          negative,
			SCIIndex:          sciIdx,
			NameTok:           nameTok,
			AggregateFields:   aggregateFields,
			AggregateBraceTok: valTok,
		})
		return nil
	}

	// Handle dotted option names like features.field_presence
	var featSubField string
	if optName == "features" && p.tok.Peek().Value == "." {
		dotTok := p.tok.Next()
		p.trackEnd(dotTok)
		subTok := p.tok.Next()
		p.trackEnd(subTok)
		featSubField = subTok.Value
		optName = "features." + featSubField
	}

	if p.seenFileOptions[optName] {
		return fmt.Errorf("%d:%d: Option \"%s\" was already set.", nameTok.Line+1, nameTok.Column+1, optName)
	}
	p.seenFileOptions[optName] = true

	if _, err := p.tok.Expect("="); err != nil {
		return err
	}

	valTok := p.tok.Next()
	p.trackEnd(valTok)

	// Concatenate adjacent string tokens (C++ protoc allows this)
	if valTok.Type == tokenizer.TokenString {
		for p.tok.Peek().Type == tokenizer.TokenString {
			next := p.tok.Next()
			p.trackEnd(next)
			valTok.Value += next.Value
		}
	}

	endTok, err := p.tok.Expect(";")
	if err != nil {
		return err
	}
	p.trackEnd(endTok)

	if fd.Options == nil {
		fd.Options = &descriptorpb.FileOptions{}
	}

	// Helper to validate boolean option values
	validateBool := func(name string) error {
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.FileOptions.%s\".", valTok.Line+1, valTok.Column+1, name)
		}
		if valTok.Value != "true" && valTok.Value != "false" {
			return fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.FileOptions.%s\".", valTok.Line+1, valTok.Column+1, name)
		}
		return nil
	}

	// Helper to validate string option values
	validateString := func(name string) error {
		if valTok.Type != tokenizer.TokenString {
			return fmt.Errorf("%d:%d: Value must be quoted string for string option \"google.protobuf.FileOptions.%s\".", valTok.Line+1, valTok.Column+1, name)
		}
		return nil
	}

	// Map option name to FileOptions field number
	var fieldNum int32
	switch optName {
	case "java_package":
		if err := validateString("java_package"); err != nil {
			return err
		}
		fd.Options.JavaPackage = proto.String(valTok.Value)
		fieldNum = 1
	case "java_outer_classname":
		if err := validateString("java_outer_classname"); err != nil {
			return err
		}
		fd.Options.JavaOuterClassname = proto.String(valTok.Value)
		fieldNum = 8
	case "java_multiple_files":
		if err := validateBool("java_multiple_files"); err != nil {
			return err
		}
		fd.Options.JavaMultipleFiles = proto.Bool(valTok.Value == "true")
		fieldNum = 10
	case "go_package":
		if err := validateString("go_package"); err != nil {
			return err
		}
		fd.Options.GoPackage = proto.String(valTok.Value)
		fieldNum = 11
	case "optimize_for":
		if valTok.Type != tokenizer.TokenIdent {
			return fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.FileOptions.optimize_for\".", valTok.Line+1, valTok.Column+1)
		}
		switch valTok.Value {
		case "SPEED":
			fd.Options.OptimizeFor = descriptorpb.FileOptions_SPEED.Enum()
		case "CODE_SIZE":
			fd.Options.OptimizeFor = descriptorpb.FileOptions_CODE_SIZE.Enum()
		case "LITE_RUNTIME":
			fd.Options.OptimizeFor = descriptorpb.FileOptions_LITE_RUNTIME.Enum()
		default:
			return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FileOptions.OptimizeMode\" has no value named \"%s\" for option \"google.protobuf.FileOptions.optimize_for\".", valTok.Line+1, valTok.Column+1, valTok.Value)
		}
		fieldNum = 9
	case "cc_generic_services":
		if err := validateBool("cc_generic_services"); err != nil {
			return err
		}
		fd.Options.CcGenericServices = proto.Bool(valTok.Value == "true")
		fieldNum = 16
	case "java_generic_services":
		if err := validateBool("java_generic_services"); err != nil {
			return err
		}
		fd.Options.JavaGenericServices = proto.Bool(valTok.Value == "true")
		fieldNum = 17
	case "py_generic_services":
		if err := validateBool("py_generic_services"); err != nil {
			return err
		}
		fd.Options.PyGenericServices = proto.Bool(valTok.Value == "true")
		fieldNum = 18
	case "deprecated":
		if err := validateBool("deprecated"); err != nil {
			return err
		}
		fd.Options.Deprecated = proto.Bool(valTok.Value == "true")
		fieldNum = 23
	case "java_string_check_utf8":
		if err := validateBool("java_string_check_utf8"); err != nil {
			return err
		}
		fd.Options.JavaStringCheckUtf8 = proto.Bool(valTok.Value == "true")
		fieldNum = 27
	case "java_generate_equals_and_hash":
		if err := validateBool("java_generate_equals_and_hash"); err != nil {
			return err
		}
		fd.Options.JavaGenerateEqualsAndHash = proto.Bool(valTok.Value == "true")
		fieldNum = 20
	case "cc_enable_arenas":
		if err := validateBool("cc_enable_arenas"); err != nil {
			return err
		}
		fd.Options.CcEnableArenas = proto.Bool(valTok.Value == "true")
		fieldNum = 31
	case "php_namespace":
		if err := validateString("php_namespace"); err != nil {
			return err
		}
		fd.Options.PhpNamespace = proto.String(valTok.Value)
		fieldNum = 41
	case "php_class_prefix":
		if err := validateString("php_class_prefix"); err != nil {
			return err
		}
		fd.Options.PhpClassPrefix = proto.String(valTok.Value)
		fieldNum = 40
	case "php_metadata_namespace":
		if err := validateString("php_metadata_namespace"); err != nil {
			return err
		}
		fd.Options.PhpMetadataNamespace = proto.String(valTok.Value)
		fieldNum = 44
	case "ruby_package":
		if err := validateString("ruby_package"); err != nil {
			return err
		}
		fd.Options.RubyPackage = proto.String(valTok.Value)
		fieldNum = 45
	case "objc_class_prefix":
		if err := validateString("objc_class_prefix"); err != nil {
			return err
		}
		fd.Options.ObjcClassPrefix = proto.String(valTok.Value)
		fieldNum = 36
	case "csharp_namespace":
		if err := validateString("csharp_namespace"); err != nil {
			return err
		}
		fd.Options.CsharpNamespace = proto.String(valTok.Value)
		fieldNum = 37
	case "swift_prefix":
		if err := validateString("swift_prefix"); err != nil {
			return err
		}
		fd.Options.SwiftPrefix = proto.String(valTok.Value)
		fieldNum = 39
	default:
		if featSubField != "" {
			// Handle features.X options
			if fd.Options.Features == nil {
				fd.Options.Features = &descriptorpb.FeatureSet{}
			}
			var featFieldNum int32
			switch featSubField {
			case "field_presence":
				v, ok := descriptorpb.FeatureSet_FieldPresence_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.FileOptions.features.field_presence\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				fd.Options.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
				featFieldNum = 1
			case "enum_type":
				v, ok := descriptorpb.FeatureSet_EnumType_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.FileOptions.features.enum_type\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				fd.Options.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
				featFieldNum = 2
			case "repeated_field_encoding":
				v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.FileOptions.features.repeated_field_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				fd.Options.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
				featFieldNum = 3
			case "utf8_validation":
				v, ok := descriptorpb.FeatureSet_Utf8Validation_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.FileOptions.features.utf8_validation\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				fd.Options.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
				featFieldNum = 4
			case "message_encoding":
				v, ok := descriptorpb.FeatureSet_MessageEncoding_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.FileOptions.features.message_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				fd.Options.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
				featFieldNum = 5
			case "json_format":
				v, ok := descriptorpb.FeatureSet_JsonFormat_value[valTok.Value]
				if !ok {
					return fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.FileOptions.features.json_format\".", valTok.Line+1, valTok.Column+1, valTok.Value)
				}
				fd.Options.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
				featFieldNum = 6
			default:
				return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
			}
			// SCI: [8] for option statement, [8, 50, featFieldNum] for specific feature
			span := multiSpan(startTok.Line, startTok.Column, endTok.Line, endTok.Column+1)
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: []int32{8},
				Span: span,
			})
			p.locations = append(p.locations, &descriptorpb.SourceCodeInfo_Location{
				Path: []int32{8, 50, featFieldNum},
				Span: span,
			})
			p.attachComments(len(p.locations)-1, firstIdx)
			return nil
		}
		return fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", nameTok.Line+1, nameTok.Column+1, optName)
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
	p.attachComments(len(p.locations)-1, firstIdx)

	return nil
}

func (p *parser) parseParenthesizedOptionName(openTok tokenizer.Token) (string, error) {
	innerTok := p.tok.Next()
	p.trackEnd(innerTok)
	fullName := "(" + innerTok.Value
	// Handle leading dot for fully-qualified names like (.pkg.ext)
	if innerTok.Value == "." {
		partTok := p.tok.Next()
		p.trackEnd(partTok)
		fullName += partTok.Value
	}
	for p.tok.Peek().Value == "." {
		dotTok := p.tok.Next()
		p.trackEnd(dotTok)
		partTok := p.tok.Next()
		p.trackEnd(partTok)
		fullName += "." + partTok.Value
	}
	closeTok, err := p.tok.Expect(")")
	if err != nil {
		return "", err
	}
	p.trackEnd(closeTok)
	fullName += ")"
	return fullName, nil
}

// consumeAggregate reads key:value pairs inside a message literal { ... }.
// Called after consuming '{'. Stops before '}'.
func (p *parser) consumeAggregate() ([]AggregateField, error) {
	var fields []AggregateField
	for p.tok.Peek().Value != "}" && p.tok.Peek().Type != tokenizer.TokenEOF {
		isExtension := false
		var fieldName string

		if p.tok.Peek().Value == "[" {
			// Extension field reference or Any type URL: [ext.name] or [type.googleapis.com/pkg.Msg]
			bracketTok := p.tok.Next() // consume '['
			p.trackEnd(bracketTok)
			isExtension = true
			nameTok := p.tok.Next()
			p.trackEnd(nameTok)
			fieldName = nameTok.Value
			for p.tok.Peek().Value == "." || p.tok.Peek().Value == "/" {
				sepTok := p.tok.Next()
				p.trackEnd(sepTok)
				partTok := p.tok.Next()
				p.trackEnd(partTok)
				fieldName += sepTok.Value + partTok.Value
			}
			closeBracket := p.tok.Next() // consume ']'
			p.trackEnd(closeBracket)
		} else {
			keyTok := p.tok.Next()
			p.trackEnd(keyTok)
			fieldName = keyTok.Value
		}

		// Expect ':' separator (required for scalars, optional for message literals)
		if p.tok.Peek().Value == ":" {
			colonTok := p.tok.Next()
			p.trackEnd(colonTok)
		} else if p.tok.Peek().Value != "{" && p.tok.Peek().Value != "<" {
			nextTok := p.tok.Peek()
			raw := nextTok.Value
			if nextTok.Type == tokenizer.TokenString {
				raw = "\"" + raw + "\""
			}
			return nil, fmt.Errorf("Expected \":\", found \"%s\".", raw)
		}

		// Read value — may be nested message literal { ... } or < ... > or scalar
		if p.tok.Peek().Value == "{" {
			brTok := p.tok.Next() // consume '{'
			p.trackEnd(brTok)
			subFields, err := p.consumeAggregate()
			if err != nil {
				return nil, err
			}
			closeTok := p.tok.Next() // consume '}'
			p.trackEnd(closeTok)
			fields = append(fields, AggregateField{
				Name:        fieldName,
				SubFields:   subFields,
				IsExtension: isExtension,
			})
		} else if p.tok.Peek().Value == "<" {
			brTok := p.tok.Next() // consume '<'
			p.trackEnd(brTok)
			subFields, err := p.consumeAggregateAngle()
			if err != nil {
				return nil, err
			}
			closeTok := p.tok.Next() // consume '>'
			p.trackEnd(closeTok)
			fields = append(fields, AggregateField{
				Name:        fieldName,
				SubFields:   subFields,
				IsExtension: isExtension,
			})
		} else if p.tok.Peek().Value == "[" {
			// List syntax for repeated fields: field: [val1, val2, val3]
			brTok := p.tok.Next() // consume '['
			p.trackEnd(brTok)
			for p.tok.Peek().Value != "]" && p.tok.Peek().Type != tokenizer.TokenEOF {
				if p.tok.Peek().Value == "{" {
					subBr := p.tok.Next()
					p.trackEnd(subBr)
					subFields, err := p.consumeAggregate()
					if err != nil {
						return nil, err
					}
					subClose := p.tok.Next()
					p.trackEnd(subClose)
					fields = append(fields, AggregateField{
						Name:        fieldName,
						SubFields:   subFields,
						IsExtension: isExtension,
					})
				} else if p.tok.Peek().Value == "<" {
					subBr := p.tok.Next()
					p.trackEnd(subBr)
					subFields, err := p.consumeAggregateAngle()
					if err != nil {
						return nil, err
					}
					subClose := p.tok.Next()
					p.trackEnd(subClose)
					fields = append(fields, AggregateField{
						Name:        fieldName,
						SubFields:   subFields,
						IsExtension: isExtension,
					})
				} else {
					negative := false
					positive := false
					valTok := p.tok.Next()
					p.trackEnd(valTok)
					if valTok.Value == "-" {
						negative = true
						valTok = p.tok.Next()
						p.trackEnd(valTok)
					} else if valTok.Value == "+" {
						positive = true
						valTok = p.tok.Next()
						p.trackEnd(valTok)
					}
					val := valTok.Value
					if valTok.Type == tokenizer.TokenString {
						for p.tok.Peek().Type == tokenizer.TokenString {
							nextTok := p.tok.Next()
							p.trackEnd(nextTok)
							val += nextTok.Value
						}
					}
					fields = append(fields, AggregateField{
						Name:        fieldName,
						Value:       val,
						ValueType:   valTok.Type,
						Negative:    negative,
						Positive:    positive,
						IsExtension: isExtension,
					})
				}
				if p.tok.Peek().Value == "," {
					sepTok := p.tok.Next()
					p.trackEnd(sepTok)
				}
			}
			closeBr := p.tok.Next() // consume ']'
			p.trackEnd(closeBr)
		} else {
			negative := false
			positive := false
			valTok := p.tok.Next()
			p.trackEnd(valTok)
			if valTok.Value == "-" {
				negative = true
				valTok = p.tok.Next()
				p.trackEnd(valTok)
			} else if valTok.Value == "+" {
				positive = true
				valTok = p.tok.Next()
				p.trackEnd(valTok)
			}

			val := valTok.Value
			// Adjacent string concatenation
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					nextTok := p.tok.Next()
					p.trackEnd(nextTok)
					val += nextTok.Value
				}
			}

			fields = append(fields, AggregateField{
				Name:        fieldName,
				Value:       val,
				ValueType:   valTok.Type,
				Negative:    negative,
				Positive:    positive,
				IsExtension: isExtension,
			})
		}

		// Optional separator (';' or ',')
		if p.tok.Peek().Value == ";" || p.tok.Peek().Value == "," {
			sepTok := p.tok.Next()
			p.trackEnd(sepTok)
		}
	}
	return fields, nil
}

// consumeAggregateAngle reads key:value pairs inside a message literal < ... >.
// Called after consuming '<'. Stops before '>'.
func (p *parser) consumeAggregateAngle() ([]AggregateField, error) {
	var fields []AggregateField
	for p.tok.Peek().Value != ">" && p.tok.Peek().Type != tokenizer.TokenEOF {
		isExtension := false
		var fieldName string

		if p.tok.Peek().Value == "[" {
			bracketTok := p.tok.Next()
			p.trackEnd(bracketTok)
			isExtension = true
			nameTok := p.tok.Next()
			p.trackEnd(nameTok)
			fieldName = nameTok.Value
			for p.tok.Peek().Value == "." || p.tok.Peek().Value == "/" {
				sepTok := p.tok.Next()
				p.trackEnd(sepTok)
				partTok := p.tok.Next()
				p.trackEnd(partTok)
				fieldName += sepTok.Value + partTok.Value
			}
			closeBracket := p.tok.Next()
			p.trackEnd(closeBracket)
		} else {
			keyTok := p.tok.Next()
			p.trackEnd(keyTok)
			fieldName = keyTok.Value
		}

		// Expect ':' separator (required for scalars, optional for message literals)
		if p.tok.Peek().Value == ":" {
			colonTok := p.tok.Next()
			p.trackEnd(colonTok)
		} else if p.tok.Peek().Value != "{" && p.tok.Peek().Value != "<" {
			nextTok := p.tok.Peek()
			raw := nextTok.Value
			if nextTok.Type == tokenizer.TokenString {
				raw = "\"" + raw + "\""
			}
			return nil, fmt.Errorf("Expected \":\", found \"%s\".", raw)
		}

		// Read value — may be nested message literal { ... } or < ... > or scalar
		if p.tok.Peek().Value == "{" {
			brTok := p.tok.Next() // consume '{'
			p.trackEnd(brTok)
			subFields, err := p.consumeAggregate()
			if err != nil {
				return nil, err
			}
			closeTok := p.tok.Next() // consume '}'
			p.trackEnd(closeTok)
			fields = append(fields, AggregateField{
				Name:        fieldName,
				SubFields:   subFields,
				IsExtension: isExtension,
			})
		} else if p.tok.Peek().Value == "<" {
			brTok := p.tok.Next() // consume '<'
			p.trackEnd(brTok)
			subFields, err := p.consumeAggregateAngle()
			if err != nil {
				return nil, err
			}
			closeTok := p.tok.Next() // consume '>'
			p.trackEnd(closeTok)
			fields = append(fields, AggregateField{
				Name:        fieldName,
				SubFields:   subFields,
				IsExtension: isExtension,
			})
		} else if p.tok.Peek().Value == "[" {
			// List syntax for repeated fields
			brTok := p.tok.Next()
			p.trackEnd(brTok)
			for p.tok.Peek().Value != "]" && p.tok.Peek().Type != tokenizer.TokenEOF {
				if p.tok.Peek().Value == "{" {
					subBr := p.tok.Next()
					p.trackEnd(subBr)
					subFields, err := p.consumeAggregate()
					if err != nil {
						return nil, err
					}
					subClose := p.tok.Next()
					p.trackEnd(subClose)
					fields = append(fields, AggregateField{
						Name:        fieldName,
						SubFields:   subFields,
						IsExtension: isExtension,
					})
				} else if p.tok.Peek().Value == "<" {
					subBr := p.tok.Next()
					p.trackEnd(subBr)
					subFields, err := p.consumeAggregateAngle()
					if err != nil {
						return nil, err
					}
					subClose := p.tok.Next()
					p.trackEnd(subClose)
					fields = append(fields, AggregateField{
						Name:        fieldName,
						SubFields:   subFields,
						IsExtension: isExtension,
					})
				} else {
					negative := false
					positive := false
					valTok := p.tok.Next()
					p.trackEnd(valTok)
					if valTok.Value == "-" {
						negative = true
						valTok = p.tok.Next()
						p.trackEnd(valTok)
					} else if valTok.Value == "+" {
						positive = true
						valTok = p.tok.Next()
						p.trackEnd(valTok)
					}
					val := valTok.Value
					if valTok.Type == tokenizer.TokenString {
						for p.tok.Peek().Type == tokenizer.TokenString {
							nextTok := p.tok.Next()
							p.trackEnd(nextTok)
							val += nextTok.Value
						}
					}
					fields = append(fields, AggregateField{
						Name:        fieldName,
						Value:       val,
						ValueType:   valTok.Type,
						Negative:    negative,
						Positive:    positive,
						IsExtension: isExtension,
					})
				}
				if p.tok.Peek().Value == "," {
					sepTok := p.tok.Next()
					p.trackEnd(sepTok)
				}
			}
			closeBr := p.tok.Next() // consume ']'
			p.trackEnd(closeBr)
		} else {
			negative := false
			positive := false
			valTok := p.tok.Next()
			p.trackEnd(valTok)
			if valTok.Value == "-" {
				negative = true
				valTok = p.tok.Next()
				p.trackEnd(valTok)
			} else if valTok.Value == "+" {
				positive = true
				valTok = p.tok.Next()
				p.trackEnd(valTok)
			}

			val := valTok.Value
			// Adjacent string concatenation
			if valTok.Type == tokenizer.TokenString {
				for p.tok.Peek().Type == tokenizer.TokenString {
					nextTok := p.tok.Next()
					p.trackEnd(nextTok)
					val += nextTok.Value
				}
			}

			fields = append(fields, AggregateField{
				Name:        fieldName,
				Value:       val,
				ValueType:   valTok.Type,
				Negative:    negative,
				Positive:    positive,
				IsExtension: isExtension,
			})
		}

		// Optional separator (';' or ',')
		if p.tok.Peek().Value == ";" || p.tok.Peek().Value == "," {
			sepTok := p.tok.Next()
			p.trackEnd(sepTok)
		}
	}
	return fields, nil
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
func (p *parser) parseFieldOptions(field *descriptorpb.FieldDescriptorProto, fieldPath []int32) ([]*descriptorpb.SourceCodeInfo_Location, error) {
	bracketTok := p.tok.Next() // consume "["
	var optLocs []*descriptorpb.SourceCodeInfo_Location
	seenFieldOpts := map[string]bool{}
	targetsCount := int32(0)

	addLoc := func(path []int32, startLine, startCol, endLine, endCol int) {
		optLocs = append(optLocs, &descriptorpb.SourceCodeInfo_Location{
			Path: path,
			Span: multiSpan(startLine, startCol, endLine, endCol),
		})
	}

	for {
		optNameTok := p.tok.Next()
		optName := optNameTok.Value

		// Handle parenthesized custom option names: [(name) = value]
		if optName == "(" {
			fullName, err := p.parseParenthesizedOptionName(optNameTok)
			if err != nil {
				return nil, err
			}

			// Handle sub-field path: [(ext).sub1.sub2 = value]
			var subFieldPath []string
			for p.tok.Peek().Value == "." {
				dotTok := p.tok.Next()
				p.trackEnd(dotTok)
				subTok := p.tok.Next()
				p.trackEnd(subTok)
				subFieldPath = append(subFieldPath, subTok.Value)
			}

			// Consume "="
			if _, err := p.tok.Expect("="); err != nil {
				return nil, err
			}

			// Read value (may be negative, aggregate, or simple scalar)
			var custOpt CustomFieldOption
			custOpt.ParenName = fullName
			// Extract inner name (strip parens and leading dot)
			inner := fullName
			if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
				inner = inner[1 : len(inner)-1]
			}
			custOpt.InnerName = inner
			custOpt.SubFieldPath = subFieldPath
			custOpt.NameTok = optNameTok
			custOpt.Field = field

			// Reject angle bracket aggregate syntax and positive sign
			if p.tok.Peek().Value == "<" || p.tok.Peek().Value == "+" {
				rejTok := p.tok.Next()
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected option value.", p.filename, rejTok.Line+1, rejTok.Column+1))
				p.skipToToken("]")
				return nil, nil
			}

			negative := false
			if p.tok.Peek().Value == "-" {
				p.tok.Next()
				negative = true
			}
			custOpt.Negative = negative

			if p.tok.Peek().Value == "{" {
				// Aggregate value
				brTok := p.tok.Next() // consume '{'
				p.trackEnd(brTok)
				aggFields, aggErr := p.consumeAggregate()
				if aggErr != nil {
					return nil, fmt.Errorf("%d:%d: Error while parsing option value for \"%s\": %s", brTok.Line+1, brTok.Column+1, inner, aggErr.Error())
				}
				closeTok := p.tok.Next() // consume '}'
				p.trackEnd(closeTok)
				custOpt.AggregateFields = aggFields
				custOpt.AggregateBraceTok = brTok
			} else {
				valTok := p.tok.Next()
				p.trackEnd(valTok)
				custOpt.AggregateBraceTok = valTok
				custOpt.Value = valTok.Value
				if valTok.Type == tokenizer.TokenString {
					for p.tok.Peek().Type == tokenizer.TokenString {
						nextStr := p.tok.Next()
						p.trackEnd(nextStr)
						custOpt.Value += nextStr.Value
					}
				}
				if negative {
					custOpt.Value = "-" + custOpt.Value
				}
				custOpt.ValueType = valTok.Type
				custOpt.ValTok = valTok
			}

			// Determine option span end position
			endTok := p.tok.Peek()
			optEndLine := endTok.Line
			optEndCol := endTok.Column // don't include ]/,

			// Add SCI: option span [fieldPath..., 8, 0, ...subfields] (placeholder; field number resolved later)
			sciPath := append(copyPath(fieldPath), 8, 0)
			if len(subFieldPath) > 0 {
				for range subFieldPath {
					sciPath = append(sciPath, 0)
				}
			}
			sciLoc := &descriptorpb.SourceCodeInfo_Location{
				Path: sciPath,
				Span: multiSpan(optNameTok.Line, optNameTok.Column, optEndLine, optEndCol),
			}
			optLocs = append(optLocs, sciLoc)
			custOpt.SCILoc = sciLoc

			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}

			p.customFieldOptions = append(p.customFieldOptions, custOpt)

			// Check for comma (more options) or closing bracket
			next := p.tok.Peek()
			if next.Value == "," {
				p.tok.Next()
				if p.tok.Peek().Value == "]" {
					tok := p.tok.Peek()
					return nil, fmt.Errorf("%d:%d: Expected identifier.", tok.Line+1, tok.Column+1)
				}
				continue
			} else if next.Value == "]" {
				break
			} else {
				break
			}
		}

		// Handle dotted option names like features.field_presence
		var featSubField string
		if optName == "features" && p.tok.Peek().Value == "." {
			p.tok.Next() // consume "."
			subTok := p.tok.Next()
			featSubField = subTok.Value
			optName = "features." + featSubField
		}

		if optName != "targets" && optName != "edition_defaults" && seenFieldOpts[optName] {
			return nil, fmt.Errorf("%d:%d: Option \"%s\" was already set.", optNameTok.Line+1, optNameTok.Column+1, optName)
		}
		seenFieldOpts[optName] = true

		// Consume "="
		p.tok.Next()

		// Handle message-literal options: edition_defaults = { ... }, feature_support = { ... }
		if (optName == "edition_defaults" || optName == "feature_support") && p.tok.Peek().Value == "{" {
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			msgLitStartTok := optNameTok
			msgLitEndTok, err := p.parseMessageLiteralFieldOption(optName, field)
			if err != nil {
				return nil, err
			}
			valEnd := msgLitEndTok.Column + len(msgLitEndTok.Value)
			valEndLine := msgLitEndTok.Line
			switch optName {
			case "edition_defaults":
				addLoc(append(copyPath(fieldPath), 8, 20, int32(len(field.Options.EditionDefaults)-1)),
					msgLitStartTok.Line, msgLitStartTok.Column, valEndLine, valEnd)
			case "feature_support":
				addLoc(append(copyPath(fieldPath), 8, 22),
					msgLitStartTok.Line, msgLitStartTok.Column, valEndLine, valEnd)
			}
			// Check for comma (more options) or closing bracket
			next := p.tok.Peek()
			if next.Value == "," {
				p.tok.Next()
				if p.tok.Peek().Value == "]" {
					tok := p.tok.Peek()
					return nil, fmt.Errorf("%d:%d: Expected identifier.", tok.Line+1, tok.Column+1)
				}
			} else if next.Value == "]" {
				break
			} else {
				break
			}
			continue
		}

		// Handle negative values for default
		negative := false
		var minusTok tokenizer.Token
		if optName == "default" && p.tok.Peek().Value == "-" {
			minusTok = p.tok.Next()
			negative = true
		}

		valTok := p.tok.Next()
		valEnd := valTok.Column + len(valTok.Value)
		valEndLine := valTok.Line
		if valTok.Type == tokenizer.TokenString {
			valEnd = valTok.Column + valTok.RawLen // use raw length (includes quotes + escapes)
			// Concatenate adjacent string tokens (C++ protoc allows this)
			for p.tok.Peek().Type == tokenizer.TokenString {
				next := p.tok.Next()
				valTok.Value += next.Value
				valEnd = next.Column + next.RawLen
				valEndLine = next.Line
			}
		}

		switch optName {
		case "default":
			// Named type (message or enum reference) — type not yet resolved.
			// Accept whatever value is provided; validation happens after type resolution.
			if field.Type == nil {
				defVal := valTok.Value
				if negative {
					defVal = "-" + defVal
				}
				field.DefaultValue = proto.String(defVal)
			} else {
			// String/bytes fields require a string literal default value
			if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_STRING ||
				field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES {
				if valTok.Type != tokenizer.TokenString {
					p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected string for field default value.", p.filename, valTok.Line+1, valTok.Column+1))
					p.skipToToken("]")
					return optLocs, nil
				}
			}
			// Bool fields require identifier "true" or "false", reject anything else
			if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL {
				if valTok.Type != tokenizer.TokenIdent || (valTok.Value != "true" && valTok.Value != "false") {
					p.errors = append(p.errors, fmt.Sprintf(`%s:%d:%d: Expected "true" or "false".`, p.filename, valTok.Line+1, valTok.Column+1))
					p.skipToToken("]")
					return optLocs, nil
				}
			}
			// Float/double fields reject string literal and non-inf/nan identifier default values
			if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE ||
				field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
				if valTok.Type == tokenizer.TokenString {
					p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected number.", p.filename, valTok.Line+1, valTok.Column+1))
					p.skipToToken("]")
					return optLocs, nil
				}
				if valTok.Type == tokenizer.TokenIdent {
					lower := strings.ToLower(valTok.Value)
					if lower != "inf" && lower != "nan" {
						p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected number.", p.filename, valTok.Line+1, valTok.Column+1))
						p.skipToToken("]")
						return optLocs, nil
					}
				}
			}
			// Integer fields reject string literal, float literal, and identifier default values
			if isIntegerType(field.GetType()) && (valTok.Type == tokenizer.TokenString || valTok.Type == tokenizer.TokenFloat || valTok.Type == tokenizer.TokenIdent) {
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected integer for field default value.", p.filename, valTok.Line+1, valTok.Column+1))
				p.skipToToken("]")
				return optLocs, nil
			}
			// Unsigned fields reject negative default values
			if negative && isUnsignedType(field.GetType()) {
				p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Unsigned field can't have negative default value.", p.filename, valTok.Line+1, valTok.Column+1))
			}
			// Check integer default value overflow for field type
			if isIntegerType(field.GetType()) {
				maxVal := intDefaultMaxValue(field.GetType(), negative)
				n, err := strconv.ParseUint(valTok.Value, 0, 64)
				if err != nil || n > maxVal {
					p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Integer out of range.", p.filename, valTok.Line+1, valTok.Column+1))
				}
			}
			defVal := valTok.Value
			if negative {
				defVal = "-" + defVal
			}
			// Normalize float/double defaults to match C++ protoc (SimpleDtoa/SimpleFtoa)
			if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE ||
				field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
				lower := strings.ToLower(defVal)
				if lower == "inf" || lower == "-inf" || lower == "nan" || lower == "-nan" {
					defVal = lower
					if defVal == "-nan" {
						defVal = "nan"
					}
				} else if v, err := strconv.ParseFloat(defVal, 64); err == nil || (errors.Is(err, strconv.ErrRange) && (math.IsInf(v, 0) || math.IsNaN(v))) {
					if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
						defVal = simpleFtoa(float32(v))
					} else {
						defVal = simpleDtoa(v)
					}
				}
			}
			// Normalize octal/hex integer defaults to decimal to match C++ protoc
			if isIntegerType(field.GetType()) {
				defVal = normalizeIntDefault(defVal)
			}
			// C++ protoc calls CEscape on bytes default values
			if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES {
				defVal = cEscape(defVal)
			}
			field.DefaultValue = proto.String(defVal)
			} // end else (field.Type != nil)
		case "json_name":
			if valTok.Type != tokenizer.TokenString {
				return nil, fmt.Errorf("%d:%d: Expected string for JSON name.", valTok.Line+1, valTok.Column+1)
			}
			field.JsonName = proto.String(valTok.Value)
			p.explicitJsonNames[field] = true
		case "deprecated":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.FieldOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
			}
			if valTok.Value != "true" && valTok.Value != "false" {
				return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.FieldOptions.deprecated\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.Deprecated = proto.Bool(valTok.Value == "true")
		case "packed":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.FieldOptions.packed\".", valTok.Line+1, valTok.Column+1)
			}
			if valTok.Value != "true" && valTok.Value != "false" {
				return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.FieldOptions.packed\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.Packed = proto.Bool(valTok.Value == "true")
		case "lazy":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.FieldOptions.lazy\".", valTok.Line+1, valTok.Column+1)
			}
			if valTok.Value != "true" && valTok.Value != "false" {
				return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.FieldOptions.lazy\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.Lazy = proto.Bool(valTok.Value == "true")
		case "jstype":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.FieldOptions.jstype\".", valTok.Line+1, valTok.Column+1)
			}
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
			default:
				return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FieldOptions.JSType\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.jstype\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
		case "ctype":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.FieldOptions.ctype\".", valTok.Line+1, valTok.Column+1)
			}
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
			default:
				return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FieldOptions.CType\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.ctype\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
		case "debug_redact":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.FieldOptions.debug_redact\".", valTok.Line+1, valTok.Column+1)
			}
			if valTok.Value != "true" && valTok.Value != "false" {
				return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.FieldOptions.debug_redact\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.DebugRedact = proto.Bool(valTok.Value == "true")
		case "unverified_lazy":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.FieldOptions.unverified_lazy\".", valTok.Line+1, valTok.Column+1)
			}
			if valTok.Value != "true" && valTok.Value != "false" {
				return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.FieldOptions.unverified_lazy\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.UnverifiedLazy = proto.Bool(valTok.Value == "true")
		case "weak":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for boolean option \"google.protobuf.FieldOptions.weak\".", valTok.Line+1, valTok.Column+1)
			}
			if valTok.Value != "true" && valTok.Value != "false" {
				return nil, fmt.Errorf("%d:%d: Value must be \"true\" or \"false\" for boolean option \"google.protobuf.FieldOptions.weak\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			field.Options.Weak = proto.Bool(valTok.Value == "true")
		case "retention":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.FieldOptions.retention\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			switch valTok.Value {
			case "RETENTION_UNKNOWN":
				field.Options.Retention = descriptorpb.FieldOptions_RETENTION_UNKNOWN.Enum()
			case "RETENTION_RUNTIME":
				field.Options.Retention = descriptorpb.FieldOptions_RETENTION_RUNTIME.Enum()
			case "RETENTION_SOURCE":
				field.Options.Retention = descriptorpb.FieldOptions_RETENTION_SOURCE.Enum()
			default:
				return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FieldOptions.OptionRetention\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.retention\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
		case "targets":
			if valTok.Type != tokenizer.TokenIdent {
				return nil, fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.FieldOptions.targets\".", valTok.Line+1, valTok.Column+1)
			}
			if field.Options == nil {
				field.Options = &descriptorpb.FieldOptions{}
			}
			var targetVal descriptorpb.FieldOptions_OptionTargetType
			switch valTok.Value {
			case "TARGET_TYPE_UNKNOWN":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_UNKNOWN
			case "TARGET_TYPE_FILE":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_FILE
			case "TARGET_TYPE_EXTENSION_RANGE":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_EXTENSION_RANGE
			case "TARGET_TYPE_MESSAGE":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_MESSAGE
			case "TARGET_TYPE_FIELD":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_FIELD
			case "TARGET_TYPE_ONEOF":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_ONEOF
			case "TARGET_TYPE_ENUM":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_ENUM
			case "TARGET_TYPE_ENUM_ENTRY":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_ENUM_ENTRY
			case "TARGET_TYPE_SERVICE":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_SERVICE
			case "TARGET_TYPE_METHOD":
				targetVal = descriptorpb.FieldOptions_TARGET_TYPE_METHOD
			default:
				return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FieldOptions.OptionTargetType\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.targets\".", valTok.Line+1, valTok.Column+1, valTok.Value)
			}
			field.Options.Targets = append(field.Options.Targets, targetVal)
		default:
			if featSubField != "" {
				if field.Options == nil {
					field.Options = &descriptorpb.FieldOptions{}
				}
				if field.Options.Features == nil {
					field.Options.Features = &descriptorpb.FeatureSet{}
				}
				if valTok.Type != tokenizer.TokenIdent {
					return nil, fmt.Errorf("%d:%d: Value must be identifier for enum-valued option \"google.protobuf.FieldOptions.features.%s\".", valTok.Line+1, valTok.Column+1, featSubField)
				}
				var featFieldNum int32
				switch featSubField {
				case "field_presence":
					v, ok := descriptorpb.FeatureSet_FieldPresence_value[valTok.Value]
					if !ok {
						return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.FieldPresence\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.features.field_presence\".", valTok.Line+1, valTok.Column+1, valTok.Value)
					}
					field.Options.Features.FieldPresence = descriptorpb.FeatureSet_FieldPresence(v).Enum()
					featFieldNum = 1
				case "enum_type":
					v, ok := descriptorpb.FeatureSet_EnumType_value[valTok.Value]
					if !ok {
						return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.EnumType\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.features.enum_type\".", valTok.Line+1, valTok.Column+1, valTok.Value)
					}
					field.Options.Features.EnumType = descriptorpb.FeatureSet_EnumType(v).Enum()
					featFieldNum = 2
				case "repeated_field_encoding":
					v, ok := descriptorpb.FeatureSet_RepeatedFieldEncoding_value[valTok.Value]
					if !ok {
						return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.RepeatedFieldEncoding\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.features.repeated_field_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
					}
					field.Options.Features.RepeatedFieldEncoding = descriptorpb.FeatureSet_RepeatedFieldEncoding(v).Enum()
					featFieldNum = 3
				case "utf8_validation":
					v, ok := descriptorpb.FeatureSet_Utf8Validation_value[valTok.Value]
					if !ok {
						return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.Utf8Validation\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.features.utf8_validation\".", valTok.Line+1, valTok.Column+1, valTok.Value)
					}
					field.Options.Features.Utf8Validation = descriptorpb.FeatureSet_Utf8Validation(v).Enum()
					featFieldNum = 4
				case "message_encoding":
					v, ok := descriptorpb.FeatureSet_MessageEncoding_value[valTok.Value]
					if !ok {
						return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.MessageEncoding\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.features.message_encoding\".", valTok.Line+1, valTok.Column+1, valTok.Value)
					}
					field.Options.Features.MessageEncoding = descriptorpb.FeatureSet_MessageEncoding(v).Enum()
					featFieldNum = 5
				case "json_format":
					v, ok := descriptorpb.FeatureSet_JsonFormat_value[valTok.Value]
					if !ok {
						return nil, fmt.Errorf("%d:%d: Enum type \"google.protobuf.FeatureSet.JsonFormat\" has no value named \"%s\" for option \"google.protobuf.FieldOptions.features.json_format\".", valTok.Line+1, valTok.Column+1, valTok.Value)
					}
					field.Options.Features.JsonFormat = descriptorpb.FeatureSet_JsonFormat(v).Enum()
					featFieldNum = 6
				default:
					return nil, fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", optNameTok.Line+1, optNameTok.Column+1, optName)
				}
				addLoc(append(copyPath(fieldPath), 8, 21, featFieldNum),
					optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
			} else {
				return nil, fmt.Errorf("%d:%d: Option \"%s\" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).", optNameTok.Line+1, optNameTok.Column+1, optName)
			}
		}

		// Build source code info for this option
		switch optName {
		case "default":
			// default_value is field 7 of FieldDescriptorProto (not under options)
			defStartCol := valTok.Column
			if negative {
				defStartCol = minusTok.Column
			}
			addLoc(append(copyPath(fieldPath), 7),
				valTok.Line, defStartCol, valEndLine, valEnd)
		case "json_name":
			// json_name goes to path [10] (field 10 of FieldDescriptorProto)
			addLoc(append(copyPath(fieldPath), 10),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
			addLoc(append(copyPath(fieldPath), 10),
				valTok.Line, valTok.Column, valEndLine, valEnd)
		case "deprecated":
			addLoc(append(copyPath(fieldPath), 8, 3),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "packed":
			addLoc(append(copyPath(fieldPath), 8, 2),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "lazy":
			addLoc(append(copyPath(fieldPath), 8, 5),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "jstype":
			addLoc(append(copyPath(fieldPath), 8, 6),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "ctype":
			addLoc(append(copyPath(fieldPath), 8, 1),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "debug_redact":
			addLoc(append(copyPath(fieldPath), 8, 16),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "unverified_lazy":
			addLoc(append(copyPath(fieldPath), 8, 15),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "weak":
			addLoc(append(copyPath(fieldPath), 8, 10),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "retention":
			addLoc(append(copyPath(fieldPath), 8, 17),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
		case "targets":
			addLoc(append(copyPath(fieldPath), 8, 19, targetsCount),
				optNameTok.Line, optNameTok.Column, valEndLine, valEnd)
			targetsCount++
		}

		// Check for comma (more options) or closing bracket
		next := p.tok.Peek()
		if next.Value == "," {
			p.tok.Next()
			// Trailing comma — next token should be an identifier, not "]"
			if p.tok.Peek().Value == "]" {
				tok := p.tok.Peek()
				return nil, fmt.Errorf("%d:%d: Expected identifier.", tok.Line+1, tok.Column+1)
			}
		} else if next.Value == "]" {
			break
		} else {
			// Unexpected token inside option brackets (e.g., ";" instead of "," or "]")
			p.errors = append(p.errors, fmt.Sprintf("%s:%d:%d: Expected \"]\".", p.filename, next.Line+1, next.Column+1))
			// Return early — leave the unexpected token for the caller's error recovery
			return nil, nil
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

	return result, nil
}

// parseMessageLiteralFieldOption parses a message literal value for edition_defaults
// or feature_support field options: { key: value [, key: value] ... }
// Returns the closing '}' token.
func (p *parser) parseMessageLiteralFieldOption(optName string, field *descriptorpb.FieldDescriptorProto) (tokenizer.Token, error) {
	openTok := p.tok.Next() // consume "{"
	_ = openTok

	switch optName {
	case "edition_defaults":
		ed := &descriptorpb.FieldOptions_EditionDefault{}
		for p.tok.Peek().Value != "}" {
			keyTok := p.tok.Next()
			key := keyTok.Value
			p.tok.Next() // consume ":"
			valTok := p.tok.Next()
			switch key {
			case "edition":
				if v, ok := descriptorpb.Edition_value[valTok.Value]; ok {
					ed.Edition = descriptorpb.Edition(v).Enum()
				}
			case "value":
				ed.Value = proto.String(valTok.Value)
			}
			// consume optional comma/semicolon
			if p.tok.Peek().Value == "," || p.tok.Peek().Value == ";" {
				p.tok.Next()
			}
		}
		closeTok := p.tok.Next() // consume "}"
		field.Options.EditionDefaults = append(field.Options.EditionDefaults, ed)
		return closeTok, nil

	case "feature_support":
		fs := &descriptorpb.FieldOptions_FeatureSupport{}
		for p.tok.Peek().Value != "}" {
			keyTok := p.tok.Next()
			key := keyTok.Value
			p.tok.Next() // consume ":"
			valTok := p.tok.Next()
			switch key {
			case "edition_introduced":
				if v, ok := descriptorpb.Edition_value[valTok.Value]; ok {
					fs.EditionIntroduced = descriptorpb.Edition(v).Enum()
				}
			case "edition_deprecated":
				if v, ok := descriptorpb.Edition_value[valTok.Value]; ok {
					fs.EditionDeprecated = descriptorpb.Edition(v).Enum()
				}
			case "deprecation_warning":
				fs.DeprecationWarning = proto.String(valTok.Value)
			case "edition_removed":
				if v, ok := descriptorpb.Edition_value[valTok.Value]; ok {
					fs.EditionRemoved = descriptorpb.Edition(v).Enum()
				}
			}
			// consume optional comma/semicolon
			if p.tok.Peek().Value == "," || p.tok.Peek().Value == ";" {
				p.tok.Next()
			}
		}
		closeTok := p.tok.Next() // consume "}"
		field.Options.FeatureSupport = fs
		return closeTok, nil
	}

	// Should not reach here, but skip the message literal
	depth := 1
	var lastTok tokenizer.Token
	for depth > 0 {
		lastTok = p.tok.Next()
		if lastTok.Value == "{" {
			depth++
		} else if lastTok.Value == "}" {
			depth--
		}
	}
	return lastTok, nil
}

func unquoteString(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func isIntegerType(t descriptorpb.FieldDescriptorProto_Type) bool {
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
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return true
	}
	return false
}

func isUnsignedType(t descriptorpb.FieldDescriptorProto_Type) bool {
	switch t {
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return true
	}
	return false
}

// simpleDtoa matches C++ protoc's SimpleDtoa: %.15g with round-trip check.
func simpleDtoa(v float64) string {
	if math.IsInf(v, 1) {
		return "inf"
	} else if math.IsInf(v, -1) {
		return "-inf"
	} else if math.IsNaN(v) {
		return "nan"
	}
	s := strconv.FormatFloat(v, 'g', 15, 64)
	if v2, err := strconv.ParseFloat(s, 64); err != nil || v2 != v {
		s = strconv.FormatFloat(v, 'g', 17, 64)
	}
	return s
}

// simpleFtoa matches C++ protoc's SimpleFtoa: cast to float32, %.6g with round-trip check.
func simpleFtoa(v float32) string {
	v64 := float64(v)
	if math.IsInf(v64, 1) {
		return "inf"
	} else if math.IsInf(v64, -1) {
		return "-inf"
	} else if math.IsNaN(v64) {
		return "nan"
	}
	s := strconv.FormatFloat(v64, 'g', 6, 64)
	if v2, err := strconv.ParseFloat(s, 32); err != nil || float32(v2) != v {
		s = strconv.FormatFloat(v64, 'g', 9, 64)
	}
	return s
}

func intDefaultMaxValue(ft descriptorpb.FieldDescriptorProto_Type, negative bool) uint64 {
	switch ft {
	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		if negative {
			return 2147483648
		}
		return 2147483647
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		return 4294967295
	case descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		if negative {
			return 9223372036854775808
		}
		return 9223372036854775807
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return 18446744073709551615
	default:
		return 18446744073709551615
	}
}

func normalizeIntDefault(s string) string {
	neg := false
	v := s
	if len(v) > 0 && v[0] == '-' {
		neg = true
		v = v[1:]
	}
	if len(v) < 2 || v[0] != '0' {
		// Handle -0 → 0
		if neg && v == "0" {
			return "0"
		}
		return s
	}
	var n uint64
	var err error
	if v[1] == 'x' || v[1] == 'X' {
		n, err = strconv.ParseUint(v[2:], 16, 64)
	} else {
		n, err = strconv.ParseUint(v, 8, 64)
	}
	if err != nil {
		return s
	}
	dec := strconv.FormatUint(n, 10)
	if neg {
		if n == 0 {
			return "0"
		}
		return "-" + dec
	}
	return dec
}

// cEscape converts raw bytes to C-escaped string, matching absl::CEscape.
func cEscape(s string) string {
	var buf []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		case '"':
			buf = append(buf, '\\', '"')
		case '\'':
			buf = append(buf, '\\', '\'')
		case '\\':
			buf = append(buf, '\\', '\\')
		default:
			if c >= 0x20 && c <= 0x7e {
				buf = append(buf, c)
			} else {
				buf = append(buf, '\\')
				buf = append(buf, '0'+(c>>6)&3)
				buf = append(buf, '0'+(c>>3)&7)
				buf = append(buf, '0'+c&7)
			}
		}
	}
	return string(buf)
}

// skipToToken consumes tokens until the target token is found and consumed.
func (p *parser) skipToToken(target string) {
	for {
		tok := p.tok.Next()
		if tok.Value == target || tok.Type == tokenizer.TokenEOF {
			return
		}
	}
}

// skipStatementCpp implements C++ protoc's SkipStatement() behavior:
// skips tokens until ; (consumed), { (consumes block then returns), or } (not consumed).
func (p *parser) skipStatementCpp() {
	for {
		tok := p.tok.Peek()
		if tok.Type == tokenizer.TokenEOF {
			return
		}
		if tok.Value == ";" {
			p.tok.Next()
			return
		}
		if tok.Value == "{" {
			p.tok.Next()
			p.skipRestOfBlock()
			return
		}
		if tok.Value == "}" {
			return
		}
		p.tok.Next()
	}
}

// skipRestOfBlock consumes tokens until the matching } is found (and consumed).
func (p *parser) skipRestOfBlock() {
	depth := 0
	for {
		tok := p.tok.Peek()
		if tok.Type == tokenizer.TokenEOF {
			return
		}
		if tok.Value == "}" {
			p.tok.Next()
			if depth <= 0 {
				return
			}
			depth--
		} else if tok.Value == "{" {
			p.tok.Next()
			depth++
		} else {
			p.tok.Next()
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

func (p *parser) addLocationSpanReturn(path []int32, startLine, startCol, endLine, endCol int) *descriptorpb.SourceCodeInfo_Location {
	pathCopy := make([]int32, len(path))
	copy(pathCopy, path)
	loc := &descriptorpb.SourceCodeInfo_Location{
		Path: pathCopy,
		Span: multiSpan(startLine, startCol, endLine, endCol),
	}
	p.locations = append(p.locations, loc)
	return loc
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
	if tok.RawLen > 0 {
		endCol = tok.Column + tok.RawLen
	}
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
func ResolveTypes(fd *descriptorpb.FileDescriptorProto, allFiles map[string]*descriptorpb.FileDescriptorProto) []string {
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

	var errors []string
	filename := fd.GetName()

	resolveMessageFieldsWithErrors(fd.GetMessageType(), prefix, types, filename, fd, &errors)

	for svcIdx, svc := range fd.GetService() {
		for methodIdx, m := range svc.GetMethod() {
			if m.InputType != nil {
				origName := m.GetInputType()
				resolved, shadow := resolveTypeName(origName, prefix, types)
				m.InputType = proto.String(resolved)
				if tp, ok := types[resolved]; !ok {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 2}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						if shadow != "" {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
						} else {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
						}
					}
				} else if tp == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 2}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not a message type.", filename, line, col, origName))
					}
				}
			}
			if m.OutputType != nil {
				origName := m.GetOutputType()
				resolved, shadow := resolveTypeName(origName, prefix, types)
				m.OutputType = proto.String(resolved)
				if tp, ok := types[resolved]; !ok {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 3}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						if shadow != "" {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
						} else {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
						}
					}
				} else if tp == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 3}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not a message type.", filename, line, col, origName))
					}
				}
			}
		}
	}

	// Resolve extension field types and extendee names
	for extIdx, ext := range fd.GetExtension() {
		if ext.Extendee != nil {
			origName := ext.GetExtendee()
			resolved, shadow := resolveTypeName(origName, prefix, types)
			ext.Extendee = proto.String(resolved)
			if _, ok := types[resolved]; !ok {
				path := []int32{7, int32(extIdx), 2}
				if line, col, ok := findSCISpanStart(fd, path); ok {
					if shadow != "" {
						errors = append(errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
					} else {
						errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
					}
				}
			}
		}
		if ext.TypeName != nil {
			origName := ext.GetTypeName()
			resolved, shadow := resolveTypeName(origName, prefix, types)
			ext.TypeName = proto.String(resolved)
			if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
				if tp, ok := types[resolved]; ok {
					ext.Type = tp.Enum()
				} else {
					path := []int32{7, int32(extIdx), 6}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						if shadow != "" {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
						} else {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
						}
					}
				}
			}
		}
	}

	return errors
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
				resolved, _ := resolveTypeName(f.GetTypeName(), msgPrefix, types)
				f.TypeName = proto.String(resolved)
				if f.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					if tp, ok := types[resolved]; ok {
						f.Type = tp.Enum()
					}
				}
			}
		}
		resolveMessageFields(msg.GetNestedType(), msgPrefix, types)
		for _, ext := range msg.GetExtension() {
			if ext.Extendee != nil {
				resolved, _ := resolveTypeName(ext.GetExtendee(), msgPrefix, types)
				ext.Extendee = proto.String(resolved)
			}
			if ext.TypeName != nil {
				resolved, _ := resolveTypeName(ext.GetTypeName(), msgPrefix, types)
				ext.TypeName = proto.String(resolved)
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					if tp, ok := types[resolved]; ok {
						ext.Type = tp.Enum()
					}
				}
			}
		}
	}
}

func resolveMessageFieldsWithErrors(msgs []*descriptorpb.DescriptorProto, prefix string, types map[string]descriptorpb.FieldDescriptorProto_Type, filename string, fd *descriptorpb.FileDescriptorProto, errors *[]string) {
	resolveMessageFieldsWithErrorsPath(msgs, prefix, types, filename, fd, nil, errors)
}

func resolveMessageFieldsWithErrorsPath(msgs []*descriptorpb.DescriptorProto, prefix string, types map[string]descriptorpb.FieldDescriptorProto_Type, filename string, fd *descriptorpb.FileDescriptorProto, parentPath []int32, errors *[]string) {
	for msgIdx, msg := range msgs {
		msgPrefix := prefix + "." + msg.GetName()
		var msgPath []int32
		if parentPath == nil {
			msgPath = []int32{4, int32(msgIdx)}
		} else {
			msgPath = append(copyPath(parentPath), 3, int32(msgIdx))
		}
		for fieldIdx, f := range msg.GetField() {
			if f.TypeName != nil {
				origName := f.GetTypeName()
				resolved, shadow := resolveTypeName(origName, msgPrefix, types)
				f.TypeName = proto.String(resolved)
				if f.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					if tp, ok := types[resolved]; ok {
						f.Type = tp.Enum()
					} else {
						path := append(copyPath(msgPath), 2, int32(fieldIdx), 6)
						if line, col, ok := findSCISpanStart(fd, path); ok {
							if shadow != "" {
								*errors = append(*errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
							} else {
								*errors = append(*errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
							}
						} else {
							*errors = append(*errors, fmt.Sprintf("%s: \"%s\" is not defined.", filename, origName))
						}
					}
				}
			}
		}
		resolveMessageFieldsWithErrorsPath(msg.GetNestedType(), msgPrefix, types, filename, fd, msgPath, errors)
		for extIdx, ext := range msg.GetExtension() {
			if ext.Extendee != nil {
				origName := ext.GetExtendee()
				resolved, shadow := resolveTypeName(origName, msgPrefix, types)
				ext.Extendee = proto.String(resolved)
				if _, ok := types[resolved]; !ok {
					path := append(copyPath(msgPath), 6, int32(extIdx), 2)
					if line, col, ok := findSCISpanStart(fd, path); ok {
						if shadow != "" {
							*errors = append(*errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
						} else {
							*errors = append(*errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
						}
					}
				}
			}
			if ext.TypeName != nil {
				origName := ext.GetTypeName()
				resolved, shadow := resolveTypeName(origName, msgPrefix, types)
				ext.TypeName = proto.String(resolved)
				if ext.GetType() != descriptorpb.FieldDescriptorProto_TYPE_GROUP {
					if tp, ok := types[resolved]; ok {
						ext.Type = tp.Enum()
					} else {
						path := append(copyPath(msgPath), 6, int32(extIdx), 6)
						if line, col, ok := findSCISpanStart(fd, path); ok {
							if shadow != "" {
								*errors = append(*errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
							} else {
								*errors = append(*errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
							}
						}
					}
				}
			}
		}
	}
}

// resolveTypeName resolves a type name using C++ protoc's scope-based lookup.
// For compound names (containing dots), it first resolves the first component
// using innermost-scope-first search, then looks up the rest within that scope.
// Returns (resolved, shadowCandidate) where shadowCandidate is non-empty if
// the first component was found at a closer scope but the full compound doesn't
// exist there (type shadowing error).
func resolveTypeName(name string, scope string, types map[string]descriptorpb.FieldDescriptorProto_Type) (string, string) {
	if strings.HasPrefix(name, ".") {
		return name, ""
	}

	firstDot := strings.Index(name, ".")
	firstPart := name
	if firstDot >= 0 {
		firstPart = name[:firstDot]
	}

	s := scope
	for s != "" {
		firstCandidate := s + "." + firstPart
		if tp, ok := types[firstCandidate]; ok {
			if firstDot >= 0 {
				// Compound name: first part found at this scope.
				// C++ protoc commits to the innermost match regardless of type.
				fullCandidate := s + "." + name
				if tp == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
					if _, ok := types[fullCandidate]; ok {
						return fullCandidate, ""
					}
				}
				// Shadowing: first part found but full compound doesn't exist
				// (or first part is non-aggregate like an enum)
				return "." + name, fullCandidate
			} else {
				// Simple name: found
				return firstCandidate, ""
			}
		}
		lastDot := strings.LastIndex(s, ".")
		if lastDot < 0 {
			break
		}
		s = s[:lastDot]
	}

	// Root scope fallback: try full name directly
	candidate := "." + name
	if _, ok := types[candidate]; ok {
		return candidate, ""
	}

	return "." + name, ""
}

func shadowErrorMsg(origName, shadowCandidate string) string {
	display := shadowCandidate
	if strings.HasPrefix(display, ".") {
		display = display[1:]
	}
	return fmt.Sprintf("\"%s\" is resolved to \"%s\", which is not defined. "+
		"The innermost scope is searched first in name resolution. "+
		"Consider using a leading '.'(i.e., \".%s\") to start from the outermost scope.",
		origName, display, origName)
}

// CheckUnresolvedTypes checks for unresolved type references in fd,
// using only the given availableFiles for imported types.
// Returns error strings like: filename:line:col: "TypeName" is not defined.
func CheckUnresolvedTypes(fd *descriptorpb.FileDescriptorProto, availableFiles map[string]*descriptorpb.FileDescriptorProto) []string {
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
	if availableFiles != nil {
		for _, dep := range fd.GetDependency() {
			if depFd, ok := availableFiles[dep]; ok {
				collectImportedTypes(depFd, types)
			}
		}
		visited := map[string]bool{fd.GetName(): true}
		for _, dep := range fd.GetDependency() {
			collectPublicImportTypes(dep, availableFiles, types, visited)
		}
	}

	var errors []string
	filename := fd.GetName()

	for msgIdx, msg := range fd.GetMessageType() {
		checkMsgUnresolved(msg, prefix, types, filename, fd, []int32{4, int32(msgIdx)}, &errors)
	}

	for svcIdx, svc := range fd.GetService() {
		for methodIdx, m := range svc.GetMethod() {
			methodPrefix := prefix
			if m.InputType != nil {
				origName := m.GetInputType()
				resolved, shadow := resolveTypeName(origName, methodPrefix, types)
				if tp, ok := types[resolved]; !ok {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 2}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						if shadow != "" {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
						} else {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
						}
					}
				} else if tp == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 2}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not a message type.", filename, line, col, origName))
					}
				}
			}
			if m.OutputType != nil {
				origName := m.GetOutputType()
				resolved, shadow := resolveTypeName(origName, methodPrefix, types)
				if tp, ok := types[resolved]; !ok {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 3}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						if shadow != "" {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
						} else {
							errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
						}
					}
				} else if tp == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
					path := []int32{6, int32(svcIdx), 2, int32(methodIdx), 3}
					if line, col, ok := findSCISpanStart(fd, path); ok {
						errors = append(errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not a message type.", filename, line, col, origName))
					}
				}
			}
		}
	}

	return errors
}

func checkMsgUnresolved(msg *descriptorpb.DescriptorProto, parentPrefix string, types map[string]descriptorpb.FieldDescriptorProto_Type, filename string, fd *descriptorpb.FileDescriptorProto, msgPath []int32, errors *[]string) {
	msgPrefix := parentPrefix + "." + msg.GetName()

	if msg.GetOptions().GetMapEntry() {
		return
	}

	for fieldIdx, f := range msg.GetField() {
		if f.TypeName != nil {
			origName := f.GetTypeName()
			resolved, shadow := resolveTypeName(origName, msgPrefix, types)
			if _, ok := types[resolved]; !ok {
				path := append(copyPath(msgPath), 2, int32(fieldIdx), 6)
				if line, col, ok := findSCISpanStart(fd, path); ok {
					if shadow != "" {
						*errors = append(*errors, fmt.Sprintf("%s:%d:%d: %s", filename, line, col, shadowErrorMsg(origName, shadow)))
					} else {
						*errors = append(*errors, fmt.Sprintf("%s:%d:%d: \"%s\" is not defined.", filename, line, col, origName))
					}
				}
			}
		}
	}

	for i, nested := range msg.GetNestedType() {
		nestedPath := append(copyPath(msgPath), 3, int32(i))
		checkMsgUnresolved(nested, msgPrefix, types, filename, fd, nestedPath, errors)
	}
}

func findSCISpanStart(fd *descriptorpb.FileDescriptorProto, path []int32) (int, int, bool) {
	for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
		locPath := loc.GetPath()
		if len(locPath) != len(path) {
			continue
		}
		match := true
		for i := range path {
			if locPath[i] != path[i] {
				match = false
				break
			}
		}
		if match {
			span := loc.GetSpan()
			if len(span) >= 2 {
				return int(span[0]) + 1, int(span[1]) + 1, true
			}
		}
	}
	return 0, 0, false
}
