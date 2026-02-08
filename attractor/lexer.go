// ABOUTME: Tokenizer/lexer for the DOT DSL that transforms source text into a token stream.
// ABOUTME: Handles identifiers, keywords, strings with escapes, numbers, booleans, comments, and DOT punctuation.
package attractor

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenType represents the type of a lexical token.
type TokenType int

const (
	TokenEOF        TokenType = iota
	TokenDigraph              // digraph keyword
	TokenSubgraph             // subgraph keyword
	TokenGraph                // graph keyword
	TokenNode                 // node keyword
	TokenEdge                 // edge keyword
	TokenLBrace               // {
	TokenRBrace               // }
	TokenLBracket             // [
	TokenRBracket             // ]
	TokenArrow                // ->
	TokenEquals               // =
	TokenComma                // ,
	TokenSemicolon            // ;
	TokenIdentifier           // bare identifier
	TokenString               // double-quoted string
	TokenNumber               // integer or float literal
	TokenBoolean              // true or false
	TokenMinus                // - (standalone, not part of -> or number)
)

// String returns a human-readable name for the token type.
func (t TokenType) String() string {
	switch t {
	case TokenEOF:
		return "EOF"
	case TokenDigraph:
		return "DIGRAPH"
	case TokenSubgraph:
		return "SUBGRAPH"
	case TokenGraph:
		return "GRAPH"
	case TokenNode:
		return "NODE"
	case TokenEdge:
		return "EDGE"
	case TokenLBrace:
		return "LBRACE"
	case TokenRBrace:
		return "RBRACE"
	case TokenLBracket:
		return "LBRACKET"
	case TokenRBracket:
		return "RBRACKET"
	case TokenArrow:
		return "ARROW"
	case TokenEquals:
		return "EQUALS"
	case TokenComma:
		return "COMMA"
	case TokenSemicolon:
		return "SEMICOLON"
	case TokenIdentifier:
		return "IDENTIFIER"
	case TokenString:
		return "STRING"
	case TokenNumber:
		return "NUMBER"
	case TokenBoolean:
		return "BOOLEAN"
	case TokenMinus:
		return "MINUS"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(t))
	}
}

// Token represents a single lexical token with its type, value, and source location.
type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int
}

// lexer holds the state of the lexical scanner.
type lexer struct {
	input  []rune
	pos    int
	line   int
	col    int
	tokens []Token
}

// Lex tokenizes the given DOT source string into a slice of tokens.
func Lex(input string) ([]Token, error) {
	l := &lexer{
		input:  []rune(input),
		pos:    0,
		line:   1,
		col:    1,
		tokens: make([]Token, 0),
	}

	if err := l.scan(); err != nil {
		return nil, err
	}

	return l.tokens, nil
}

// scan processes all characters in the input and produces tokens.
func (l *lexer) scan() error {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]

		// Skip whitespace
		if unicode.IsSpace(ch) {
			l.advance()
			continue
		}

		// Line comments: // ...
		if ch == '/' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '/' {
			l.skipLineComment()
			continue
		}

		// Block comments: /* ... */
		if ch == '/' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '*' {
			if err := l.skipBlockComment(); err != nil {
				return err
			}
			continue
		}

		// Strings
		if ch == '"' {
			if err := l.lexString(); err != nil {
				return err
			}
			continue
		}

		// Arrow: ->
		if ch == '-' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '>' {
			l.emit(TokenArrow, "->")
			l.advance()
			l.advance()
			continue
		}

		// Numbers: starts with digit, or minus followed by digit
		if ch == '-' && l.pos+1 < len(l.input) && (unicode.IsDigit(l.input[l.pos+1]) || l.input[l.pos+1] == '.') {
			l.lexNumber()
			continue
		}

		if unicode.IsDigit(ch) {
			l.lexNumber()
			continue
		}

		// Standalone minus (for undirected edge detection -- is two minuses)
		if ch == '-' {
			l.emit(TokenMinus, "-")
			l.advance()
			continue
		}

		// Identifiers and keywords
		if ch == '_' || unicode.IsLetter(ch) {
			l.lexIdentifier()
			continue
		}

		// Punctuation
		switch ch {
		case '{':
			l.emit(TokenLBrace, "{")
			l.advance()
		case '}':
			l.emit(TokenRBrace, "}")
			l.advance()
		case '[':
			l.emit(TokenLBracket, "[")
			l.advance()
		case ']':
			l.emit(TokenRBracket, "]")
			l.advance()
		case '=':
			l.emit(TokenEquals, "=")
			l.advance()
		case ',':
			l.emit(TokenComma, ",")
			l.advance()
		case ';':
			l.emit(TokenSemicolon, ";")
			l.advance()
		default:
			return fmt.Errorf("unexpected character %q at line %d, col %d", string(ch), l.line, l.col)
		}
	}

	l.tokens = append(l.tokens, Token{Type: TokenEOF, Value: "", Line: l.line, Col: l.col})
	return nil
}

// advance moves the position forward by one character, tracking line and column.
func (l *lexer) advance() {
	if l.pos < len(l.input) {
		if l.input[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}
}

// emit adds a token to the token list with the current position info.
func (l *lexer) emit(typ TokenType, value string) {
	l.tokens = append(l.tokens, Token{Type: typ, Value: value, Line: l.line, Col: l.col})
}

// skipLineComment skips from // to end of line.
func (l *lexer) skipLineComment() {
	// Skip the //
	l.advance()
	l.advance()
	for l.pos < len(l.input) && l.input[l.pos] != '\n' {
		l.advance()
	}
}

// skipBlockComment skips from /* to */ and returns an error for unterminated comments.
func (l *lexer) skipBlockComment() error {
	startLine := l.line
	// Skip the /*
	l.advance()
	l.advance()
	for l.pos < len(l.input) {
		if l.input[l.pos] == '*' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '/' {
			l.advance()
			l.advance()
			return nil
		}
		l.advance()
	}
	return fmt.Errorf("unterminated block comment starting at line %d", startLine)
}

// lexString reads a double-quoted string with escape sequences.
func (l *lexer) lexString() error {
	startLine := l.line
	startCol := l.col
	l.advance() // skip opening quote

	var sb strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]

		if ch == '\\' {
			l.advance()
			if l.pos >= len(l.input) {
				return fmt.Errorf("unterminated string starting at line %d, col %d", startLine, startCol)
			}
			escaped := l.input[l.pos]
			switch escaped {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			default:
				sb.WriteByte('\\')
				sb.WriteRune(escaped)
			}
			l.advance()
			continue
		}

		if ch == '"' {
			l.advance() // skip closing quote
			l.tokens = append(l.tokens, Token{Type: TokenString, Value: sb.String(), Line: startLine, Col: startCol})
			return nil
		}

		sb.WriteRune(ch)
		l.advance()
	}

	return fmt.Errorf("unterminated string starting at line %d, col %d", startLine, startCol)
}

// lexNumber reads an integer or float literal, with optional leading sign.
func (l *lexer) lexNumber() {
	startLine := l.line
	startCol := l.col
	var sb strings.Builder

	// Optional sign
	if l.pos < len(l.input) && l.input[l.pos] == '-' {
		sb.WriteByte('-')
		l.advance()
	}

	// Integer part
	for l.pos < len(l.input) && unicode.IsDigit(l.input[l.pos]) {
		sb.WriteRune(l.input[l.pos])
		l.advance()
	}

	// Decimal point and fractional part
	if l.pos < len(l.input) && l.input[l.pos] == '.' {
		sb.WriteByte('.')
		l.advance()
		for l.pos < len(l.input) && unicode.IsDigit(l.input[l.pos]) {
			sb.WriteRune(l.input[l.pos])
			l.advance()
		}
	}

	l.tokens = append(l.tokens, Token{Type: TokenNumber, Value: sb.String(), Line: startLine, Col: startCol})
}

// lexIdentifier reads an identifier or keyword.
func (l *lexer) lexIdentifier() {
	startLine := l.line
	startCol := l.col
	var sb strings.Builder

	for l.pos < len(l.input) && (l.input[l.pos] == '_' || unicode.IsLetter(l.input[l.pos]) || unicode.IsDigit(l.input[l.pos])) {
		sb.WriteRune(l.input[l.pos])
		l.advance()
	}

	word := sb.String()

	// Check for keywords
	var typ TokenType
	switch word {
	case "digraph":
		typ = TokenDigraph
	case "subgraph":
		typ = TokenSubgraph
	case "graph":
		typ = TokenGraph
	case "node":
		typ = TokenNode
	case "edge":
		typ = TokenEdge
	case "true", "false":
		typ = TokenBoolean
	default:
		typ = TokenIdentifier
	}

	l.tokens = append(l.tokens, Token{Type: typ, Value: word, Line: startLine, Col: startCol})
}
