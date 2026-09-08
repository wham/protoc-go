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

// indexedComments is one sparse entry of the token→comments mapping: cd holds
// the comments preceding token slot idx. Most tokens have none, so storing
// only the occupied slots replaces what used to be a dense array parallel to
// every token.
type indexedComments struct {
	idx int
	cd  TokenComments
}

type Tokenizer struct {
	input        string
	pos          int
	line         int // 0-based
	col          int // 0-based
	tokens       []Token
	comments     []indexedComments // sparse, ascending by idx
	commentSlots int               // next comment slot (mirrors the old dense length)
	idx          int
	Errors       []TokenError
}

// symbolStrings caches single-byte strings to avoid allocations in tokenize.
var symbolStrings [128]string

func init() {
	for i := range symbolStrings {
		symbolStrings[i] = string(rune(i))
	}
}

func New(input string) *Tokenizer {
	// Size the token slice from two upper bounds and take the tighter: the
	// densest realistic input runs ~4 bytes per token, and no file has more
	// than about four tokens per `;`, `=`, `{` or `}` (real ones 2-3.5,
	// generated ones 2.6). Bytes alone overshoot commented files two- to
	// three-fold; the punctuation count, four SIMD scans, brings them within
	// ~30%. push extrapolates the rest if either bound was still wrong.
	est := len(input) / 4
	if punct := 4*(strings.Count(input, ";")+strings.Count(input, "=")+strings.Count(input, "{")+strings.Count(input, "}")) + 64; punct < est {
		est = punct
	}
	if est < 64 {
		est = 64
	}
	t := &Tokenizer{
		input:  input,
		tokens: make([]Token, 0, est),
	}
	// Skip UTF-8 BOM if present (matching C++ protoc behavior).
	// Keep in input so positions account for BOM bytes.
	if len(input) >= 3 && input[0] == 0xEF && input[1] == 0xBB && input[2] == 0xBF {
		t.pos = 3
		t.col = 3
	}
	t.tokenize()
	return t
}

// push appends a token. When the slice is full it is regrown to the count the
// whole input projects to at the density tokenized so far, rather than doubled,
// so an estimate that fell a little short costs a little, and a multi-MB input
// settles its final size in one or two copies. It always grows by at least a
// quarter, so a run of bad projections still stays a short one.
func (t *Tokenizer) push(tok Token) {
	if len(t.tokens) == cap(t.tokens) {
		n := len(t.tokens)
		want := n + n/4
		if t.pos > 0 {
			if proj := int(float64(n) * float64(len(t.input)) / float64(t.pos)); proj+proj/8 > want {
				want = proj + proj/8
			}
		}
		grown := make([]Token, n, want+16)
		copy(grown, t.tokens)
		t.tokens = grown
	}
	t.tokens = append(t.tokens, tok)
}

func (t *Tokenizer) tokenize() {
	prevTokenLine := -1 // no previous token
	for t.pos < len(t.input) {
		cd := t.collectComments(prevTokenLine)
		if t.pos >= len(t.input) {
			t.addComments(cd)
			break
		}

		ch := t.input[t.pos]
		t.addComments(cd)

		// Check for control characters (null byte and unprintable bytes 1-31
		// excluding whitespace chars that are already consumed by collectComments).
		if isControlChar(ch) {
			t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Invalid control characters encountered in text."})
			t.advance()
			for t.pos < len(t.input) && isControlChar(t.input[t.pos]) {
				t.advance()
			}
			continue
		}

		if ch == '"' || ch == '\'' {
			t.readString()
		} else if ch >= '0' && ch <= '9' {
			t.readNumber()
			if t.pos < len(t.input) && isIdentStart(t.input[t.pos]) {
				t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Need space between number and identifier."})
			}
		} else if ch == '.' && t.pos+1 < len(t.input) && t.input[t.pos+1] >= '0' && t.input[t.pos+1] <= '9' {
			t.readFloatStartingWithDot()
			if t.pos < len(t.input) && isIdentStart(t.input[t.pos]) {
				t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Need space between number and identifier."})
			}
		} else if isIdentStart(ch) {
			t.readIdent()
		} else {
			if ch&0x80 != 0 {
				t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: fmt.Sprintf("Interpreting non ascii codepoint %d.", ch)})
			}
			var symStr string
			if ch < 128 {
				symStr = symbolStrings[ch]
			} else {
				symStr = string(ch)
			}
			t.push(Token{Type: TokenSymbol, Value: symStr, Line: t.line, Column: t.col})
			t.advance()
		}
		prevTokenLine = t.tokens[len(t.tokens)-1].Line
	}
	// EOF token
	if t.commentSlots < len(t.tokens)+1 {
		t.addComments(t.collectComments(prevTokenLine))
	}
	t.push(Token{Type: TokenEOF, Value: "", Line: t.line, Column: t.col})
}

// addComments records cd for the next comment slot, storing only non-empty
// entries. The slot counter advances either way so sparse indexes stay
// identical to the dense array this replaces.
func (t *Tokenizer) addComments(cd TokenComments) {
	if cd.PrevTrailing != "" || cd.Leading != "" || len(cd.Detached) > 0 {
		t.comments = append(t.comments, indexedComments{idx: t.commentSlots, cd: cd})
	}
	t.commentSlots++
}

// collectComments scans whitespace and comments between tokens, classifying
// them as trailing (of prev token), detached, or leading (of next token).
// Mirrors C++ CommentCollector logic from tokenizer.cc.
func (t *Tokenizer) collectComments(prevTokenLine int) TokenComments {
	var result TokenComments
	canAttachToPrev := prevTokenLine >= 0
	var commentBuf commentText
	hasComment := false
	isLineComment := false

	// Phase 1: Check for trailing comment on same line as previous token
	if canAttachToPrev {
		// Skip non-newline whitespace
		for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r' || t.input[t.pos] == '\v' || t.input[t.pos] == '\f') {
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
			for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r' || t.input[t.pos] == '\v' || t.input[t.pos] == '\f') {
				t.advance()
			}
			if t.pos < len(t.input) && t.input[t.pos] == '\n' {
				t.advance()
			}
		} else if t.input[t.pos] == '\n' {
			// C++ protoc: after consuming the newline, can_attach_to_prev_
			// stays true. A comment on the next line (before a blank line)
			// is still considered trailing of the previous token.
			t.advance()
		} else {
			// Next token on same line, no comments
			return result
		}
	}

	// Phase 2: Collect remaining comments, detect blank lines for detachment
	for t.pos < len(t.input) {
		// Skip non-newline whitespace
		for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r' || t.input[t.pos] == '\v' || t.input[t.pos] == '\f') {
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
			commentBuf.add(text)
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
			commentBuf.add(text)
			hasComment = true
			isLineComment = false
			// Consume trailing whitespace and newline
			for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\r' || t.input[t.pos] == '\v' || t.input[t.pos] == '\f') {
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
				hasComment = false
			}
			canAttachToPrev = false
			t.advance()
		} else {
			// Non-comment, non-whitespace → next token found
			break
		}
	}

	// C++ protoc flushes the comment buffer when the next token is }, ], or )
	// (end of scope). The flushed comment becomes trailing (if canAttachToPrev)
	// or detached (if not). Otherwise it becomes leading of the next token.
	if hasComment {
		if t.pos < len(t.input) && (t.input[t.pos] == '}' || t.input[t.pos] == ']' || t.input[t.pos] == ')') {
			if canAttachToPrev {
				result.PrevTrailing = commentBuf.String()
			} else {
				result.Detached = append(result.Detached, commentBuf.String())
			}
		} else {
			result.Leading = commentBuf.String()
		}
	}

	return result
}

// commentText accumulates the pieces of one comment. A single piece, which is
// most comments, is handed back as the substring of the input it already is;
// only a run of consecutive line comments is joined in a builder.
type commentText struct {
	first string
	n     int
	buf   strings.Builder
}

func (c *commentText) add(s string) {
	switch c.n {
	case 0:
		c.first = s
	case 1:
		c.buf.WriteString(c.first)
		fallthrough
	default:
		c.buf.WriteString(s)
	}
	c.n++
}

func (c *commentText) String() string {
	if c.n <= 1 {
		return c.first
	}
	return c.buf.String()
}

func (c *commentText) Reset() {
	c.first = ""
	c.n = 0
	c.buf.Reset()
}

func (t *Tokenizer) flushComment(result *TokenComments, buf *commentText, canAttachToPrev bool) {
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
	for t.pos < len(t.input) && t.input[t.pos] != '\n' && t.input[t.pos] != 0 {
		t.advance()
	}
	if t.pos < len(t.input) && t.input[t.pos] == '\n' {
		t.advance() // skip \n, which the text keeps
		return t.input[start:t.pos]
	}
	// EOF or null byte without trailing newline
	return t.input[start:t.pos]
}

// readBlockCommentText reads text between /* and */, returns content without delimiters.
// Mirrors C++ Tokenizer::ConsumeBlockComment: after each newline, strips leading
// whitespace and one leading '*' (if not followed by '/').
// startLine and startCol are the 0-based position of the '/' in '/*'.
func (t *Tokenizer) readBlockCommentText(startLine, startCol int) string {
	var buf strings.Builder
	for t.pos < len(t.input) {
		ch := t.input[t.pos]

		if ch == '\n' {
			buf.WriteByte('\n')
			t.advance()
			// Strip leading non-newline whitespace
			for t.pos < len(t.input) {
				c := t.input[t.pos]
				if c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f' {
					t.advance()
				} else {
					break
				}
			}
			// Strip one leading '*' if not followed by '/'
			if t.pos < len(t.input) && t.input[t.pos] == '*' {
				if t.pos+1 < len(t.input) && t.input[t.pos+1] == '/' {
					t.advance() // *
					t.advance() // /
					return buf.String()
				}
				t.advance() // strip the *
			}
			continue
		}

		if ch == '*' {
			if t.pos+1 < len(t.input) && t.input[t.pos+1] == '/' {
				t.advance() // *
				t.advance() // /
				return buf.String()
			}
			buf.WriteByte(ch)
			t.advance()
			continue
		}

		if ch == '/' && t.pos+1 < len(t.input) && t.input[t.pos+1] == '*' {
			// Nested block comment — consume '/' but not '*' (so '*/' can end comment).
			t.advance()
			t.Errors = append(t.Errors, TokenError{
				Line: t.line, Column: t.col,
				Message: `"/*" inside block comment.  Block comments cannot be nested.`,
			})
			buf.WriteByte(ch)
			continue
		}

		if ch == 0 {
			// Null byte terminates block comment (same as EOF in C++ protoc)
			break
		}

		buf.WriteByte(ch)
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
	lo, hi := 0, len(t.comments)
	for lo < hi {
		mid := (lo + hi) / 2
		if t.comments[mid].idx < i {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(t.comments) && t.comments[lo].idx == i {
		return t.comments[lo].cd
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
		if t.input[t.pos] == 0 {
			t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Unexpected end of string."})
			break
		}
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
					// Unicode escape: \UNNNNNNNN (exactly 8 hex digits, up to 10ffff)
					// C++ validates digit-by-digit: 00[01]XXXXX, error at first failing digit
					t.advance()
					hexStart := t.col
					cp, cnt := t.readUnicodeHex(8)
					if cnt < 8 {
						t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: "Expected eight hex digits for \\U escape sequence."})
						continue
					}
					if cp > 0x1FFFFF {
						errCol := hexStart
						d0 := (cp >> 28) & 0xF
						d1 := (cp >> 24) & 0xF
						if d0 != 0 {
							errCol = hexStart
						} else if d1 != 0 {
							errCol = hexStart + 1
						} else {
							errCol = hexStart + 2
						}
						t.Errors = append(t.Errors, TokenError{Line: t.line, Column: errCol, Message: "Expected eight hex digits up to 10ffff for \\U escape sequence"})
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
	t.push(Token{Type: TokenString, Value: sb.String(), Line: startLine, Column: startCol, RawLen: t.col - startCol})
}

func (t *Tokenizer) readNumber() {
	startLine := t.line
	startCol := t.col
	start := t.pos
	isFloat := false

	if t.input[t.pos] == '0' && t.pos+1 < len(t.input) && (t.input[t.pos+1] == 'x' || t.input[t.pos+1] == 'X') {
		t.advance()
		t.advance()
		hexStart := t.pos
		for t.pos < len(t.input) && isHexDigit(t.input[t.pos]) {
			t.advance()
		}
		if t.pos == hexStart {
			// "0x" with no hex digits following
			t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: `"0x" must be followed by hex digits.`})
			// Still emit a token so the parser sees something
			t.push(Token{Type: TokenInt, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
			return
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
			expStart := t.pos
			for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
				t.advance()
			}
			if t.pos == expStart {
				t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: `"e" must be followed by exponent.`})
			}
		}
	}

	tokType := TokenInt
	if isFloat {
		tokType = TokenFloat
	}
	t.push(Token{Type: tokType, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
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
		expStart := t.pos
		for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
			t.advance()
		}
		if t.pos == expStart {
			t.Errors = append(t.Errors, TokenError{Line: t.line, Column: t.col, Message: `"e" must be followed by exponent.`})
		}
	}
	t.push(Token{Type: TokenFloat, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
}

func (t *Tokenizer) readIdent() {
	startLine := t.line
	startCol := t.col
	start := t.pos
	for t.pos < len(t.input) && isIdentPart(t.input[t.pos]) {
		t.advance()
	}
	t.push(Token{Type: TokenIdent, Value: t.input[start:t.pos], Line: startLine, Column: startCol})
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

// Len returns the total number of tokens (including EOF).
func (t *Tokenizer) Len() int {
	return len(t.tokens)
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

// isControlChar returns true for null byte and unprintable ASCII control characters
// (bytes 1-31) excluding whitespace characters (tab, newline, carriage return,
// vertical tab, form feed) which are handled elsewhere.
func isControlChar(ch byte) bool {
	if ch == 0 {
		return true
	}
	if ch < ' ' && ch != '\t' && ch != '\n' && ch != '\r' && ch != '\v' && ch != '\f' {
		return true
	}
	return false
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
	// Without an underscore the JSON name is the field name itself; skip the
	// copy. Field names are identifiers, so this is the common case.
	i := strings.IndexByte(name, '_')
	if i < 0 {
		return name
	}
	var result strings.Builder
	result.Grow(len(name))
	result.WriteString(name[:i])
	upper := false
	for ; i < len(name); i++ {
		c := name[i]
		if c == '_' {
			upper = true
			continue
		}
		if c >= utf8.RuneSelf {
			return toJSONNameSlow(&result, name[i:], upper)
		}
		if upper {
			if 'a' <= c && c <= 'z' {
				c -= 'a' - 'A'
			}
			upper = false
		}
		result.WriteByte(c)
	}
	return result.String()
}

// toJSONNameSlow finishes ToJSONName rune by rune from the first non-ASCII
// byte, preserving the original unicode.ToUpper behaviour.
func toJSONNameSlow(result *strings.Builder, rest string, upper bool) string {
	for _, r := range rest {
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
	if cp >= 0xD800 && cp <= 0xDFFF {
		// Surrogate codepoints: Go's utf8.EncodeRune replaces these with U+FFFD,
		// but C++ protoc encodes them as raw UTF-8 bytes. Manually encode.
		sb.WriteByte(byte(0xE0 | (cp >> 12)))
		sb.WriteByte(byte(0x80 | ((cp >> 6) & 0x3F)))
		sb.WriteByte(byte(0x80 | (cp & 0x3F)))
	} else if cp <= 0x10FFFF {
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], rune(cp))
		sb.Write(buf[:n])
	} else {
		fmt.Fprintf(sb, "\\U%08x", cp)
	}
}
