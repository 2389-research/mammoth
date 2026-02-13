// ABOUTME: Common handler interface, registry, and shape-to-type mapping for the attractor pipeline runner.
// ABOUTME: All 9 built-in node handlers implement NodeHandler and are registered via DefaultHandlerRegistry.
package attractor

import (
	"context"
)

// NodeHandler is the interface that all node handlers implement.
// The execution engine dispatches to the appropriate handler based on node type or shape.
type NodeHandler interface {
	// Type returns the handler type string (e.g., "start", "codergen", "wait.human").
	Type() string

	// Execute runs the handler logic for the given node.
	// ctx is the Go context for cancellation/timeout.
	// node is the parsed Node with all its attributes.
	// pctx is the shared pipeline Context (thread-safe KV store).
	// store is the artifact store for large outputs.
	Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error)
}

// HandlerRegistry maps handler type strings to handler instances.
type HandlerRegistry struct {
	handlers map[string]NodeHandler
}

// NewHandlerRegistry creates a new empty handler registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string]NodeHandler),
	}
}

// Register adds a handler to the registry, keyed by its Type() string.
// Registering for an already-registered type replaces the previous handler.
func (r *HandlerRegistry) Register(handler NodeHandler) {
	r.handlers[handler.Type()] = handler
}

// Get returns the handler registered for the given type string, or nil if not found.
func (r *HandlerRegistry) Get(typeName string) NodeHandler {
	return r.handlers[typeName]
}

// Resolve finds the appropriate handler for a node using the resolution order:
// 1. Explicit type attribute on the node
// 2. Shape-based resolution using the shape-to-handler-type mapping
// 3. Default to codergen handler
func (r *HandlerRegistry) Resolve(node *Node) NodeHandler {
	// 1. Explicit type attribute
	if node.Attrs != nil {
		if typeName, ok := node.Attrs["type"]; ok && typeName != "" {
			if h, exists := r.handlers[typeName]; exists {
				return h
			}
		}
	}

	// 2. Shape-based resolution
	if node.Attrs != nil {
		if shape, ok := node.Attrs["shape"]; ok {
			handlerType := ShapeToHandlerType(shape)
			if h, exists := r.handlers[handlerType]; exists {
				return h
			}
		}
	}

	// 3. Default to codergen
	if h, exists := r.handlers["codergen"]; exists {
		return h
	}

	return nil
}

// DefaultHandlerRegistry creates a registry with all 9 built-in handlers registered.
func DefaultHandlerRegistry() *HandlerRegistry {
	reg := NewHandlerRegistry()
	reg.Register(&StartHandler{})
	reg.Register(&ExitHandler{})
	reg.Register(&CodergenHandler{})
	reg.Register(&ConditionalHandler{})
	reg.Register(&ParallelHandler{})
	reg.Register(&FanInHandler{})
	reg.Register(&ToolHandler{})
	reg.Register(&ManagerLoopHandler{})
	reg.Register(&WaitForHumanHandler{})
	return reg
}

// shapeToType maps Graphviz shape names to handler type strings.
var shapeToType = map[string]string{
	"Mdiamond":      "start",
	"Msquare":       "exit",
	"box":           "codergen",
	"diamond":       "conditional",
	"component":     "parallel",
	"tripleoctagon": "parallel.fan_in",
	"parallelogram": "tool",
	"house":         "stack.manager_loop",
	"hexagon":       "wait.human",
}

// ShapeToHandlerType returns the handler type string for a given Graphviz shape.
// Unknown shapes default to "codergen" (the LLM handler).
func ShapeToHandlerType(shape string) string {
	if t, ok := shapeToType[shape]; ok {
		return t
	}
	return "codergen"
}
