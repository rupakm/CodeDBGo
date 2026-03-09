package pattern

import (
	"fmt"
	"strings"
)

// Parse parses a pattern string into a PatternNode IR tree.
func Parse(input string) (PatternNode, error) {
	tokens := Lex(input)
	p := &parser{tokens: tokens}
	node, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	// Check for trailing assignment or binary op
	node, err = p.maybeInfix(node)
	if err != nil {
		return nil, err
	}
	return node, nil
}

type parser struct {
	tokens []Token
	pos    int
}

func (p *parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: EOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() Token {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *parser) expect(kind TokenKind) (Token, error) {
	tok := p.peek()
	if tok.Kind != kind {
		return tok, fmt.Errorf("expected %v, got %v at pos %d", kind, tok.Kind, tok.Pos)
	}
	return p.advance(), nil
}

func (p *parser) skipNewlines() {
	for p.peek().Kind == NEWLINE {
		p.advance()
	}
}

// parseExpr parses the top-level expression, handling statements like if/func/def.
func (p *parser) parseExpr() (PatternNode, error) {
	p.skipNewlines()
	tok := p.peek()

	// if statement
	if tok.Kind == KEYWORD && tok.Value == "if" {
		return p.parseIfStmt()
	}

	// func/function/def
	if tok.Kind == KEYWORD && (tok.Value == "def" || tok.Value == "func" || tok.Value == "function") {
		return p.parseFuncDef()
	}

	return p.parseUnary()
}

// maybeInfix checks for assignment or binary op after an expression.
func (p *parser) maybeInfix(left PatternNode) (PatternNode, error) {
	tok := p.peek()

	// Assignment: = or :=
	if tok.Kind == ASSIGN {
		p.advance()
		right, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &Assignment{Left: left, Right: right}, nil
	}

	// Binary op
	if tok.Kind == OP {
		p.advance()
		right, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &BinaryOp{Left: left, Op: tok.Value, Right: right}, nil
	}

	return left, nil
}

// parseUnary parses an atom followed by optional trailers (calls, member access).
func (p *parser) parseUnary() (PatternNode, error) {
	node, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	return p.parseTrailers(node)
}

// parseTrailers handles .ident, .ident(...), and (...) after an atom.
func (p *parser) parseTrailers(node PatternNode) (PatternNode, error) {
	for {
		tok := p.peek()

		// Call: node(args)
		if tok.Kind == LPAREN {
			p.advance()
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(RPAREN); err != nil {
				return nil, err
			}
			node = &Call{Func: node, Args: args}
			continue
		}

		// Member access: node.ident or node.ident(args)
		if tok.Kind == DOT {
			p.advance()
			memberTok := p.peek()
			if memberTok.Kind != IDENT && memberTok.Kind != METAVAR {
				return nil, fmt.Errorf("expected identifier after '.', got %v", memberTok.Kind)
			}
			p.advance()
			var member PatternNode
			if memberTok.Kind == METAVAR {
				member = p.metavarNode(memberTok)
			} else {
				member = &Literal{Value: memberTok.Value}
			}

			// Check for method call: node.member(args)
			if p.peek().Kind == LPAREN {
				p.advance()
				args, err := p.parseArgs()
				if err != nil {
					return nil, err
				}
				if _, err := p.expect(RPAREN); err != nil {
					return nil, err
				}
				node = &MethodCall{Object: node, Method: member, Args: args}
			} else {
				// Field access — model as method call with nil args for now
				node = &MethodCall{Object: node, Method: member, Args: nil}
			}
			continue
		}

		break
	}
	return node, nil
}

// parseAtom parses a single atom (leaf or grouped expression).
func (p *parser) parseAtom() (PatternNode, error) {
	tok := p.peek()

	switch tok.Kind {
	case METAVAR:
		p.advance()
		return p.metavarNode(tok), nil

	case IDENT:
		p.advance()
		return &Literal{Value: tok.Value}, nil

	case NUMBER:
		p.advance()
		return &Literal{Value: tok.Value}, nil

	case STRING:
		p.advance()
		return &Literal{Value: `"` + tok.Value + `"`}, nil

	case ELLIPSIS:
		p.advance()
		return &Ellipsis{}, nil

	case DEEP_OPEN:
		p.advance()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		// inner may have trailers
		inner, err = p.parseTrailers(inner)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(DEEP_CLOSE); err != nil {
			return nil, err
		}
		return &DeepExpr{Inner: inner}, nil

	case LPAREN:
		p.advance()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(RPAREN); err != nil {
			return nil, err
		}
		return inner, nil

	case EOF:
		return nil, fmt.Errorf("unexpected end of input")

	default:
		return nil, fmt.Errorf("unexpected token %v(%q) at pos %d", tok.Kind, tok.Value, tok.Pos)
	}
}

// metavarNode creates the appropriate PatternNode for a METAVAR token.
func (p *parser) metavarNode(tok Token) PatternNode {
	if tok.Value == "_" {
		return &Wildcard{}
	}
	if strings.HasPrefix(tok.Value, "...") {
		return &EllipsisMetavar{Name: tok.Value[3:]}
	}
	return &Metavar{Name: tok.Value}
}

// parseArgs parses a comma-separated list of arguments inside parens.
func (p *parser) parseArgs() ([]PatternNode, error) {
	var args []PatternNode
	if p.peek().Kind == RPAREN {
		return args, nil
	}
	for {
		p.skipNewlines()
		arg, err := p.parseArg()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.peek().Kind != COMMA {
			break
		}
		p.advance() // consume comma
	}
	return args, nil
}

// parseArg parses a single argument (which can be an expr, ellipsis, or metavar).
func (p *parser) parseArg() (PatternNode, error) {
	node, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	// Check for infix in arg context
	node, err = p.maybeInfix(node)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// parseIfStmt parses: if EXPR : BLOCK  or  if EXPR { BLOCK }
func (p *parser) parseIfStmt() (PatternNode, error) {
	p.advance() // consume "if"
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	// Check for infix on condition
	cond, err = p.maybeInfix(cond)
	if err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &IfStmt{Cond: cond, Body: body}, nil
}

// parseFuncDef parses: def NAME(PARAMS): BLOCK / func NAME(PARAMS) { BLOCK }
func (p *parser) parseFuncDef() (PatternNode, error) {
	p.advance() // consume "def"/"func"/"function"

	// Name
	nameTok := p.peek()
	if nameTok.Kind != IDENT && nameTok.Kind != METAVAR {
		return nil, fmt.Errorf("expected function name, got %v", nameTok.Kind)
	}
	p.advance()
	var name PatternNode
	if nameTok.Kind == METAVAR {
		name = p.metavarNode(nameTok)
	} else {
		name = &Literal{Value: nameTok.Value}
	}

	// Params
	if _, err := p.expect(LPAREN); err != nil {
		return nil, err
	}
	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(RPAREN); err != nil {
		return nil, err
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	return &FuncDef{Name: name, Params: params, Body: body}, nil
}

// parseParams parses a parameter list.
func (p *parser) parseParams() ([]PatternNode, error) {
	var params []PatternNode
	if p.peek().Kind == RPAREN {
		return params, nil
	}
	for {
		tok := p.peek()
		switch tok.Kind {
		case METAVAR:
			p.advance()
			params = append(params, p.metavarNode(tok))
		case IDENT:
			p.advance()
			params = append(params, &Literal{Value: tok.Value})
		case ELLIPSIS:
			p.advance()
			params = append(params, &Ellipsis{})
		default:
			return nil, fmt.Errorf("unexpected token in params: %v", tok.Kind)
		}
		if p.peek().Kind != COMMA {
			break
		}
		p.advance() // consume comma
	}
	return params, nil
}

// parseBlock parses a block: either `: body` or `{ body }`.
func (p *parser) parseBlock() ([]PatternNode, error) {
	tok := p.peek()

	if tok.Kind == COLON {
		p.advance()
		p.skipNewlines()
		return p.parseBlockBody()
	}

	if tok.Kind == LBRACE {
		p.advance()
		p.skipNewlines()
		body, err := p.parseBlockBody()
		if err != nil {
			return nil, err
		}
		p.skipNewlines()
		if _, err := p.expect(RBRACE); err != nil {
			return nil, err
		}
		return body, nil
	}

	return nil, fmt.Errorf("expected ':' or '{' for block, got %v", tok.Kind)
}

// parseBlockBody parses one or more statements in a block.
func (p *parser) parseBlockBody() ([]PatternNode, error) {
	var stmts []PatternNode
	for {
		p.skipNewlines()
		tok := p.peek()
		if tok.Kind == EOF || tok.Kind == RBRACE {
			break
		}
		stmt, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt, err = p.maybeInfix(stmt)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
		if p.peek().Kind != NEWLINE && p.peek().Kind != SEMICOLON {
			break
		}
		p.advance()
	}
	return stmts, nil
}
