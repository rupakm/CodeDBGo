package pattern

// PatternNode is the interface for all pattern IR nodes.
type PatternNode interface {
	patternNode()
}

// --- Atom nodes ---

// Literal matches exact text: "foo", "None", "42".
type Literal struct{ Value string }

// Metavar matches any expression and binds to Name. Same name must unify.
type Metavar struct{ Name string }

// Ellipsis matches zero or more items in a sequence.
type Ellipsis struct{}

// EllipsisMetavar matches zero or more items and binds them.
type EllipsisMetavar struct{ Name string }

// Wildcard matches any single node without binding.
type Wildcard struct{}

// --- Compound nodes ---

// Call matches function calls: foo(...) or $F(...).
type Call struct {
	Func PatternNode
	Args []PatternNode
}

// MethodCall matches method calls: $X.foo(...) or $X.$M(...).
type MethodCall struct {
	Object PatternNode
	Method PatternNode
	Args   []PatternNode
}

// BinaryOp matches binary operations: $X == $Y, $X + $Y.
type BinaryOp struct {
	Left  PatternNode
	Op    string
	Right PatternNode
}

// Assignment matches assignments: $X = $Y, $X := $Y.
type Assignment struct {
	Left  PatternNode
	Right PatternNode
}

// IfStmt matches conditionals: if $COND: ... or if $COND { ... }.
type IfStmt struct {
	Cond PatternNode
	Body []PatternNode
}

// FuncDef matches function definitions.
type FuncDef struct {
	Name   PatternNode
	Params []PatternNode
	Body   []PatternNode
}

// DeepExpr matches a pattern nested arbitrarily deep: <... P ...>.
type DeepExpr struct {
	Inner PatternNode
}

// Block matches a sequence of statements.
type Block struct {
	Stmts []PatternNode
}

func (*Literal) patternNode()        {}
func (*Metavar) patternNode()        {}
func (*Ellipsis) patternNode()       {}
func (*EllipsisMetavar) patternNode() {}
func (*Wildcard) patternNode()       {}
func (*Call) patternNode()           {}
func (*MethodCall) patternNode()     {}
func (*BinaryOp) patternNode()      {}
func (*Assignment) patternNode()    {}
func (*IfStmt) patternNode()        {}
func (*FuncDef) patternNode()       {}
func (*DeepExpr) patternNode()      {}
func (*Block) patternNode()         {}

// MatchFormula is the interface for boolean combinators over patterns.
type MatchFormula interface {
	matchFormula()
}

// And requires all sub-formulas to match.
type And struct{ Formulas []MatchFormula }

// Or requires at least one sub-formula to match.
type Or struct{ Formulas []MatchFormula }

// Not excludes matches of the sub-formula.
type Not struct{ Formula MatchFormula }

// Inside requires matches to occur within the scope of the sub-formula.
type Inside struct{ Formula MatchFormula }

// NotInside excludes matches within the scope of the sub-formula.
type NotInside struct{ Formula MatchFormula }

// MetavarConstraint applies a constraint to a bound metavariable.
// Op is one of: "~" (regex), "!~", "==", "!=", "<", ">", "<=", ">=".
type MetavarConstraint struct {
	Metavar string
	Op      string
	Value   string
}

// BasePattern is a leaf formula: a pattern in a specific language.
type BasePattern struct {
	Lang    string
	Pattern PatternNode
}

func (*And) matchFormula()              {}
func (*Or) matchFormula()               {}
func (*Not) matchFormula()              {}
func (*Inside) matchFormula()           {}
func (*NotInside) matchFormula()        {}
func (*MetavarConstraint) matchFormula() {}
func (*BasePattern) matchFormula()      {}
