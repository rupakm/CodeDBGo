package pattern

import "testing"

func TestLexSimpleCall(t *testing.T) {
	tokens := Lex("foo($X, ..., $X)")
	want := []TokenKind{IDENT, LPAREN, METAVAR, COMMA, ELLIPSIS, COMMA, METAVAR, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	for i, tok := range tokens {
		if tok.Kind != want[i] {
			t.Errorf("token[%d] = %v, want %v", i, tok.Kind, want[i])
		}
	}
	if tokens[2].Value != "X" {
		t.Errorf("metavar name = %q, want %q", tokens[2].Value, "X")
	}
}

func TestLexMethodCall(t *testing.T) {
	tokens := Lex("$X.save()")
	want := []TokenKind{METAVAR, DOT, IDENT, LPAREN, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexAssignment(t *testing.T) {
	tokens := Lex("$X = $Y")
	want := []TokenKind{METAVAR, ASSIGN, METAVAR, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexBinaryOp(t *testing.T) {
	tokens := Lex("$X == None")
	want := []TokenKind{METAVAR, OP, IDENT, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[1].Value != "==" {
		t.Errorf("op = %q, want %q", tokens[1].Value, "==")
	}
}

func TestLexDeepExpr(t *testing.T) {
	tokens := Lex("<... $X ...>")
	want := []TokenKind{DEEP_OPEN, METAVAR, DEEP_CLOSE, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexKeyword(t *testing.T) {
	tokens := Lex("if $X: ...")
	want := []TokenKind{KEYWORD, METAVAR, COLON, ELLIPSIS, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[0].Value != "if" {
		t.Errorf("keyword = %q, want %q", tokens[0].Value, "if")
	}
}

func TestLexFuncDef(t *testing.T) {
	tokens := Lex("def $F(...): ...")
	want := []TokenKind{KEYWORD, METAVAR, LPAREN, ELLIPSIS, RPAREN, COLON, ELLIPSIS, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
}

func TestLexString(t *testing.T) {
	tokens := Lex(`foo("hello")`)
	want := []TokenKind{IDENT, LPAREN, STRING, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[2].Value != "hello" {
		t.Errorf("string = %q, want %q", tokens[2].Value, "hello")
	}
}

func TestLexWildcard(t *testing.T) {
	tokens := Lex("foo($_, $_)")
	want := []TokenKind{IDENT, LPAREN, METAVAR, COMMA, METAVAR, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[2].Value != "_" {
		t.Errorf("wildcard metavar = %q, want %q", tokens[2].Value, "_")
	}
}

func TestLexEllipsisMetavar(t *testing.T) {
	tokens := Lex("foo($...ARGS)")
	want := []TokenKind{IDENT, LPAREN, METAVAR, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	// $...ARGS is a METAVAR with Value "...ARGS"
	if tokens[2].Value != "...ARGS" {
		t.Errorf("ellipsis metavar = %q, want %q", tokens[2].Value, "...ARGS")
	}
}

func TestLexGoShortDecl(t *testing.T) {
	tokens := Lex("$X := $Y")
	want := []TokenKind{METAVAR, ASSIGN, METAVAR, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[1].Value != ":=" {
		t.Errorf("assign = %q, want %q", tokens[1].Value, ":=")
	}
}

func TestLexNumber(t *testing.T) {
	tokens := Lex("foo(42)")
	want := []TokenKind{IDENT, LPAREN, NUMBER, RPAREN, EOF}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(want), tokens)
	}
	if tokens[2].Value != "42" {
		t.Errorf("number = %q, want %q", tokens[2].Value, "42")
	}
}
