// Package integrity finds signs of copying in a contest's submissions
// (cache/v1/integrity-plan.md, decision D49): similar code by winnowing
// normalised tokens, identical wrong answers, hard-coded expected outputs,
// and probing. It produces evidence for a person to review, never a
// verdict: no AI-likelihood score, no automatic penalty.
package integrity

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Token is one normalised token: identifiers become "I", numbers "N" and
// string or character literals "S"; keywords and operators keep their
// text. Start and End are byte offsets in the source, for highlighting.
type Token struct {
	Text       string
	Start, End int
}

// maxSource bounds the work per program; submissions are at most 64 KB.
const maxSource = 128 << 10

var keywords = map[string]map[string]bool{
	"cpp17": set(`alignas alignof and asm auto bool break case catch char class const constexpr
		const_cast continue decltype default delete do double dynamic_cast else enum explicit extern
		false float for friend goto if inline int long mutable namespace new noexcept not nullptr
		operator or private protected public register reinterpret_cast return short signed sizeof
		static static_assert static_cast struct switch template this throw true try typedef typeid
		typename union unsigned using virtual void volatile while xor`),
	"java21": set(`abstract assert boolean break byte case catch char class const continue default
		do double else enum extends final finally float for goto if implements import instanceof int
		interface long native new package private protected public return short static strictfp super
		switch synchronized this throw throws transient try void volatile while var record yield
		true false null`),
	"python3": set(`False None True and as assert async await break class continue def del elif
		else except finally for from global if import in is lambda nonlocal not or pass raise return
		try while with yield match case`),
	"node22": set(`await break case catch class const continue debugger default delete do else
		export extends false finally for function if import in instanceof let new null return super
		switch this throw true try typeof var void while with yield async of undefined`),
}

func set(words string) map[string]bool {
	m := map[string]bool{}
	start := -1
	for i := 0; i <= len(words); i++ {
		if i == len(words) || words[i] == ' ' || words[i] == '\n' || words[i] == '\t' {
			if start >= 0 {
				m[words[start:i]] = true
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	return m
}

// operators are matched longest first.
var operators = []string{
	">>>=", "<<=", ">>=", ">>>", "...", "**=", "//=", "<=>", "->*",
	"==", "!=", "<=", ">=", "&&", "||", "++", "--", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=",
	"<<", ">>", "->", "::", "**", "//", "=>", "?.", "??",
}

// Lex returns the normalised tokens of a program in language lang (a
// judge language id). Unknown languages are lexed with C-like rules.
func Lex(lang, src string) []Token {
	if len(src) > maxSource {
		src = src[:maxSource]
	}
	l := lexer{src: src, lang: lang, kw: keywords[lang]}
	l.run()
	return l.out
}

type lexer struct {
	src  string
	lang string
	kw   map[string]bool
	pos  int
	out  []Token
	// lineStart is true until the first token of a line, for C++
	// preprocessor lines.
	lineStart bool
}

func (l *lexer) emit(text string, start int) {
	l.out = append(l.out, Token{Text: text, Start: start, End: l.pos})
	l.lineStart = false
}

func (l *lexer) python() bool { return l.lang == "python3" }

func (l *lexer) run() {
	l.lineStart = true
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		start := l.pos
		switch {
		case c == '\n':
			l.pos++
			l.lineStart = true
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.pos++
		case l.python() && c == '#':
			l.skipLine()
		case !l.python() && c == '/' && l.peek(1) == '/':
			l.skipLine()
		case !l.python() && c == '/' && l.peek(1) == '*':
			l.skipBlockComment()
		case l.lang == "cpp17" && c == '#' && l.lineStart:
			// Preprocessor lines (#include, #define) are template noise.
			l.skipDirective()
		case c == '"' || c == '\'' || (c == '`' && l.lang == "node22"):
			l.str()
			l.emit("S", start)
		case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
			l.number()
			l.emit("N", start)
		case isIdentStart(l.src, l.pos):
			l.ident()
			word := l.src[start:l.pos]
			if l.lang == "cpp17" && cppRawPrefixes[word] && l.pos < len(l.src) && l.src[l.pos] == '"' {
				l.rawCpp()
				l.emit("S", start)
				continue
			}
			if l.stringPrefix(word) {
				l.str()
				l.emit("S", start)
				continue
			}
			if l.kw[word] {
				l.emit(word, start)
			} else {
				l.emit("I", start)
			}
		default:
			l.operator()
			l.emit(l.src[start:l.pos], start)
		}
	}
}

func (l *lexer) peek(n int) byte {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *lexer) skipLine() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
}

func (l *lexer) skipBlockComment() {
	l.pos += 2
	for l.pos < len(l.src) {
		if l.src[l.pos] == '*' && l.peek(1) == '/' {
			l.pos += 2
			return
		}
		l.pos++
	}
}

// skipDirective skips a preprocessor line and its backslash continuations.
func (l *lexer) skipDirective() {
	for l.pos < len(l.src) {
		if l.src[l.pos] == '\\' && l.peek(1) == '\n' {
			l.pos += 2
			continue
		}
		if l.src[l.pos] == '\n' {
			return
		}
		l.pos++
	}
}

// stringPrefix reports whether word, just read, prefixes a string literal
// that starts at l.pos: Python r, b, f, rb and the like, C++ R, u8, L.
func (l *lexer) stringPrefix(word string) bool {
	if l.pos >= len(l.src) || (l.src[l.pos] != '"' && l.src[l.pos] != '\'') {
		return false
	}
	switch l.lang {
	case "python3":
		switch lower(word) {
		case "r", "b", "f", "u", "rb", "br", "fr", "rf":
			return true
		}
	case "cpp17":
		switch word {
		case "u8", "u", "U", "L":
			return true
		}
	}
	return false
}

// cppRawPrefixes start a C++ raw string literal R"delim(...)delim".
var cppRawPrefixes = map[string]bool{"R": true, "u8R": true, "uR": true, "UR": true, "LR": true}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// str consumes a string or character literal starting at l.pos: escapes,
// Python and Java triple quotes, and JavaScript template literals with
// nested ${} expressions.
func (l *lexer) str() {
	if l.pos >= len(l.src) {
		return
	}
	q := l.src[l.pos]
	if (l.python() || l.lang == "java21") && q != '`' && l.peek(1) == q && l.peek(2) == q {
		l.pos += 3
		for l.pos < len(l.src) {
			if l.src[l.pos] == '\\' {
				l.pos += 2
				continue
			}
			if l.src[l.pos] == q && l.peek(1) == q && l.peek(2) == q {
				l.pos += 3
				return
			}
			l.pos++
		}
		l.pos = len(l.src)
		return
	}
	l.pos++
	depth := 0
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\\':
			l.pos += 2
			continue
		case q == '`' && c == '$' && l.peek(1) == '{':
			depth++
			l.pos += 2
			continue
		case q == '`' && depth > 0 && c == '{':
			depth++
		case q == '`' && depth > 0 && c == '}':
			depth--
		case c == q && depth == 0:
			l.pos++
			return
		case c == '\n' && q != '`':
			return // unterminated: stop at the line end
		}
		l.pos++
	}
	l.pos = len(l.src)
}

// rawCpp consumes a C++ raw string R"delim(...)delim" whose quote is at
// l.pos. A delimiter is at most 16 characters; anything else is read as
// an ordinary string.
func (l *lexer) rawCpp() {
	open := l.pos + 1
	paren := open
	for paren < len(l.src) && paren-open <= 16 && l.src[paren] != '(' && l.src[paren] != '"' && l.src[paren] != '\n' {
		paren++
	}
	if paren >= len(l.src) || l.src[paren] != '(' {
		l.str()
		return
	}
	closing := ")" + l.src[open:paren] + "\""
	if i := strings.Index(l.src[paren+1:], closing); i >= 0 {
		l.pos = paren + 1 + i + len(closing)
		return
	}
	l.pos = len(l.src)
}

func (l *lexer) number() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if isDigit(c) || isLetter(c) || c == '.' || c == '_' || c == '\'' && l.lang == "cpp17" {
			l.pos++
			continue
		}
		if (c == '+' || c == '-') && l.pos > 0 {
			p := l.src[l.pos-1] | 0x20
			if p == 'e' || p == 'p' {
				l.pos++
				continue
			}
		}
		return
	}
}

func (l *lexer) ident() {
	for l.pos < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		if r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			l.pos += size
			continue
		}
		return
	}
}

func (l *lexer) operator() {
	for _, op := range operators {
		if strings.HasPrefix(l.src[l.pos:], op) {
			// "//" reaches here only in Python (floor division); elsewhere
			// it starts a comment and was skipped.
			l.pos += len(op)
			return
		}
	}
	_, size := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += size
}

func isDigit(c byte) bool  { return '0' <= c && c <= '9' }
func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isIdentStart(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r == '_' || r == '$' || unicode.IsLetter(r)
}
