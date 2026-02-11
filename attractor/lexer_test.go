// ABOUTME: Tests for the DOT DSL lexer/tokenizer.
// ABOUTME: Covers identifiers, keywords, strings, numbers, punctuation, comments, and full digraph tokenization.
package attractor

import (
	"testing"
)

func TestLexIdentifiers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"_private", "_private"},
		{"node123", "node123"},
		{"A_B_C", "A_B_C"},
		{"_", "_"},
		{"x", "x"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
			}
			// Should produce at least one token plus EOF
			if len(tokens) < 2 {
				t.Fatalf("Lex(%q) produced %d tokens, want at least 2", tt.input, len(tokens))
			}
			if tokens[0].Type != TokenIdentifier {
				t.Errorf("Lex(%q)[0].Type = %v, want TokenIdentifier", tt.input, tokens[0].Type)
			}
			if tokens[0].Value != tt.want {
				t.Errorf("Lex(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.want)
			}
			if tokens[len(tokens)-1].Type != TokenEOF {
				t.Errorf("last token should be EOF")
			}
		})
	}
}

func TestLexKeywords(t *testing.T) {
	tests := []struct {
		input    string
		wantType TokenType
	}{
		{"digraph", TokenDigraph},
		{"subgraph", TokenSubgraph},
		{"graph", TokenGraph},
		{"node", TokenNode},
		{"edge", TokenEdge},
		{"true", TokenBoolean},
		{"false", TokenBoolean},
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

func TestLexStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple string", `"hello"`, "hello"},
		{"string with spaces", `"hello world"`, "hello world"},
		{"escaped quote", `"say \"hi\""`, `say "hi"`},
		{"escaped backslash", `"path\\to"`, `path\to`},
		{"escaped newline", `"line1\nline2"`, "line1\nline2"},
		{"escaped tab", `"col1\tcol2"`, "col1\tcol2"},
		{"empty string", `""`, ""},
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
		name     string
		input    string
		wantType TokenType
		want     string
	}{
		{"integer", "42", TokenNumber, "42"},
		{"negative integer", "-1", TokenNumber, "-1"},
		{"zero", "0", TokenNumber, "0"},
		{"float", "3.14", TokenNumber, "3.14"},
		{"negative float", "-0.5", TokenNumber, "-0.5"},
		{"leading dot float", "0.123", TokenNumber, "0.123"},
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
			if tokens[0].Value != tt.want {
				t.Errorf("Lex(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.want)
			}
		})
	}
}

func TestLexPunctuation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType TokenType
		want     string
	}{
		{"left brace", "{", TokenLBrace, "{"},
		{"right brace", "}", TokenRBrace, "}"},
		{"left bracket", "[", TokenLBracket, "["},
		{"right bracket", "]", TokenRBracket, "]"},
		{"arrow", "->", TokenArrow, "->"},
		{"equals", "=", TokenEquals, "="},
		{"comma", ",", TokenComma, ","},
		{"semicolon", ";", TokenSemicolon, ";"},
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
			if tokens[0].Value != tt.want {
				t.Errorf("Lex(%q)[0].Value = %q, want %q", tt.input, tokens[0].Value, tt.want)
			}
		})
	}
}

func TestLexComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int // expected non-EOF tokens
		wantType TokenType
		wantVal  string
	}{
		{
			name:     "line comment strips content",
			input:    "hello // this is a comment",
			wantLen:  1,
			wantType: TokenIdentifier,
			wantVal:  "hello",
		},
		{
			name:     "block comment strips content",
			input:    "hello /* block comment */ world",
			wantLen:  2,
			wantType: TokenIdentifier,
			wantVal:  "hello",
		},
		{
			name:    "only comment",
			input:   "// just a comment",
			wantLen: 0,
		},
		{
			name:    "only block comment",
			input:   "/* block comment */",
			wantLen: 0,
		},
		{
			name:     "multiline block comment",
			input:    "before /* line1\nline2\nline3 */ after",
			wantLen:  2,
			wantType: TokenIdentifier,
			wantVal:  "before",
		},
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
			if nonEOF != tt.wantLen {
				t.Errorf("Lex(%q) produced %d non-EOF tokens, want %d", tt.input, nonEOF, tt.wantLen)
				for i, tok := range tokens {
					t.Logf("  token[%d]: %v %q", i, tok.Type, tok.Value)
				}
			}
			if tt.wantLen > 0 {
				if tokens[0].Type != tt.wantType {
					t.Errorf("first token type = %v, want %v", tokens[0].Type, tt.wantType)
				}
				if tokens[0].Value != tt.wantVal {
					t.Errorf("first token value = %q, want %q", tokens[0].Value, tt.wantVal)
				}
			}
		})
	}
}

func TestLexFullDigraph(t *testing.T) {
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

	// Verify expected sequence of token types
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
		for i, tok := range tokens {
			t.Logf("  token[%d]: %v %q", i, tok.Type, tok.Value)
		}
	}

	for i, exp := range expected {
		if i >= len(tokens) {
			break
		}
		if tokens[i].Type != exp.typ {
			t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp.typ)
		}
		if tokens[i].Value != exp.val {
			t.Errorf("token[%d].Value = %q, want %q", i, tokens[i].Value, exp.val)
		}
	}
}

func TestLexDuration(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"seconds", `"900s"`, "900s"},
		{"minutes", `"15m"`, "15m"},
		{"hours", `"2h"`, "2h"},
		{"milliseconds", `"250ms"`, "250ms"},
		{"days", `"1d"`, "1d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("Lex(%q) error: %v", tt.input, err)
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

func TestLexUnterminatedString(t *testing.T) {
	_, err := Lex(`"unterminated`)
	if err == nil {
		t.Error("Lex of unterminated string should return error")
	}
}

func TestLexUnterminatedBlockComment(t *testing.T) {
	_, err := Lex(`/* unterminated block comment`)
	if err == nil {
		t.Error("Lex of unterminated block comment should return error")
	}
}

func TestLexUndirectedEdge(t *testing.T) {
	// "--" should produce two separate minus/unknown tokens or be handled in parser
	// The lexer should not crash on this input
	tokens, err := Lex("A -- B")
	if err != nil {
		t.Fatalf("Lex(\"A -- B\") error: %v", err)
	}
	// Should produce tokens without crashing
	if len(tokens) < 2 {
		t.Errorf("expected at least 2 tokens, got %d", len(tokens))
	}
}

func TestLexLineNumbers(t *testing.T) {
	input := "digraph\n{\n}"
	tokens, err := Lex(input)
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	// digraph should be on line 1
	if tokens[0].Line != 1 {
		t.Errorf("token 'digraph' line = %d, want 1", tokens[0].Line)
	}
	// { should be on line 2
	if tokens[1].Line != 2 {
		t.Errorf("token '{' line = %d, want 2", tokens[1].Line)
	}
	// } should be on line 3
	if tokens[2].Line != 3 {
		t.Errorf("token '}' line = %d, want 3", tokens[2].Line)
	}
}
