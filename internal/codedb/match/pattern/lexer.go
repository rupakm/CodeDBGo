package pattern

import (
	"unicode"
)

// Lex tokenizes a pattern string into a slice of Tokens.
func Lex(input string) []Token {
	l := &lexer{input: []rune(input)}
	var tokens []Token
	for {
		tok := l.next()
		tokens = append(tokens, tok)
		if tok.Kind == EOF {
			break
		}
	}
	return tokens
}

type lexer struct {
	input []rune
	pos   int
}

func (l *lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *lexer) advance() rune {
	r := l.input[l.pos]
	l.pos++
	return r
}

func (l *lexer) match(s string) bool {
	rs := []rune(s)
	if l.pos+len(rs) > len(l.input) {
		return false
	}
	for i, r := range rs {
		if l.input[l.pos+i] != r {
			return false
		}
	}
	return true
}

func (l *lexer) next() Token {
	// Skip whitespace (except newlines)
	for l.pos < len(l.input) && l.input[l.pos] != '\n' && unicode.IsSpace(l.input[l.pos]) {
		l.pos++
	}

	if l.pos >= len(l.input) {
		return Token{Kind: EOF, Pos: l.pos}
	}

	start := l.pos
	ch := l.peek()

	// Newline
	if ch == '\n' {
		l.advance()
		return Token{Kind: NEWLINE, Value: "\n", Pos: start}
	}

	// Deep expression open: <...
	if ch == '<' && l.match("<...") {
		l.pos += 4
		return Token{Kind: DEEP_OPEN, Value: "<...", Pos: start}
	}

	// Deep expression close: ...>
	if ch == '.' && l.match("...>") {
		l.pos += 4
		return Token{Kind: DEEP_CLOSE, Value: "...>", Pos: start}
	}

	// Ellipsis: ...
	if ch == '.' && l.match("...") {
		l.pos += 3
		return Token{Kind: ELLIPSIS, Value: "...", Pos: start}
	}

	// Metavar: $X, $_, $...ARGS
	if ch == '$' {
		l.advance()
		if l.match("...") {
			l.pos += 3
			name := "..."
			for l.pos < len(l.input) && (unicode.IsUpper(l.input[l.pos]) || l.input[l.pos] == '_' || unicode.IsDigit(l.input[l.pos])) {
				name += string(l.advance())
			}
			return Token{Kind: METAVAR, Value: name, Pos: start}
		}
		if l.pos < len(l.input) && l.input[l.pos] == '_' && (l.pos+1 >= len(l.input) || !isIdentChar(l.input[l.pos+1])) {
			l.advance()
			return Token{Kind: METAVAR, Value: "_", Pos: start}
		}
		name := ""
		for l.pos < len(l.input) && (unicode.IsUpper(l.input[l.pos]) || l.input[l.pos] == '_' || unicode.IsDigit(l.input[l.pos])) {
			name += string(l.advance())
		}
		if name == "" {
			return Token{Kind: IDENT, Value: "$", Pos: start}
		}
		return Token{Kind: METAVAR, Value: name, Pos: start}
	}

	// String literals
	if ch == '"' || ch == '\'' {
		return l.lexString(ch, start)
	}

	// Numbers
	if unicode.IsDigit(ch) || (ch == '-' && l.pos+1 < len(l.input) && unicode.IsDigit(l.input[l.pos+1])) {
		return l.lexNumber(start)
	}

	// Multi-character operators
	if ch == ':' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: ASSIGN, Value: ":=", Pos: start}
	}
	if ch == '+' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: ASSIGN, Value: "+=", Pos: start}
	}
	if ch == '=' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: "==", Pos: start}
	}
	if ch == '!' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: "!=", Pos: start}
	}
	if ch == '<' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: "<=", Pos: start}
	}
	if ch == '>' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
		l.pos += 2
		return Token{Kind: OP, Value: ">=", Pos: start}
	}
	if ch == '&' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '&' {
		l.pos += 2
		return Token{Kind: OP, Value: "&&", Pos: start}
	}
	if ch == '|' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '|' {
		l.pos += 2
		return Token{Kind: OP, Value: "||", Pos: start}
	}

	// Single-character tokens
	l.advance()
	switch ch {
	case '(':
		return Token{Kind: LPAREN, Value: "(", Pos: start}
	case ')':
		return Token{Kind: RPAREN, Value: ")", Pos: start}
	case '{':
		return Token{Kind: LBRACE, Value: "{", Pos: start}
	case '}':
		return Token{Kind: RBRACE, Value: "}", Pos: start}
	case '[':
		return Token{Kind: LBRACKET, Value: "[", Pos: start}
	case ']':
		return Token{Kind: RBRACKET, Value: "]", Pos: start}
	case '.':
		return Token{Kind: DOT, Value: ".", Pos: start}
	case ',':
		return Token{Kind: COMMA, Value: ",", Pos: start}
	case ':':
		return Token{Kind: COLON, Value: ":", Pos: start}
	case ';':
		return Token{Kind: SEMICOLON, Value: ";", Pos: start}
	case '=':
		return Token{Kind: ASSIGN, Value: "=", Pos: start}
	case '<':
		return Token{Kind: OP, Value: "<", Pos: start}
	case '>':
		return Token{Kind: OP, Value: ">", Pos: start}
	case '+', '-', '*', '/':
		return Token{Kind: OP, Value: string(ch), Pos: start}
	}

	// Identifiers and keywords
	if isIdentStart(ch) {
		l.pos = start // back up to include first char
		return l.lexIdent(start)
	}

	// Unknown character — treat as ident
	return Token{Kind: IDENT, Value: string(ch), Pos: start}
}

func (l *lexer) lexString(quote rune, start int) Token {
	l.advance() // consume opening quote
	var val []rune
	for l.pos < len(l.input) && l.input[l.pos] != quote {
		if l.input[l.pos] == '\\' && l.pos+1 < len(l.input) {
			l.advance() // skip backslash
		}
		val = append(val, l.advance())
	}
	if l.pos < len(l.input) {
		l.advance() // consume closing quote
	}
	return Token{Kind: STRING, Value: string(val), Pos: start}
}

func (l *lexer) lexNumber(start int) Token {
	var val []rune
	if l.input[l.pos] == '-' {
		val = append(val, l.advance())
	}
	for l.pos < len(l.input) && (unicode.IsDigit(l.input[l.pos]) || l.input[l.pos] == '.') {
		val = append(val, l.advance())
	}
	return Token{Kind: NUMBER, Value: string(val), Pos: start}
}

func (l *lexer) lexIdent(start int) Token {
	var val []rune
	for l.pos < len(l.input) && isIdentChar(l.input[l.pos]) {
		val = append(val, l.advance())
	}
	name := string(val)
	if keywords[name] {
		return Token{Kind: KEYWORD, Value: name, Pos: start}
	}
	return Token{Kind: IDENT, Value: name, Pos: start}
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}
