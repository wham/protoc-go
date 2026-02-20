// Package tokenizer implements lexical analysis of .proto files.
// This mirrors C++ google::protobuf::io::Tokenizer from io/tokenizer.cc.
package tokenizer

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type TokenType int

const (
	TokenIdent   TokenType = iota
	TokenString            // quoted string literal
	TokenInt               // integer literal
	TokenFloat             // float literal
	TokenSymbol            // single-char symbol
	TokenEOF
)

type Token struct {
	Type   TokenType
	Value  string
	Line   int // 0-based
	Column int // 0-based
	RawLen int // raw source length (for strings: includes quotes and escape sequences)
}

// TokenComments holds classified comment data between two adjacent tokens.
// Mirrors C++ Tokenizer::NextWithComments output.
type TokenComments struct {
	PrevTrailing string   // trailing comment of the previous token
	Detached     []string // detached comments (separated by blank lines)
	Leading      string   // leading comment for this token
}

// TokenError represents an error detected during tokenization.
type TokenError struct {
	Line    int    // 0-based
	Column  int    // 0-based
	Message string
	Notes   []TokenError // follow-up notes (printed after main error, not sorted separately)
}

type Tokenizer struct {
	input    string
	pos      int
	line     int // 0-based
	col      int // 0-based
	tokens   []Token
	comments []TokenComments // parallel to tokens
	idx      int
	Errors   []TokenError
}

func New(input string) *Tokenizer {
	t := &Tokenizer{input: input}
	// Skip UTF-8 BOM if present (matching C++ protoc behavior).
	// Keep in input so positions account for BOM bytes.
	if len(input) >= 3 && input[0] == 0xEF && input[1] == 0xBB && input[2] == 0xBF {
		t.pos = 3
		t.col = 3
	}
	t.tokenize()
	return t
}

func (t *Tokenizer) tokenize() {
	prevTokenLine := -1 // no previous token
	for t.pos < len(t.input) {
		cd := t.collectComments(prevTokenLine)
		if t.pos >= len(t.input) {
			t.comments = append(t.comments, cd)
			break
		}

		ch := t.input[t.pos]
		t.comments = append(t.comments, cd)

		if ch == '"' || ch == '\'' {
			t.readString()
		} else if ch >= '0' && ch <= '9' {
			t.readNumber()
		} else if ch == '.' && t.pos+1 < len(t.input) && t.input[t.pos+1] >= '0' && t.input[t.pos+1] <= '9' {
			t.readFloatStartingWithDot()
		} else if isIdentStart(ch) {
			t.readIdent()
		} else {
			t.tokens = append(t.tokens, Token{Type: TokenSymbol, Value: string(ch), Line: t.line, Column: t.col})
			t.advance()
		}
		prevTokenLine = t.tokens[len(t.tokens)-1].Line
	}
	// EOF token
	if len(t.comments) < len(t.tokens)+1 {
		t.comments = append(t.comments, t.collectComments(prevTokenLine))
	}
	t.tokens = append(t.tokens, Token{Type: TokenEOF, Value: "", Line: t.line, Column: t.col})
	// Ensure comments slice matches tokens
	for len(t.comments) < len(t.tokens) {
		t.comments = append(t.comments, TokenComments{})
	}
}

// collectComments scans whitespace and comments between tokens, classifying
// them as trailing (of prev token), detached, or leading (of next token).
// Mirrors C++ CommentCollector logic from tokenizer.cc.
func (t *Tokenizer) collectComments(prevTokenLine int) TokenComments {
	var result TokenComments
	canAttachToPrev := prevTokenLine >= 0
	var commentBuf strings.Builder
	hasComment := false
	isLineComment := false

	// Phase 1: Check for trailing comment on same line as previous token
	if canAttachToPrev {
		// Skip non-newline whitespace
		for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r') {
			t.advance()
		}
		if t.pos >= len(t.input) {
			return result
		}
		if t.pos+1 < len(t.input) && t.input[t.pos] == '/' && t.input[t.pos+1] == '/' {
			// Line comment on same line → trailing of prev
			t.advance() // skip /
			t.advance() // skip /
			text := t.readLineCommentText()
			result.PrevTrailing = text
			canAttachToPrev = false
		} else if t.pos+1 < len(t.input) && t.input[t.pos] == '/' && t.input[t.pos+1] == '*' {
			// Block comment on same line → trailing of prev
			bcStartLine, bcStartCol := t.line, t.col
			t.advance() // skip /
			t.advance() // skip *
			text := t.readBlockCommentText(bcStartLine, bcStartCol)
			result.PrevTrailing = text
			canAttachToPrev = false
			// Consume rest of line
			for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r') {
				t.advance()
			}
			if t.pos < len(t.input) && t.input[t.pos] == '\n' {
				t.advance()
			}
		} else if t.input[t.pos] == '\n' {
			t.advance()
			canAttachToPrev = false
		} else {
			// Next token on same line, no comments
			return result
		}
	}

	// Phase 2: Collect remaining comments, detect blank lines for detachment
	for t.pos < len(t.input) {
		// Skip non-newline whitespace
		for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r') {
			t.advance()
		}
		if t.pos >= len(t.input) {
			break
		}

		if t.pos+1 < len(t.input) && t.input[t.pos] == '/' && t.input[t.pos+1] == '/' {
			// Line comment - append to buffer (consecutive line comments merge)
			if hasComment && !isLineComment {
				// Previous was block comment, flush it
				t.flushComment(&result, &commentBuf, canAttachToPrev)
				canAttachToPrev = false
			}
			t.advance() // skip /
			t.advance() // skip /
			text := t.readLineCommentText()
			commentBuf.WriteString(text)
			hasComment = true
			isLineComment = true
		} else if t.pos+1 < len(t.input) && t.input[t.pos] == '/' && t.input[t.pos+1] == '*' {
			// Block comment - flush previous if any
			if hasComment {
				t.flushComment(&result, &commentBuf, canAttachToPrev)
				canAttachToPrev = false
			}
			bcStartLine, bcStartCol := t.line, t.col
			t.advance() // skip /
			t.advance() // skip *
			text := t.readBlockCommentText(bcStartLine, bcStartCol)
			commentBuf.WriteString(text)
			hasComment = true
			isLineComment = false
			// Consume trailing whitespace and newline
			for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r') {
				t.advance()
			}
			if t.pos < len(t.input) && t.input[t.pos] == '\n' {
				t.advance()
			}
		} else if t.input[t.pos] == '\n' {
			// Blank line → flush current comment as detached
			if hasComment {
				t.flushComment(&result, &commentBuf, canAttachToPrev)
				canAttachToPrev = false
			}
			canAttachToPrev = false
			t.advance()
		} else {
			// Non-comment, non-whitespace → next token found
			break
		}
	}

	// Whatever remains in the buffer is the leading comment
	if hasComment {
		result.Leading = commentBuf.String()
	}

	return result
}

func (t *Tokenizer) flushComment(result *TokenComments, buf *strings.Builder, canAttachToPrev bool) {
	text := buf.String()
	if canAttachToPrev {
		result.PrevTrailing = text
	} else {
		result.Detached = append(result.Detached, text)
	}
	buf.Reset()
}

// readLineCommentText reads text after "//" until end of line, returns text with trailing \n.
func (t *Tokenizer) readLineCommentText() string {
	start := t.pos
	for t.pos < len(t.input) && t.input[t.pos] != '\n' {
		t.advance()
	}
	text := t.input[start:t.pos]
	if t.pos < len(t.input) {
		t.advance() // skip \n
	}
	return text + "\n"
}

// readBlockCommentText reads text between /* and */, returns content without delimiters.
// startLine and startCol are the 0-based position of the '/' in '/*'.
func (t *Tokenizer) readBlockCommentText(startLine, startCol int) string {
	var buf strings.Builder
	for t.pos < len(t.input) {
		if t.input[t.pos] == '*' && t.pos+1 < len(t.input) && t.input[t.pos+1] == '/' {
			t.advance() // skip *
			t.advance() // skip /
			return buf.String()
		}
		buf.WriteByte(t.input[t.pos])
		t.advance()
	}
	// EOF inside block comment
	t.Errors = append(t.Errors,
		TokenError{
			Line: t.line, Column: t.col,
			Message: "End-of-file inside block comment.",
			Notes:   []TokenError{{Line: startLine, Column: startCol, Message: "  Comment started here."}},
		},
	)
	return buf.String()
}

// CommentsAt returns comment data for the token at index i.
func (t *Tokenizer) CommentsAt(i int) TokenComments {
	if i >= 0 && i < len(t.comments) {
		return t.comments[i]
	}
	return TokenComments{}
}

// CurrentIndex returns the current token index (the one Peek would return).
func (t *Tokenizer) CurrentIndex() int {
	return t.idx
}

func (t *Tokenizer) readString() {
	quote := t.input[t.pos]
	startLine := t.line
	startCol := t.col
	t.advance() // skip opening quote
	var sb strings.Builder
	for t.pos < len(t.input) && t.input[t.pos] != quote {
		if t.input[t.pos] == '\n' {
			t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Multiline strings are not allowed. Did you miss a \"?."})
			break
		}
		if t.input[t.pos] == '\\' {
			t.advance()
			if t.pos < len(t.input) {
				ch := t.input[t.pos]
				switch ch {
				case 'n':
					sb.WriteByte('\n')
				case 't':
					sb.WriteByte('\t')
				case 'r':
					sb.WriteByte('\r')
				case 'a':
					sb.WriteByte('\a')
				case 'b':
					sb.WriteByte('\b')
				case 'f':
					sb.WriteByte('\f')
				case 'v':
					sb.WriteByte('\v')
				case '\\':
					sb.WriteByte('\\')
				case '\'':
					sb.WriteByte('\'')
				case '"':
					sb.WriteByte('"')
				case '?':
					sb.WriteByte('?')
				case 'x', 'X':
					// Hex escape: \xHH (up to 2 hex digits)
					val := byte(0)
					t.advance()
					count := 0
					for i := 0; i < 2 && t.pos < len(t.input) && isHexDigit(t.input[t.pos]); i++ {
						val = val*16 + hexVal(t.input[t.pos])
						t.advance()
						count++
					}
					if count == 0 {
						t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Expected hex digits for escape sequence."})
					}
					sb.WriteByte(val)
					continue // already advanced past the digits
				case 'u':
					// Unicode escape: \uNNNN (exactly 4 hex digits)
					t.advance()
					cp, cnt := t.readUnicodeHex(4)
					if cnt < 4 {
						t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Expected four hex digits for \\u escape sequence."})
						continue
					}
					if isHeadSurrogate(cp) && t.pos+1 < len(t.input) && t.input[t.pos] == '\\' && t.input[t.pos+1] == 'u' {
						t.advance() // skip '\'
						t.advance() // skip 'u'
						trail, trailCnt := t.readUnicodeHex(4)
						if trailCnt < 4 {
							t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Expected four hex digits for \\u escape sequence."})
							appendUTF8(&sb, cp)
							continue
						}
						if isTrailSurrogate(trail) {
							cp = assembleUTF16(cp, trail)
						} else {
							appendUTF8(&sb, cp)
							appendUTF8(&sb, trail)
							continue
						}
					}
					appendUTF8(&sb, cp)
					continue
				case 'U':
					// Unicode escape: \UNNNNNNNN (exactly 8 hex digits)
					t.advance()
					cp, cnt := t.readUnicodeHex(8)
					if cnt < 8 {
						t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Expected eight hex digits for \\U escape sequence."})
						continue
					}
					appendUTF8(&sb, cp)
					continue
				case '0', '1', '2', '3', '4', '5', '6', '7':
					// Octal escape: \NNN (up to 3 digits)
					val := ch - '0'
					for i := 0; i < 2; i++ {
						if t.pos+1 < len(t.input) && t.input[t.pos+1] >= '0' && t.input[t.pos+1] <= '7' {
							t.advance()
							val = val*8 + (t.input[t.pos] - '0')
						} else {
							break
						}
					}
					sb.WriteByte(val)
				default:
					t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Invalid escape sequence in string literal."})
					sb.WriteByte(ch)
				}
				t.advance()
			}
		} else {
			sb.WriteByte(t.input[t.pos])
			t.advance()
		}
	}
	if t.pos < len(t.input) && t.input[t.pos] == quote {
		t.advance() // skip closing quote
	} else if t.pos >= len(t.input) {
		t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Unexpected end of string."})
	}
	t.tokens = append(t.tokens, Token{Type: TokenString, Value: sb.String(), Line: startLine, Column: startCol, RawLen: t.col - startCol})
}

func (t *Tokenizer) readNumber() {
	startLine := t.line
	startCol := t.col
	start := t.pos
	isFloat := false

	if t.input[t.pos] == '0' && t.pos+1 < len(t.input) && (t.input[t.pos+1] == 'x' || t.input[t.pos+1] == 'X') {
		t.advance()
		t.advance()
		for t.pos < len(t.input) && isHexDigit(t.input[t.pos]) {
			t.advance()
		}
	} else {
		for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
			t.advance()
		}
		if t.pos < len(t.input) && t.input[t.pos] == '.' {
			isFloat = true
			t.advance()
			for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
				t.advance()
			}
		}
		if t.pos < len(t.input) && (t.input[t.pos] == 'e' || t.input[t.pos] == 'E') {
			isFloat = true
			t.advance()
			if t.pos < len(t.input) && (t.input[t.pos] == '+' || t.input[t.pos] == '-') {
				t.advance()
			}
			for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
				t.advance()
			}
		}
	}

	tokType := TokenInt
	if isFloat {
		tokType = TokenFloat
	}
	t.tokens = append(t.tokens, Token{Type: tokType, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
}

// readFloatStartingWithDot handles float literals that begin with '.' (e.g., .5, .25).
func (t *Tokenizer) readFloatStartingWithDot() {
	startLine := t.line
	startCol := t.col
	start := t.pos
	t.advance() // skip '.'
	for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
		t.advance()
	}
	if t.pos < len(t.input) && (t.input[t.pos] == 'e' || t.input[t.pos] == 'E') {
		t.advance()
		if t.pos < len(t.input) && (t.input[t.pos] == '+' || t.input[t.pos] == '-') {
			t.advance()
		}
		for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
			t.advance()
		}
	}
	t.tokens = append(t.tokens, Token{Type: TokenFloat, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
}

func (t *Tokenizer) readIdent() {
	startLine := t.line
	startCol := t.col
	start := t.pos
	for t.pos < len(t.input) && isIdentPart(t.input[t.pos]) {
		t.advance()
	}
	t.tokens = append(t.tokens, Token{Type: TokenIdent, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
}

const tabWidth = 8

func (t *Tokenizer) advance() {
	if t.pos < len(t.input) {
		if t.input[t.pos] == '\n' {
			t.line++
			t.col = 0
		} else if t.input[t.pos] == '\t' {
			t.col += tabWidth - t.col%tabWidth
		} else {
			t.col++
		}
		t.pos++
	}
}

// Peek returns the current token without advancing.
func (t *Tokenizer) Peek() Token {
	if t.idx < len(t.tokens) {
		return t.tokens[t.idx]
	}
	return Token{Type: TokenEOF}
}

// PeekAt returns the token at offset positions ahead without advancing.
func (t *Tokenizer) PeekAt(offset int) Token {
	idx := t.idx + offset
	if idx < len(t.tokens) {
		return t.tokens[idx]
	}
	return Token{Type: TokenEOF}
}

// Next returns the current token and advances.
func (t *Tokenizer) Next() Token {
	tok := t.Peek()
	if t.idx < len(t.tokens) {
		t.idx++
	}
	return tok
}

// Expect consumes a token matching the expected value, or returns an error.
func (t *Tokenizer) Expect(value string) (Token, error) {
	tok := t.Next()
	if tok.Value != value {
		return tok, fmt.Errorf("%d:%d: Expected %q.", tok.Line+1, tok.Column+1, value)
	}
	return tok, nil
}

// ExpectIdent consumes an identifier token, or returns an error.
func (t *Tokenizer) ExpectIdent() (Token, error) {
	tok := t.Next()
	if tok.Type != TokenIdent {
		return tok, fmt.Errorf("%d:%d: Expected identifier.", tok.Line+1, tok.Column+1)
	}
	return tok, nil
}

// ExpectInt consumes an integer token, or returns an error.
func (t *Tokenizer) ExpectInt() (Token, error) {
	tok := t.Next()
	if tok.Type != TokenInt {
		return tok, fmt.Errorf("%d:%d: Expected integer.", tok.Line+1, tok.Column+1)
	}
	return tok, nil
}

// ExpectString consumes a string token, or returns an error.
func (t *Tokenizer) ExpectString() (Token, error) {
	tok := t.Next()
	if tok.Type != TokenString {
		return tok, fmt.Errorf("%d:%d: Expected string.", tok.Line+1, tok.Column+1)
	}
	return tok, nil
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}

func isHexDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func hexVal(ch byte) byte {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0'
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10
	case ch >= 'A' && ch <= 'F':
		return ch - 'A' + 10
	}
	return 0
}

// ToJSONName converts a proto field name to its JSON name using proto3 camelCase rules.
func ToJSONName(name string) string {
	var result strings.Builder
	upper := false
	for _, r := range name {
		if r == '_' {
			upper = true
			continue
		}
		if upper {
			result.WriteRune(unicode.ToUpper(r))
			upper = false
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// readUnicodeHex reads exactly n hex digits and returns the code point value and count read.
func (t *Tokenizer) readUnicodeHex(n int) (uint32, int) {
	var val uint32
	count := 0
	for i := 0; i < n && t.pos < len(t.input) && isHexDigit(t.input[t.pos]); i++ {
		val = val*16 + uint32(hexVal(t.input[t.pos]))
		t.advance()
		count++
	}
	return val, count
}

func isHeadSurrogate(cp uint32) bool { return cp >= 0xD800 && cp < 0xDC00 }
func isTrailSurrogate(cp uint32) bool { return cp >= 0xDC00 && cp < 0xE000 }
func assembleUTF16(head, trail uint32) uint32 {
	return 0x10000 + (head-0xD800)*0x400 + (trail - 0xDC00)
}

func appendUTF8(sb *strings.Builder, cp uint32) {
	if cp <= 0x10FFFF {
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], rune(cp))
		sb.Write(buf[:n])
	} else {
		fmt.Fprintf(sb, "\\U%08x", cp)
	}
}
