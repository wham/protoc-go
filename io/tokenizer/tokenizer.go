// Package tokenizer implements lexical analysis of .proto files.
// This mirrors C++ google::protobuf::io::Tokenizer from io/tokenizer.cc.
package tokenizer

import (
	"fmt"
	"strings"
	"unicode"
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
}

type Tokenizer struct {
	input  string
	pos    int
	line   int // 0-based
	col    int // 0-based
	tokens []Token
	idx    int
}

func New(input string) *Tokenizer {
	t := &Tokenizer{input: input}
	t.tokenize()
	return t
}

func (t *Tokenizer) tokenize() {
	for t.pos < len(t.input) {
		t.skipWhitespaceAndComments()
		if t.pos >= len(t.input) {
			break
		}

		ch := t.input[t.pos]

		if ch == '"' || ch == '\'' {
			t.readString()
		} else if ch >= '0' && ch <= '9' {
			t.readNumber()
		} else if isIdentStart(ch) {
			t.readIdent()
		} else {
			t.tokens = append(t.tokens, Token{Type: TokenSymbol, Value: string(ch), Line: t.line, Column: t.col})
			t.advance()
		}
	}
	t.tokens = append(t.tokens, Token{Type: TokenEOF, Value: "", Line: t.line, Column: t.col})
}

func (t *Tokenizer) skipWhitespaceAndComments() {
	for t.pos < len(t.input) {
		ch := t.input[t.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			t.advance()
			continue
		}
		if ch == '/' && t.pos+1 < len(t.input) {
			if t.input[t.pos+1] == '/' {
				// Line comment
				for t.pos < len(t.input) && t.input[t.pos] != '\n' {
					t.advance()
				}
				continue
			}
			if t.input[t.pos+1] == '*' {
				// Block comment
				t.advance()
				t.advance()
				for t.pos < len(t.input) {
					if t.input[t.pos] == '*' && t.pos+1 < len(t.input) && t.input[t.pos+1] == '/' {
						t.advance()
						t.advance()
						break
					}
					t.advance()
				}
				continue
			}
		}
		break
	}
}

func (t *Tokenizer) readString() {
	quote := t.input[t.pos]
	startLine := t.line
	startCol := t.col
	t.advance() // skip opening quote
	var sb strings.Builder
	for t.pos < len(t.input) && t.input[t.pos] != quote {
		if t.input[t.pos] == '\\' {
			t.advance()
			if t.pos < len(t.input) {
				sb.WriteByte(t.input[t.pos])
				t.advance()
			}
		} else {
			sb.WriteByte(t.input[t.pos])
			t.advance()
		}
	}
	if t.pos < len(t.input) {
		t.advance() // skip closing quote
	}
	t.tokens = append(t.tokens, Token{Type: TokenString, Value: sb.String(), Line: startLine, Column: startCol})
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

func (t *Tokenizer) readIdent() {
	startLine := t.line
	startCol := t.col
	start := t.pos
	for t.pos < len(t.input) && isIdentPart(t.input[t.pos]) {
		t.advance()
	}
	t.tokens = append(t.tokens, Token{Type: TokenIdent, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
}

func (t *Tokenizer) advance() {
	if t.pos < len(t.input) {
		if t.input[t.pos] == '\n' {
			t.line++
			t.col = 0
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
		return tok, fmt.Errorf("line %d:%d: expected %q, got %q", tok.Line+1, tok.Column+1, value, tok.Value)
	}
	return tok, nil
}

// ExpectIdent consumes an identifier token, or returns an error.
func (t *Tokenizer) ExpectIdent() (Token, error) {
	tok := t.Next()
	if tok.Type != TokenIdent {
		return tok, fmt.Errorf("line %d:%d: expected identifier, got %q", tok.Line+1, tok.Column+1, tok.Value)
	}
	return tok, nil
}

// ExpectInt consumes an integer token, or returns an error.
func (t *Tokenizer) ExpectInt() (Token, error) {
	tok := t.Next()
	if tok.Type != TokenInt {
		return tok, fmt.Errorf("line %d:%d: expected integer, got %q", tok.Line+1, tok.Column+1, tok.Value)
	}
	return tok, nil
}

// ExpectString consumes a string token, or returns an error.
func (t *Tokenizer) ExpectString() (Token, error) {
	tok := t.Next()
	if tok.Type != TokenString {
		return tok, fmt.Errorf("line %d:%d: expected string, got %q", tok.Line+1, tok.Column+1, tok.Value)
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
