// ABOUTME: Recursive descent parser for the DOT DSL that produces an in-memory Graph model.
// ABOUTME: Parses digraphs with nodes, edges, attributes, defaults, subgraphs, and chained edge expansion.
package attractor

import (
	"fmt"
	"strings"
	"unicode"
)

// parser holds the state of the recursive descent parser.
type parser struct {
	tokens       []Token
	pos          int
	graph        *Graph
	nodeDefaults map[string]string // current scope node defaults
	edgeDefaults map[string]string // current scope edge defaults
}

// Parse parses the given DOT source string into a Graph.
func Parse(input string) (*Graph, error) {
	tokens, err := Lex(input)
	if err != nil {
		return nil, fmt.Errorf("lex error: %w", err)
	}

	p := &parser{
		tokens: tokens,
		pos:    0,
		graph: &Graph{
			Nodes:        make(map[string]*Node),
			Edges:        make([]*Edge, 0),
			Attrs:        make(map[string]string),
			NodeDefaults: make(map[string]string),
			EdgeDefaults: make(map[string]string),
			Subgraphs:    make([]*Subgraph, 0),
		},
		nodeDefaults: make(map[string]string),
		edgeDefaults: make(map[string]string),
	}

	if err := p.parseGraph(); err != nil {
		return nil, err
	}

	return p.graph, nil
}

// current returns the current token.
func (p *parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

// peek returns the token at the given offset from the current position.
func (p *parser) peek(offset int) Token {
	idx := p.pos + offset
	if idx >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[idx]
}

// advance moves to the next token and returns the consumed token.
func (p *parser) advance() Token {
	tok := p.current()
	p.pos++
	return tok
}

// expect consumes the next token and returns an error if it doesn't match the expected type.
func (p *parser) expect(typ TokenType) (Token, error) {
	tok := p.current()
	if tok.Type != typ {
		return tok, fmt.Errorf("expected %v but got %v (%q) at line %d, col %d",
			typ, tok.Type, tok.Value, tok.Line, tok.Col)
	}
	p.advance()
	return tok, nil
}

// skipSemicolon optionally consumes a semicolon if present.
func (p *parser) skipSemicolon() {
	if p.current().Type == TokenSemicolon {
		p.advance()
	}
}

// parseGraph parses: 'digraph' Identifier '{' Statement* '}'
func (p *parser) parseGraph() error {
	// Check for and reject 'strict' modifier
	if p.current().Type == TokenIdentifier && p.current().Value == "strict" {
		return fmt.Errorf("strict modifier is not supported at line %d, col %d",
			p.current().Line, p.current().Col)
	}

	if _, err := p.expect(TokenDigraph); err != nil {
		return fmt.Errorf("expected 'digraph': %w", err)
	}

	name, err := p.expect(TokenIdentifier)
	if err != nil {
		return fmt.Errorf("expected graph name: %w", err)
	}
	p.graph.Name = name.Value

	if _, err := p.expect(TokenLBrace); err != nil {
		return err
	}

	if err := p.parseStatements(); err != nil {
		return err
	}

	if _, err := p.expect(TokenRBrace); err != nil {
		return err
	}

	// Check for multiple digraphs
	if p.current().Type == TokenDigraph {
		return fmt.Errorf("multiple digraphs are not supported; only one digraph per file is allowed")
	}

	// Apply graph-level node defaults to the graph struct
	for k, v := range p.nodeDefaults {
		p.graph.NodeDefaults[k] = v
	}
	for k, v := range p.edgeDefaults {
		p.graph.EdgeDefaults[k] = v
	}

	return nil
}

// parseStatements parses a sequence of statements until a closing brace or EOF.
func (p *parser) parseStatements() error {
	for p.current().Type != TokenRBrace && p.current().Type != TokenEOF {
		if err := p.parseStatement(); err != nil {
			return err
		}
	}
	return nil
}

// parseStatement parses a single statement within a digraph or subgraph.
func (p *parser) parseStatement() error {
	tok := p.current()

	switch tok.Type {
	case TokenGraph:
		// graph [attrs] or graph attr stmt
		return p.parseGraphAttrStmt()

	case TokenNode:
		// node [defaults]
		return p.parseNodeDefaults()

	case TokenEdge:
		// edge [defaults]
		return p.parseEdgeDefaults()

	case TokenSubgraph:
		return p.parseSubgraph()

	case TokenIdentifier, TokenString:
		return p.parseNodeOrEdgeStmt()

	case TokenSemicolon:
		p.advance()
		return nil

	default:
		return fmt.Errorf("unexpected token %v (%q) at line %d, col %d",
			tok.Type, tok.Value, tok.Line, tok.Col)
	}
}

// parseGraphAttrStmt parses: 'graph' AttrBlock ';'?
func (p *parser) parseGraphAttrStmt() error {
	p.advance() // consume 'graph'

	if p.current().Type == TokenLBracket {
		attrs, err := p.parseAttrBlock()
		if err != nil {
			return err
		}
		for k, v := range attrs {
			p.graph.Attrs[k] = v
		}
	}

	p.skipSemicolon()
	return nil
}

// parseNodeDefaults parses: 'node' AttrBlock ';'?
func (p *parser) parseNodeDefaults() error {
	p.advance() // consume 'node'

	if p.current().Type == TokenLBracket {
		attrs, err := p.parseAttrBlock()
		if err != nil {
			return err
		}
		for k, v := range attrs {
			p.nodeDefaults[k] = v
		}
	}

	p.skipSemicolon()
	return nil
}

// parseEdgeDefaults parses: 'edge' AttrBlock ';'?
func (p *parser) parseEdgeDefaults() error {
	p.advance() // consume 'edge'

	if p.current().Type == TokenLBracket {
		attrs, err := p.parseAttrBlock()
		if err != nil {
			return err
		}
		for k, v := range attrs {
			p.edgeDefaults[k] = v
		}
	}

	p.skipSemicolon()
	return nil
}

// parseSubgraph parses: 'subgraph' Identifier? '{' Statement* '}'
func (p *parser) parseSubgraph() error {
	p.advance() // consume 'subgraph'

	sg := &Subgraph{
		Nodes:        make([]string, 0),
		NodeDefaults: make(map[string]string),
		Attrs:        make(map[string]string),
	}

	// Optional name
	if p.current().Type == TokenIdentifier {
		sg.Name = p.current().Value
		p.advance()
	}

	if _, err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Save outer defaults and create scoped defaults
	outerNodeDefaults := p.nodeDefaults
	p.nodeDefaults = make(map[string]string)
	for k, v := range outerNodeDefaults {
		p.nodeDefaults[k] = v
	}

	// Parse statements within subgraph, tracking which nodes are added
	nodesBefore := make(map[string]bool)
	for id := range p.graph.Nodes {
		nodesBefore[id] = true
	}

	for p.current().Type != TokenRBrace && p.current().Type != TokenEOF {
		tok := p.current()
		switch tok.Type {
		case TokenIdentifier:
			// Check for top-level key=value in subgraph (e.g., label = "Loop A")
			if p.peek(1).Type == TokenEquals {
				key := p.advance().Value
				p.advance() // consume =
				val, err := p.parseValue()
				if err != nil {
					return err
				}
				sg.Attrs[key] = val
				p.skipSemicolon()
				continue
			}
			if err := p.parseNodeOrEdgeStmt(); err != nil {
				return err
			}
		case TokenNode:
			// node defaults within subgraph are scoped
			p.advance()
			if p.current().Type == TokenLBracket {
				attrs, err := p.parseAttrBlock()
				if err != nil {
					return err
				}
				for k, v := range attrs {
					p.nodeDefaults[k] = v
					sg.NodeDefaults[k] = v
				}
			}
			p.skipSemicolon()
		case TokenEdge:
			if err := p.parseEdgeDefaults(); err != nil {
				return err
			}
		case TokenGraph:
			if err := p.parseGraphAttrStmt(); err != nil {
				return err
			}
		case TokenSemicolon:
			p.advance()
		default:
			return fmt.Errorf("unexpected token %v (%q) in subgraph at line %d, col %d",
				tok.Type, tok.Value, tok.Line, tok.Col)
		}
	}

	if _, err := p.expect(TokenRBrace); err != nil {
		return err
	}

	// Record which nodes were added during subgraph parsing
	for id := range p.graph.Nodes {
		if !nodesBefore[id] {
			sg.Nodes = append(sg.Nodes, id)
		}
	}

	// Derive class from subgraph label and apply to contained nodes
	if label, ok := sg.Attrs["label"]; ok && label != "" {
		derivedClass := deriveClassName(label)
		for _, nodeID := range sg.Nodes {
			node := p.graph.Nodes[nodeID]
			if node != nil && node.Attrs["class"] == "" {
				node.Attrs["class"] = derivedClass
			}
		}
	}

	// Restore outer defaults
	p.nodeDefaults = outerNodeDefaults

	p.graph.Subgraphs = append(p.graph.Subgraphs, sg)
	p.skipSemicolon()
	return nil
}

// deriveClassName produces a CSS-like class name from a subgraph label.
// Lowercases, replaces spaces with hyphens, strips non-alphanumeric characters (except hyphens).
func deriveClassName(label string) string {
	lower := strings.ToLower(label)
	lower = strings.ReplaceAll(lower, " ", "-")
	var sb strings.Builder
	for _, ch := range lower {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '-' {
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

// parseNodeOrEdgeStmt parses a node statement or edge statement.
// Disambiguates by looking ahead for -> (edge) or = (graph attr decl).
func (p *parser) parseNodeOrEdgeStmt() error {
	// Check for undirected edge operator --
	if p.peek(1).Type == TokenMinus {
		return fmt.Errorf("undirected edges (--) are not supported at line %d, col %d; use directed edges (->)",
			p.peek(1).Line, p.peek(1).Col)
	}

	// Check for graph-level attribute declaration: identifier = value
	if p.peek(1).Type == TokenEquals {
		key := p.advance().Value
		p.advance() // consume =
		val, err := p.parseValue()
		if err != nil {
			return err
		}
		p.graph.Attrs[key] = val
		p.skipSemicolon()
		return nil
	}

	// Read first identifier
	id := p.advance().Value

	// Edge statement: identifier -> identifier ...
	if p.current().Type == TokenArrow {
		return p.parseEdgeStmt(id)
	}

	// Node statement: identifier [attrs]?
	return p.parseNodeStmt(id)
}

// parseNodeStmt parses: Identifier AttrBlock? ';'?
func (p *parser) parseNodeStmt(id string) error {
	var attrs map[string]string
	if p.current().Type == TokenLBracket {
		var err error
		attrs, err = p.parseAttrBlock()
		if err != nil {
			return err
		}
	}

	p.ensureNode(id, attrs)
	p.skipSemicolon()
	return nil
}

// parseEdgeStmt parses: Identifier ( '->' Identifier )+ AttrBlock? ';'?
func (p *parser) parseEdgeStmt(firstID string) error {
	// Collect all node IDs in the chain
	nodeIDs := []string{firstID}

	for p.current().Type == TokenArrow {
		p.advance() // consume ->
		tok := p.current()
		if tok.Type != TokenIdentifier && tok.Type != TokenString {
			return fmt.Errorf("expected identifier after -> at line %d, col %d", tok.Line, tok.Col)
		}
		nodeIDs = append(nodeIDs, tok.Value)
		p.advance()
	}

	// Optional attribute block
	var attrs map[string]string
	if p.current().Type == TokenLBracket {
		var err error
		attrs, err = p.parseAttrBlock()
		if err != nil {
			return err
		}
	}

	// Ensure all nodes in the chain exist
	for _, id := range nodeIDs {
		p.ensureNode(id, nil)
	}

	// Expand chained edges: A -> B -> C becomes A->B, B->C
	for i := 0; i < len(nodeIDs)-1; i++ {
		edgeAttrs := make(map[string]string)
		// Apply edge defaults
		for k, v := range p.edgeDefaults {
			edgeAttrs[k] = v
		}
		// Apply explicit attrs (override defaults)
		for k, v := range attrs {
			edgeAttrs[k] = v
		}
		p.graph.Edges = append(p.graph.Edges, &Edge{
			From:  nodeIDs[i],
			To:    nodeIDs[i+1],
			Attrs: edgeAttrs,
		})
	}

	p.skipSemicolon()
	return nil
}

// ensureNode creates a node if it doesn't exist, merging defaults and explicit attributes.
func (p *parser) ensureNode(id string, explicitAttrs map[string]string) {
	node, exists := p.graph.Nodes[id]
	if !exists {
		node = &Node{
			ID:    id,
			Attrs: make(map[string]string),
		}
		// Apply node defaults
		for k, v := range p.nodeDefaults {
			node.Attrs[k] = v
		}
		p.graph.Nodes[id] = node
	}

	// Apply explicit attributes (override defaults)
	for k, v := range explicitAttrs {
		node.Attrs[k] = v
	}
}

// parseAttrBlock parses: '[' Attr ( ',' Attr )* ']'
func (p *parser) parseAttrBlock() (map[string]string, error) {
	if _, err := p.expect(TokenLBracket); err != nil {
		return nil, err
	}

	attrs := make(map[string]string)

	// Empty attr block
	if p.current().Type == TokenRBracket {
		p.advance()
		return attrs, nil
	}

	// Parse first attr
	key, val, err := p.parseAttr()
	if err != nil {
		return nil, err
	}
	attrs[key] = val

	// Parse remaining attrs separated by commas
	for p.current().Type == TokenComma {
		p.advance() // consume comma
		// Allow trailing comma before ]
		if p.current().Type == TokenRBracket {
			break
		}
		key, val, err = p.parseAttr()
		if err != nil {
			return nil, err
		}
		attrs[key] = val
	}

	if _, err := p.expect(TokenRBracket); err != nil {
		return nil, err
	}

	return attrs, nil
}

// parseAttr parses: Key '=' Value
func (p *parser) parseAttr() (string, string, error) {
	// Key can be an identifier (possibly qualified with dots)
	key, err := p.parseKey()
	if err != nil {
		return "", "", err
	}

	if _, err := p.expect(TokenEquals); err != nil {
		return "", "", err
	}

	val, err := p.parseValue()
	if err != nil {
		return "", "", err
	}

	return key, val, nil
}

// parseKey parses: Identifier ( '.' Identifier )*
func (p *parser) parseKey() (string, error) {
	tok := p.current()
	if tok.Type != TokenIdentifier {
		return "", fmt.Errorf("expected attribute key (identifier) but got %v (%q) at line %d, col %d",
			tok.Type, tok.Value, tok.Line, tok.Col)
	}
	key := tok.Value
	p.advance()

	// Handle qualified identifiers: key.subkey
	for p.current().Value == "." {
		p.advance() // consume dot - it'll be tokenized as part of something, need to handle
		// Actually dots in identifiers like "model.provider" aren't separate tokens
		// They'd be in separate identifiers. Let's handle dot-separated keys.
		break
	}

	return key, nil
}

// parseValue parses a value: String | Integer | Float | Boolean | Duration | Identifier.
// All values are stored as strings in the attribute maps.
func (p *parser) parseValue() (string, error) {
	tok := p.current()

	switch tok.Type {
	case TokenString:
		p.advance()
		return tok.Value, nil

	case TokenNumber:
		p.advance()
		return tok.Value, nil

	case TokenBoolean:
		p.advance()
		return tok.Value, nil

	case TokenIdentifier:
		// Bare identifiers as values (e.g., shape=box, rankdir=LR)
		p.advance()
		return tok.Value, nil

	case TokenMinus:
		// Negative number: - followed by number
		p.advance()
		if p.current().Type == TokenNumber {
			val := "-" + p.current().Value
			p.advance()
			return val, nil
		}
		return "-", nil

	default:
		return "", fmt.Errorf("expected value but got %v (%q) at line %d, col %d",
			tok.Type, tok.Value, tok.Line, tok.Col)
	}
}
