package symbols

import "testing"

func TestGoExtraction(t *testing.T) {
	source := `package main

func hello() {
	world()
}

func world() {
}
`
	symbols, refs := Extract(source, "go")
	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d: %+v", len(symbols), symbols)
	}
	if symbols[0].Name != "hello" {
		t.Errorf("symbols[0].Name = %q", symbols[0].Name)
	}
	if symbols[0].Kind != "function" {
		t.Errorf("symbols[0].Kind = %q, want function", symbols[0].Kind)
	}
	if symbols[1].Name != "world" {
		t.Errorf("symbols[1].Name = %q", symbols[1].Name)
	}

	// world() call reference
	var worldRefs []Ref
	for _, r := range refs {
		if r.RefName == "world" {
			worldRefs = append(worldRefs, r)
		}
	}
	if len(worldRefs) == 0 {
		t.Error("missing world() call ref")
	}
	if len(worldRefs) > 0 && worldRefs[0].ContainingSymIdx < 0 {
		t.Error("world() call should be inside hello")
	}
}

func TestRustExtraction(t *testing.T) {
	source := `fn hello() {
    println!("hi");
}

fn world() {
    hello();
}
`
	symbols, refs := Extract(source, "rust")
	if len(symbols) < 2 {
		t.Fatalf("expected at least 2 symbols, got %d: %+v", len(symbols), symbols)
	}
	if symbols[0].Name != "hello" {
		t.Errorf("symbols[0].Name = %q", symbols[0].Name)
	}
	if symbols[0].Kind != "function" {
		t.Errorf("symbols[0].Kind = %q, want function", symbols[0].Kind)
	}

	var helloRefs []Ref
	for _, r := range refs {
		if r.RefName == "hello" {
			helloRefs = append(helloRefs, r)
		}
	}
	if len(helloRefs) == 0 {
		t.Error("missing hello() call ref")
	}
	// hello() should be called inside world()
	if len(helloRefs) > 0 {
		worldIdx := -1
		for i, s := range symbols {
			if s.Name == "world" {
				worldIdx = i
			}
		}
		if helloRefs[0].ContainingSymIdx != worldIdx {
			t.Errorf("hello() call containingSymIdx = %d, want %d (world)", helloRefs[0].ContainingSymIdx, worldIdx)
		}
	}

	// println! macro ref
	var printlnRefs []Ref
	for _, r := range refs {
		if r.RefName == "println" {
			printlnRefs = append(printlnRefs, r)
		}
	}
	if len(printlnRefs) == 0 {
		t.Error("missing println! macro ref")
	}
}

func TestRustStructAndImpl(t *testing.T) {
	source := `struct Foo {
    x: i32,
}

impl Foo {
    fn bar(&self) {
        baz();
    }
}
`
	symbols, refs := Extract(source, "rust")

	hasStruct := false
	hasImpl := false
	hasBar := false
	for _, s := range symbols {
		if s.Name == "Foo" && s.Kind == "struct" {
			hasStruct = true
		}
		if s.Name == "Foo" && s.Kind == "impl" {
			hasImpl = true
		}
		if s.Name == "bar" && s.Kind == "function" {
			hasBar = true
			// bar should be nested inside impl Foo
			if s.ParentIdx < 0 {
				t.Error("bar should have a parent (impl Foo)")
			} else if symbols[s.ParentIdx].Name != "Foo" || symbols[s.ParentIdx].Kind != "impl" {
				t.Errorf("bar's parent = %s/%s, want Foo/impl", symbols[s.ParentIdx].Name, symbols[s.ParentIdx].Kind)
			}
		}
	}
	if !hasStruct {
		t.Error("missing Foo struct")
	}
	if !hasImpl {
		t.Error("missing Foo impl")
	}
	if !hasBar {
		t.Error("missing bar function")
	}

	// baz() call ref, inside bar
	var bazRefs []Ref
	for _, r := range refs {
		if r.RefName == "baz" {
			bazRefs = append(bazRefs, r)
		}
	}
	if len(bazRefs) != 1 {
		t.Fatalf("expected 1 baz ref, got %d", len(bazRefs))
	}
	barIdx := -1
	for i, s := range symbols {
		if s.Name == "bar" {
			barIdx = i
		}
	}
	if bazRefs[0].ContainingSymIdx != barIdx {
		t.Errorf("baz() containingSymIdx = %d, want %d (bar)", bazRefs[0].ContainingSymIdx, barIdx)
	}
}

func TestPythonExtraction(t *testing.T) {
	source := `class MyClass:
    def method(self):
        other_func()

def standalone():
    pass
`
	symbols, refs := Extract(source, "python")
	if len(symbols) < 3 {
		t.Fatalf("expected at least 3 symbols, got %d: %+v", len(symbols), symbols)
	}

	hasClass := false
	hasMethod := false
	hasStandalone := false
	for _, s := range symbols {
		if s.Name == "MyClass" && s.Kind == "class" {
			hasClass = true
		}
		if s.Name == "method" && s.Kind == "function" {
			hasMethod = true
			// method should be nested in MyClass
			if s.ParentIdx < 0 {
				t.Error("method should be nested in MyClass")
			} else if symbols[s.ParentIdx].Name != "MyClass" {
				t.Errorf("method's parent = %s, want MyClass", symbols[s.ParentIdx].Name)
			}
		}
		if s.Name == "standalone" && s.Kind == "function" {
			hasStandalone = true
			if s.ParentIdx >= 0 {
				t.Error("standalone should have no parent")
			}
		}
	}
	if !hasClass {
		t.Error("missing MyClass")
	}
	if !hasMethod {
		t.Error("missing method")
	}
	if !hasStandalone {
		t.Error("missing standalone")
	}

	// other_func() reference
	var otherRefs []Ref
	for _, r := range refs {
		if r.RefName == "other_func" {
			otherRefs = append(otherRefs, r)
		}
	}
	if len(otherRefs) == 0 {
		t.Error("missing other_func() ref")
	}
}

func TestCExtraction(t *testing.T) {
	source := `struct Point {
    int x;
    int y;
};

int add(int a, int b) {
    return a + b;
}

int main() {
    add(1, 2);
    return 0;
}
`
	symbols, refs := Extract(source, "c")

	hasPoint := false
	hasAdd := false
	hasMain := false
	for _, s := range symbols {
		if s.Name == "Point" && s.Kind == "struct" {
			hasPoint = true
		}
		if s.Name == "add" && s.Kind == "function" {
			hasAdd = true
		}
		if s.Name == "main" && s.Kind == "function" {
			hasMain = true
		}
	}
	if !hasPoint {
		t.Error("missing Point struct")
	}
	if !hasAdd {
		t.Error("missing add function")
	}
	if !hasMain {
		t.Error("missing main function")
	}

	// add() call reference inside main
	var addRefs []Ref
	for _, r := range refs {
		if r.RefName == "add" {
			addRefs = append(addRefs, r)
		}
	}
	if len(addRefs) != 1 {
		t.Fatalf("expected 1 add ref, got %d", len(addRefs))
	}
	mainIdx := -1
	for i, s := range symbols {
		if s.Name == "main" {
			mainIdx = i
		}
	}
	if addRefs[0].ContainingSymIdx != mainIdx {
		t.Errorf("add() containingSymIdx = %d, want %d (main)", addRefs[0].ContainingSymIdx, mainIdx)
	}
}

func TestUnsupportedLanguage(t *testing.T) {
	symbols, refs := Extract("some code", "fortran")
	if symbols != nil || refs != nil {
		t.Error("expected nil for unsupported language")
	}
}

func TestSupportedLanguages(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) != 9 {
		t.Errorf("expected 9 supported languages, got %d", len(langs))
	}
}

func TestGoReturnType(t *testing.T) {
	source := `package main

func hello(x int, y string) string {
	return ""
}
`
	symbols, _ := Extract(source, "go")
	if len(symbols) == 0 {
		t.Fatal("no symbols")
	}
	if symbols[0].ReturnType != "string" {
		t.Errorf("return_type = %q, want string", symbols[0].ReturnType)
	}
	if symbols[0].Params != "x int, y string" {
		t.Errorf("params = %q, want %q", symbols[0].Params, "x int, y string")
	}
}

func TestRustTypeInfo(t *testing.T) {
	source := `fn hello(x: i32, y: &str) -> String {
    x.to_string()
}

struct Foo {
    bar: Vec<String>,
}

impl Foo {
    fn method(&self, n: usize) -> Option<i32> {
        None
    }
}
`
	symbols, _ := Extract(source, "rust")

	var hello, fooStruct, method *Symbol
	for i := range symbols {
		switch {
		case symbols[i].Name == "hello" && symbols[i].Kind == "function":
			hello = &symbols[i]
		case symbols[i].Name == "Foo" && symbols[i].Kind == "struct":
			fooStruct = &symbols[i]
		case symbols[i].Name == "method" && symbols[i].Kind == "function":
			method = &symbols[i]
		}
	}

	if hello == nil {
		t.Fatal("missing hello function")
	}
	if hello.ReturnType != "String" {
		t.Errorf("hello return_type = %q, want String", hello.ReturnType)
	}
	if hello.Params != "x: i32, y: &str" {
		t.Errorf("hello params = %q", hello.Params)
	}

	if fooStruct == nil {
		t.Fatal("missing Foo struct")
	}
	if fooStruct.ReturnType != "" {
		t.Errorf("struct should have no return_type, got %q", fooStruct.ReturnType)
	}

	if method == nil {
		t.Fatal("missing method")
	}
	if method.ReturnType != "Option<i32>" {
		t.Errorf("method return_type = %q, want Option<i32>", method.ReturnType)
	}
	if method.Params != "&self, n: usize" {
		t.Errorf("method params = %q", method.Params)
	}
}

func TestPythonTypeInfo(t *testing.T) {
	source := "def hello(x: int, y: str) -> str:\n    return \"\"\n"
	symbols, _ := Extract(source, "python")
	if len(symbols) == 0 {
		t.Fatal("no symbols")
	}
	if symbols[0].ReturnType != "str" {
		t.Errorf("return_type = %q, want str", symbols[0].ReturnType)
	}
	if symbols[0].Params != "x: int, y: str" {
		t.Errorf("params = %q", symbols[0].Params)
	}
}

func TestTypescriptTypeInfo(t *testing.T) {
	source := "function hello(x: number, y: string): string {\n    return \"\";\n}\n"
	symbols, _ := Extract(source, "typescript")
	if len(symbols) == 0 {
		t.Fatal("no symbols")
	}
	if symbols[0].ReturnType != "string" {
		t.Errorf("return_type = %q, want string", symbols[0].ReturnType)
	}
	if symbols[0].Params != "x: number, y: string" {
		t.Errorf("params = %q", symbols[0].Params)
	}
}

func TestJSNoTypes(t *testing.T) {
	source := "function hello(x, y) {\n    return x;\n}\n"
	symbols, _ := Extract(source, "javascript")
	if len(symbols) == 0 {
		t.Fatal("no symbols")
	}
	if symbols[0].ReturnType != "" {
		t.Errorf("JS should have no return_type, got %q", symbols[0].ReturnType)
	}
	if symbols[0].Params != "x, y" {
		t.Errorf("params = %q, want %q", symbols[0].Params, "x, y")
	}
}

func TestCTypeInfo(t *testing.T) {
	source := `int add(int a, int b) {
    return a + b;
}
`
	symbols, _ := Extract(source, "c")
	if len(symbols) == 0 {
		t.Fatal("no symbols")
	}
	add := symbols[0]
	if add.ReturnType != "int" {
		t.Errorf("return_type = %q, want int", add.ReturnType)
	}
	if add.Params != "int a, int b" {
		t.Errorf("params = %q, want %q", add.Params, "int a, int b")
	}
}

func TestSignatureExtraction(t *testing.T) {
	source := `fn hello(x: i32) -> String {
    x.to_string()
}
`
	symbols, _ := Extract(source, "rust")
	if len(symbols) == 0 {
		t.Fatal("no symbols")
	}
	if symbols[0].Signature != "fn hello(x: i32) -> String" {
		t.Errorf("signature = %q", symbols[0].Signature)
	}
}

func TestPythonMethodCallAndNesting(t *testing.T) {
	source := `class Animal:
    def speak(self):
        self.make_sound()

    def make_sound(self):
        print("...")

class Dog(Animal):
    def make_sound(self):
        print("woof")

def train(animal):
    animal.speak()
`
	symbols, refs := Extract(source, "python")

	// Verify all symbols exist with correct kinds
	want := map[string]string{
		"Animal":     "class",
		"speak":      "function",
		"make_sound": "function",
		"Dog":        "class",
		"train":      "function",
	}
	found := map[string]bool{}
	for _, s := range symbols {
		if expectedKind, ok := want[s.Name]; ok {
			if s.Kind != expectedKind {
				t.Errorf("%s kind = %q, want %q", s.Name, s.Kind, expectedKind)
			}
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol %q", name)
		}
	}

	// speak and make_sound should be nested inside their class
	for _, s := range symbols {
		if s.Name == "speak" {
			if s.ParentIdx < 0 || symbols[s.ParentIdx].Name != "Animal" {
				t.Errorf("speak should be nested in Animal")
			}
		}
		if s.Name == "train" && s.ParentIdx >= 0 {
			t.Error("train should be top-level (no parent)")
		}
	}

	// make_sound should appear twice (Animal and Dog)
	makeSoundCount := 0
	for _, s := range symbols {
		if s.Name == "make_sound" {
			makeSoundCount++
		}
	}
	if makeSoundCount != 2 {
		t.Errorf("expected 2 make_sound symbols, got %d", makeSoundCount)
	}

	// print() should be referenced
	var printRefs []Ref
	for _, r := range refs {
		if r.RefName == "print" {
			printRefs = append(printRefs, r)
		}
	}
	if len(printRefs) < 2 {
		t.Errorf("expected at least 2 print refs, got %d", len(printRefs))
	}
}

func TestGoMethodsAndTypes(t *testing.T) {
	source := `package main

type Server struct {
	port int
}

func NewServer(port int) *Server {
	return &Server{port: port}
}

func (s *Server) Start() error {
	return s.listen()
}

func (s *Server) listen() error {
	return nil
}

type Handler interface{}
`
	symbols, refs := Extract(source, "go")

	// Check all symbols
	want := map[string]string{
		"Server":    "type",
		"NewServer": "function",
		"Start":     "method",
		"listen":    "method",
		"Handler":   "type",
	}
	found := map[string]bool{}
	for _, s := range symbols {
		if expectedKind, ok := want[s.Name]; ok {
			if s.Kind != expectedKind {
				t.Errorf("%s kind = %q, want %q", s.Name, s.Kind, expectedKind)
			}
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol %q", name)
		}
	}

	// NewServer should have return type *Server and params
	for _, s := range symbols {
		if s.Name == "NewServer" {
			if s.Params != "port int" {
				t.Errorf("NewServer params = %q, want %q", s.Params, "port int")
			}
			if s.ReturnType != "*Server" {
				t.Errorf("NewServer return_type = %q, want %q", s.ReturnType, "*Server")
			}
		}
		if s.Name == "Start" {
			if s.ReturnType != "error" {
				t.Errorf("Start return_type = %q, want %q", s.ReturnType, "error")
			}
		}
	}

	// s.listen() should be a ref inside Start
	var listenRefs []Ref
	for _, r := range refs {
		if r.RefName == "listen" {
			listenRefs = append(listenRefs, r)
		}
	}
	if len(listenRefs) != 1 {
		t.Fatalf("expected 1 listen ref, got %d", len(listenRefs))
	}
	startIdx := -1
	for i, s := range symbols {
		if s.Name == "Start" {
			startIdx = i
		}
	}
	if listenRefs[0].ContainingSymIdx != startIdx {
		t.Errorf("listen() containingSymIdx = %d, want %d (Start)", listenRefs[0].ContainingSymIdx, startIdx)
	}
}

func TestRustEnumAndTraits(t *testing.T) {
	source := `enum Color {
    Red,
    Green,
    Blue,
}

trait Drawable {
    fn draw(&self);
}

struct Circle {
    radius: f64,
}

impl Drawable for Circle {
    fn draw(&self) {
        render(self.radius);
    }
}

fn render(r: f64) {
    println!("drawing circle with radius {}", r);
}
`
	symbols, refs := Extract(source, "rust")

	type wantSym struct{ name, kind string }
	wantList := []wantSym{
		{"Color", "enum"},
		{"Drawable", "trait"},
		{"Circle", "struct"},
		{"Circle", "impl"},
		{"draw", "function"},
		{"render", "function"},
	}
	for _, w := range wantList {
		found := false
		for _, s := range symbols {
			if s.Name == w.name && s.Kind == w.kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing symbol %s/%s", w.name, w.kind)
		}
	}

	// draw should be nested in an impl
	for _, s := range symbols {
		if s.Name == "draw" {
			if s.ParentIdx < 0 {
				t.Error("draw should be nested inside impl")
			}
		}
	}

	// render() call inside draw
	var renderRefs []Ref
	for _, r := range refs {
		if r.RefName == "render" {
			renderRefs = append(renderRefs, r)
		}
	}
	if len(renderRefs) != 1 {
		t.Fatalf("expected 1 render ref, got %d", len(renderRefs))
	}
	drawIdx := -1
	for i, s := range symbols {
		if s.Name == "draw" {
			drawIdx = i
		}
	}
	if renderRefs[0].ContainingSymIdx != drawIdx {
		t.Errorf("render() containingSymIdx = %d, want %d (draw)", renderRefs[0].ContainingSymIdx, drawIdx)
	}

	// println! macro ref
	var printlnRefs []Ref
	for _, r := range refs {
		if r.RefName == "println" {
			printlnRefs = append(printlnRefs, r)
		}
	}
	if len(printlnRefs) != 1 {
		t.Errorf("expected 1 println! ref, got %d", len(printlnRefs))
	}
}

func TestTypescriptClassesAndInterfaces(t *testing.T) {
	source := `interface Logger {
    log(message: string): void;
}

class ConsoleLogger implements Logger {}

enum Level {
    Debug,
    Info,
    Error,
}

type Config = {
    level: Level;
    logger: Logger;
};

function createLogger(config: Config): Logger {
    return new ConsoleLogger();
}

function useLogger(logger: Logger): void {
    createLogger(logger);
}
`
	symbols, refs := Extract(source, "typescript")

	want := map[string]string{
		"Logger":        "interface",
		"ConsoleLogger": "class",
		"Level":         "enum",
		"Config":        "type_alias",
		"createLogger":  "function",
		"useLogger":     "function",
	}
	found := map[string]bool{}
	for _, s := range symbols {
		if expectedKind, ok := want[s.Name]; ok {
			if s.Kind != expectedKind {
				t.Errorf("%s kind = %q, want %q", s.Name, s.Kind, expectedKind)
			}
			found[s.Name] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("missing symbol %q", name)
		}
	}

	// createLogger should have return type and params
	for _, s := range symbols {
		if s.Name == "createLogger" {
			if s.ReturnType != "Logger" {
				t.Errorf("createLogger return_type = %q, want Logger", s.ReturnType)
			}
			if s.Params != "config: Config" {
				t.Errorf("createLogger params = %q, want %q", s.Params, "config: Config")
			}
		}
	}

	// createLogger() call ref inside useLogger
	var createRefs []Ref
	for _, r := range refs {
		if r.RefName == "createLogger" {
			createRefs = append(createRefs, r)
		}
	}
	if len(createRefs) != 1 {
		t.Fatalf("expected 1 createLogger ref, got %d", len(createRefs))
	}
	useLoggerIdx := -1
	for i, s := range symbols {
		if s.Name == "useLogger" {
			useLoggerIdx = i
		}
	}
	if createRefs[0].ContainingSymIdx != useLoggerIdx {
		t.Errorf("createLogger() containingSymIdx = %d, want %d (useLogger)", createRefs[0].ContainingSymIdx, useLoggerIdx)
	}
}
