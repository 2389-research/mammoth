// ABOUTME: Tests for the DOT DSL lexer/tokenizer in the consolidated dot package.
// ABOUTME: Covers empty input, single tokens, keywords, strings, numbers, comments, arrows, errors, and full digraph tokenization.
package dot

import (
	"strings"
	"testing"
)

func TestLexEmptyInput(t *testing.T) {
	tokens, err := Lex("")
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", "", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("Lex(%q) produced %d tokens, want 1 (just EOF)", "", len(tokens))
	}
	if tokens[0].Type != TokenEOF {
		t.Errorf("Lex(%q)[0].Type = %v, want TokenEOF", "", tokens[0].Type)
	}
}

func TestLexSingleTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType TokenType
		wantVal  string
	}{
		{"left brace", "{", TokenLBrace, "{"},
		{"right brace", "}", TokenRBrace, "}"},
		{"left bracket", "[", TokenLBracket, "["},
		{"right bracket", "]", TokenRBracket, "]"},
		{"arrow", "->", TokenArrow, "->"},
		{"equals", "=", TokenEquals, "="},
		{"comma", ",", TokenComma, ","},
		{"semicolon", ";", TokenSemicolon, ";"},
		{"minus", "- ", TokenMinus, "-"}, // trailing space to avoid ambiguity
		{"identifier", "foo", TokenIdentifier, "foo"},
		{"string", `"hello"`, TokenString, "hello"},
		{"number int", "42", TokenNumber, "42"},
		{"number float", "3.14", TokenNumber, "3.14"},
		{"boolean true", "true", TokenBoolean, "true"},
		{"boolean false", "false", TokenBoolean, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			if len(tokens) < 2 {
				t.Fatalf("Lex(%q) produced %d tokens, want at least 2", tt.input, len(tokens))
			}
			if tokens[0].Type != tt.wantType {
				t.Errorf("Lex(%q)[0].Type = %v, want %v", tt.input, tokens[0].Type, tt.wantType)
			}
			if tokens[0].Value != tt.wantVal {
				t.Errorf("Lex(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.wantVal)
			}
			if tokens[len(tokens)-1].Type != TokenEOF {
				t.Errorf("last token should be EOF, got %v", tokens[len(tokens)-1].Type)
			}
		})
	}
}

func TestLexKeywordsVsIdentifiers(t *testing.T) {
	tests := []struct {
		input    string
		wantType TokenType
	}{
		// Keywords
		{"digraph", TokenDigraph},
		{"subgraph", TokenSubgraph},
		{"graph", TokenGraph},
		{"node", TokenNode},
		{"edge", TokenEdge},
		{"true", TokenBoolean},
		{"false", TokenBoolean},
		// Identifiers that look like keywords but aren't
		{"digraphs", TokenIdentifier},
		{"subgraphed", TokenIdentifier},
		{"graphs", TokenIdentifier},
		{"noder", TokenIdentifier},
		{"edges", TokenIdentifier},
		{"truefalse", TokenIdentifier},
		{"_private", TokenIdentifier},
		{"node123", TokenIdentifier},
		{"A_B_C", TokenIdentifier},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			if len(tokens) < 2 {
				t.Fatalf("Lex(%q) produced %d tokens, want at least 2", tt.input, len(tokens))
			}
			if tokens[0].Type != tt.wantType {
				t.Errorf("Lex(%q)[0].Type = %v, want %v", tt.input, tokens[0].Type, tt.wantType)
			}
			if tokens[0].Value != tt.input {
				t.Errorf("Lex(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.input)
			}
		})
	}
}

func TestLexQuotedStringsWithEscapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", `"hello"`, "hello"},
		{"spaces", `"hello world"`, "hello world"},
		{"escaped quote", `"say \"hi\""`, `say "hi"`},
		{"escaped backslash", `"path\\to"`, `path\to`},
		{"escaped newline", `"line1\nline2"`, "line1\nline2"},
		{"escaped tab", `"col1\tcol2"`, "col1\tcol2"},
		{"empty string", `""`, ""},
		{"unknown escape passthrough", `"test\xvalue"`, `test\xvalue`},
		{"duration string", `"900s"`, "900s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			if len(tokens) < 2 {
				t.Fatalf("Lex(%q) produced %d tokens, want at least 2", tt.input, len(tokens))
			}
			if tokens[0].Type != TokenString {
				t.Errorf("Lex(%q)[0].Type = %v, want TokenString", tt.input, tokens[0].Type)
			}
			if tokens[0].Value != tt.want {
				t.Errorf("Lex(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.want)
			}
		})
	}
}

func TestLexNumbers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"integer", "42", "42"},
		{"zero", "0", "0"},
		{"negative integer", "-1", "-1"},
		{"float", "3.14", "3.14"},
		{"negative float", "-0.5", "-0.5"},
		{"large number", "9999", "9999"},
		{"decimal only", "0.123", "0.123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			if len(tokens) < 2 {
				t.Fatalf("Lex(%q) produced %d tokens, want at least 2", tt.input, len(tokens))
			}
			if tokens[0].Type != TokenNumber {
				t.Errorf("Lex(%q)[0].Type = %v, want TokenNumber", tt.input, tokens[0].Type)
			}
			if tokens[0].Value != tt.want {
				t.Errorf("Lex(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.want)
			}
		})
	}
}

func TestLexComments(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantNonEOF  int
		wantFirstTy TokenType
		wantFirstV  string
	}{
		{"line comment after token", "hello // this is a comment", 1, TokenIdentifier, "hello"},
		{"block comment between tokens", "hello /* block */ world", 2, TokenIdentifier, "hello"},
		{"only line comment", "// just a comment", 0, TokenEOF, ""},
		{"only block comment", "/* block comment */", 0, TokenEOF, ""},
		{"multiline block comment", "before /* line1\nline2\nline3 */ after", 2, TokenIdentifier, "before"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			nonEOF := 0
			for _, tok := range tokens {
				if tok.Type != TokenEOF {
					nonEOF++
				}
			}
			if nonEOF != tt.wantNonEOF {
				t.Errorf("Lex(%q) produced %d non-EOF tokens, want %d", tt.input, nonEOF, tt.wantNonEOF)
				for i, tok := range tokens {
					t.Logf("  token[%d]: %v %q", i, tok.Type, tok.Value)
				}
			}
			if tt.wantNonEOF > 0 {
				if tokens[0].Type != tt.wantFirstTy {
					t.Errorf("first token type = %v, want %v", tokens[0].Type, tt.wantFirstTy)
				}
				if tokens[0].Value != tt.wantFirstV {
					t.Errorf("first token value = %q, want %q", tokens[0].Value, tt.wantFirstV)
				}
			}
		})
	}
}

func TestLexArrowVsMinus(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantTypes  []TokenType
		wantValues []string
	}{
		{
			name:       "arrow between identifiers",
			input:      "A -> B",
			wantTypes:  []TokenType{TokenIdentifier, TokenArrow, TokenIdentifier, TokenEOF},
			wantValues: []string{"A", "->", "B", ""},
		},
		{
			name:       "undirected edge (double minus)",
			input:      "A -- B",
			wantTypes:  []TokenType{TokenIdentifier, TokenMinus, TokenMinus, TokenIdentifier, TokenEOF},
			wantValues: []string{"A", "-", "-", "B", ""},
		},
		{
			name:       "standalone minus",
			input:      "A - B",
			wantTypes:  []TokenType{TokenIdentifier, TokenMinus, TokenIdentifier, TokenEOF},
			wantValues: []string{"A", "-", "B", ""},
		},
		{
			name:       "negative number not standalone minus",
			input:      "-42",
			wantTypes:  []TokenType{TokenNumber, TokenEOF},
			wantValues: []string{"-42", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			if len(tokens) != len(tt.wantTypes) {
				t.Fatalf("Lex(%q) produced %d tokens, want %d", tt.input, len(tokens), len(tt.wantTypes))
			}
			for i, wt := range tt.wantTypes {
				if tokens[i].Type != wt {
					t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, wt)
				}
				if tokens[i].Value != tt.wantValues[i] {
					t.Errorf("token[%d].Value = %q, want %q", i, tokens[i].Value, tt.wantValues[i])
				}
			}
		})
	}
}

func TestLexErrorUnterminatedString(t *testing.T) {
	_, err := Lex(`"unterminated`)
	if err == nil {
		t.Fatal("Lex of unterminated string should return error")
	}
	if !strings.Contains(err.Error(), "unterminated string") {
		t.Errorf("error message %q should contain 'unterminated string'", err.Error())
	}
}

func TestLexErrorUnterminatedBlockComment(t *testing.T) {
	_, err := Lex(`/* unterminated block comment`)
	if err == nil {
		t.Fatal("Lex of unterminated block comment should return error")
	}
	if !strings.Contains(err.Error(), "unterminated block comment") {
		t.Errorf("error message %q should contain 'unterminated block comment'", err.Error())
	}
}

func TestLexErrorUnexpectedCharacter(t *testing.T) {
	_, err := Lex(`@`)
	if err == nil {
		t.Fatal("Lex of unexpected character should return error")
	}
	if !strings.Contains(err.Error(), "unexpected character") {
		t.Errorf("error message %q should contain 'unexpected character'", err.Error())
	}
	// Should include line and column info
	if !strings.Contains(err.Error(), "line") || !strings.Contains(err.Error(), "col") {
		t.Errorf("error message %q should contain line/col info", err.Error())
	}
}

func TestLexLineColumnTracking(t *testing.T) {
	input := "digraph\n{\n}"
	tokens, err := Lex(input)
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	// digraph on line 1, col 1
	if tokens[0].Line != 1 || tokens[0].Col != 1 {
		t.Errorf("token 'digraph' at (%d,%d), want (1,1)", tokens[0].Line, tokens[0].Col)
	}
	// { on line 2, col 1
	if tokens[1].Line != 2 || tokens[1].Col != 1 {
		t.Errorf("token '{' at (%d,%d), want (2,1)", tokens[1].Line, tokens[1].Col)
	}
	// } on line 3, col 1
	if tokens[2].Line != 3 || tokens[2].Col != 1 {
		t.Errorf("token '}' at (%d,%d), want (3,1)", tokens[2].Line, tokens[2].Col)
	}
}

func TestLexLineColumnInlineTracking(t *testing.T) {
	input := "A -> B"
	tokens, err := Lex(input)
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	// A at col 1
	if tokens[0].Col != 1 {
		t.Errorf("token 'A' col = %d, want 1", tokens[0].Col)
	}
	// -> at col 3
	if tokens[1].Col != 3 {
		t.Errorf("token '->' col = %d, want 3", tokens[1].Col)
	}
	// B at col 6
	if tokens[2].Col != 6 {
		t.Errorf("token 'B' col = %d, want 6", tokens[2].Col)
	}
}

func TestLexFullDigraphSnippet(t *testing.T) {
	input := `digraph Simple {
    graph [goal="Run tests"]
    rankdir=LR
    start [shape=Mdiamond, label="Start"]
    start -> run_tests
}`

	tokens, err := Lex(input)
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}

	expected := []struct {
		typ TokenType
		val string
	}{
		{TokenDigraph, "digraph"},
		{TokenIdentifier, "Simple"},
		{TokenLBrace, "{"},
		{TokenGraph, "graph"},
		{TokenLBracket, "["},
		{TokenIdentifier, "goal"},
		{TokenEquals, "="},
		{TokenString, "Run tests"},
		{TokenRBracket, "]"},
		{TokenIdentifier, "rankdir"},
		{TokenEquals, "="},
		{TokenIdentifier, "LR"},
		{TokenIdentifier, "start"},
		{TokenLBracket, "["},
		{TokenIdentifier, "shape"},
		{TokenEquals, "="},
		{TokenIdentifier, "Mdiamond"},
		{TokenComma, ","},
		{TokenIdentifier, "label"},
		{TokenEquals, "="},
		{TokenString, "Start"},
		{TokenRBracket, "]"},
		{TokenIdentifier, "start"},
		{TokenArrow, "->"},
		{TokenIdentifier, "run_tests"},
		{TokenRBrace, "}"},
		{TokenEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ {
			t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp.typ)
		}
		if tokens[i].Value != exp.val {
			t.Errorf("token[%d].Value = %q, want %q", i, tokens[i].Value, exp.val)
		}
	}
}

func TestLexTokenTypeString(t *testing.T) {
	tests := []struct {
		typ  TokenType
		want string
	}{
		{TokenEOF, "EOF"},
		{TokenDigraph, "DIGRAPH"},
		{TokenSubgraph, "SUBGRAPH"},
		{TokenGraph, "GRAPH"},
		{TokenNode, "NODE"},
		{TokenEdge, "EDGE"},
		{TokenLBrace, "LBRACE"},
		{TokenRBrace, "RBRACE"},
		{TokenLBracket, "LBRACKET"},
		{TokenRBracket, "RBRACKET"},
		{TokenArrow, "ARROW"},
		{TokenEquals, "EQUALS"},
		{TokenComma, "COMMA"},
		{TokenSemicolon, "SEMICOLON"},
		{TokenIdentifier, "IDENTIFIER"},
		{TokenString, "STRING"},
		{TokenNumber, "NUMBER"},
		{TokenBoolean, "BOOLEAN"},
		{TokenMinus, "MINUS"},
		{TokenType(999), "UNKNOWN(999)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.typ.String()
			if got != tt.want {
				t.Errorf("TokenType(%d).String() = %q, want %q", int(tt.typ), got, tt.want)
			}
		})
	}
}

func TestLexWhitespaceOnly(t *testing.T) {
	tokens, err := Lex("   \t\n\n  ")
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("Lex of whitespace-only produced %d tokens, want 1 (just EOF)", len(tokens))
	}
	if tokens[0].Type != TokenEOF {
		t.Errorf("expected EOF, got %v", tokens[0].Type)
	}
}
