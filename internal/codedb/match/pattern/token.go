package pattern

import "fmt"

// TokenKind identifies the type of a lexer token.
type TokenKind int

const (
	EOF      TokenKind = iota
	IDENT              // foo, bar, None, null
	METAVAR            // $X, $_, $...ARGS
	NUMBER             // 42, 3.14
	STRING             // "hello", 'world'
	ELLIPSIS           // ...
	DEEP_OPEN          // <...
	DEEP_CLOSE         // ...>
	LPAREN             // (
	RPAREN             // )
	LBRACE             // {
	RBRACE             // }
	LBRACKET           // [
	RBRACKET           // ]
	DOT                // .
	COMMA              // ,
	COLON              // :
	SEMICOLON          // ;
	OP                 // ==, !=, <, >, <=, >=, +, -, *, /, &&, ||
	ASSIGN             // =, :=, +=
	KEYWORD            // if, def, func, function, class, for, while, return, import
	NEWLINE            // \n
)

// Token is a single lexer token.
type Token struct {
	Kind  TokenKind
	Value string
	Pos   int // byte offset in input
}

func (t Token) String() string {
	return fmt.Sprintf("%v(%q)", t.Kind, t.Value)
}

var keywords = map[string]bool{
	"if": true, "def": true, "func": true, "function": true,
	"class": true, "for": true, "while": true, "return": true, "import": true,
}
