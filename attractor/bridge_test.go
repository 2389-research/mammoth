// ABOUTME: Tests for the agent-to-engine event bridge that translates SessionEvent -> EngineEvent.
// ABOUTME: Validates event translation, tool call log building, turn counting, and goroutine lifecycle.
package attractor

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/2389-research/mammoth/agent"
	"github.com/2389-research/mammoth/llm"
)

func TestBridgeTranslatesToolCallStart(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "test_node", handler, nil, nil, nil)
		}
	}()

	emitter.Emit(agent.SessionEvent{
		Kind:      agent.EventToolCallStart,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{"tool_name": "file_write", "call_id": "tc_1"},
	})

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventAgentToolCallStart {
		t.Errorf("expected EventAgentToolCallStart, got %q", events[0].Type)
	}
	if events[0].NodeID != "test_node" {
		t.Errorf("expected nodeID 'test_node', got %q", events[0].NodeID)
	}
	if events[0].Data["tool_name"] != "file_write" {
		t.Errorf("expected tool_name 'file_write', got %v", events[0].Data["tool_name"])
	}
	if events[0].Data["call_id"] != "tc_1" {
		t.Errorf("expected call_id 'tc_1', got %v", events[0].Data["call_id"])
	}
}

func TestBridgeTranslatesToolCallEnd(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex
	var toolLog []ToolCallEntry
	var toolLogMu sync.Mutex
	toolStarts := &sync.Map{}

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "test_node", handler, toolStarts, &toolLog, &toolLogMu)
		}
	}()

	startTime := time.Now()
	toolStarts.Store("tc_1", startTime)
	toolStarts.Store("tc_1_name", "bash")

	emitter.Emit(agent.SessionEvent{
		Kind:      agent.EventToolCallEnd,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{"call_id": "tc_1", "output": "hello world output"},
	})

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventAgentToolCallEnd {
		t.Errorf("expected EventAgentToolCallEnd, got %q", events[0].Type)
	}
	if events[0].Data["tool_name"] != "bash" {
		t.Errorf("expected tool_name 'bash', got %v", events[0].Data["tool_name"])
	}
	if events[0].Data["call_id"] != "tc_1" {
		t.Errorf("expected call_id 'tc_1', got %v", events[0].Data["call_id"])
	}
	if events[0].Data["output_snippet"] != "hello world output" {
		t.Errorf("expected output_snippet 'hello world output', got %v", events[0].Data["output_snippet"])
	}
	if _, ok := events[0].Data["duration_ms"]; !ok {
		t.Error("expected duration_ms in event data")
	}

	toolLogMu.Lock()
	defer toolLogMu.Unlock()
	if len(toolLog) != 1 {
		t.Fatalf("expected 1 tool log entry, got %d", len(toolLog))
	}
	if toolLog[0].ToolName != "bash" {
		t.Errorf("expected tool log entry tool_name 'bash', got %q", toolLog[0].ToolName)
	}
}

func TestBridgeTranslatesToolCallEndOutputTruncation(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex
	var toolLog []ToolCallEntry
	var toolLogMu sync.Mutex
	toolStarts := &sync.Map{}

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "n", handler, toolStarts, &toolLog, &toolLogMu)
		}
	}()

	toolStarts.Store("tc_long", time.Now())
	toolStarts.Store("tc_long_name", "bash")

	longOutput := strings.Repeat("x", 600)
	emitter.Emit(agent.SessionEvent{
		Kind:      agent.EventToolCallEnd,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{"call_id": "tc_long", "output": longOutput},
	})

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	// output_snippet should be truncated to 500 chars in the event
	snippet, ok := events[0].Data["output_snippet"].(string)
	if !ok {
		t.Fatal("expected output_snippet to be a string")
	}
	if len(snippet) > 500 {
		t.Errorf("expected output_snippet <= 500 chars, got %d", len(snippet))
	}

	// tool log output should also be truncated to 500 chars
	toolLogMu.Lock()
	defer toolLogMu.Unlock()
	if len(toolLog[0].Output) > 500 {
		t.Errorf("expected tool log output <= 500 chars, got %d", len(toolLog[0].Output))
	}
}

func TestBridgeTranslatesAssistantTextEnd(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex
	var turnCount int32

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "test_node", handler, nil, nil, nil)
			if evt.Kind == agent.EventAssistantTextEnd {
				atomicAddInt32(&turnCount, 1)
			}
		}
	}()

	emitter.Emit(agent.SessionEvent{
		Kind:      agent.EventAssistantTextEnd,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{"text": "Hello world", "reasoning": "thinking..."},
	})

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventAgentLLMTurn {
		t.Errorf("expected EventAgentLLMTurn, got %q", events[0].Type)
	}
	if events[0].Data["text_length"] != 11 {
		t.Errorf("expected text_length 11, got %v", events[0].Data["text_length"])
	}
	if events[0].Data["has_reasoning"] != true {
		t.Errorf("expected has_reasoning true, got %v", events[0].Data["has_reasoning"])
	}
	if turnCount != 1 {
		t.Errorf("expected turn count 1, got %d", turnCount)
	}
}

func TestBridgeTranslatesSteeringInjected(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "test_node", handler, nil, nil, nil)
		}
	}()

	emitter.Emit(agent.SessionEvent{
		Kind:      agent.EventSteeringInjected,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{"content": "focus on the task"},
	})

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventAgentSteering {
		t.Errorf("expected EventAgentSteering, got %q", events[0].Type)
	}
	if events[0].Data["message"] != "focus on the task" {
		t.Errorf("expected message 'focus on the task', got %v", events[0].Data["message"])
	}
}

func TestBridgeTranslatesLoopDetection(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "test_node", handler, nil, nil, nil)
		}
	}()

	emitter.Emit(agent.SessionEvent{
		Kind:      agent.EventLoopDetection,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{"message": "repeating pattern detected"},
	})

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventAgentLoopDetected {
		t.Errorf("expected EventAgentLoopDetected, got %q", events[0].Type)
	}
	if events[0].Data["message"] != "repeating pattern detected" {
		t.Errorf("expected message, got %v", events[0].Data["message"])
	}
}

func TestBridgeIgnoresUnmappedEvents(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "test_node", handler, nil, nil, nil)
		}
	}()

	// These events should not produce EngineEvents
	for _, kind := range []agent.EventKind{
		agent.EventSessionStart,
		agent.EventSessionEnd,
		agent.EventUserInput,
		agent.EventToolCallOutputDelta,
		agent.EventTurnLimit,
		agent.EventError,
	} {
		emitter.Emit(agent.SessionEvent{
			Kind:      kind,
			Timestamp: time.Now(),
			SessionID: "s1",
		})
	}

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 0 {
		t.Errorf("expected 0 events for unmapped event kinds, got %d", len(events))
	}
}

func TestBridgeTranslatesAssistantTextStart(t *testing.T) {
	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ts := time.Now()
	evt := agent.SessionEvent{
		Kind:      agent.EventAssistantTextStart,
		Timestamp: ts,
		SessionID: "s1",
		Data:      nil,
	}

	bridgeSessionEvent(evt, "stream_node", handler, nil, nil, nil)

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventAgentTextStart {
		t.Errorf("expected EventAgentTextStart, got %q", events[0].Type)
	}
	if events[0].NodeID != "stream_node" {
		t.Errorf("expected nodeID 'stream_node', got %q", events[0].NodeID)
	}
	if events[0].Timestamp != ts {
		t.Errorf("expected timestamp to be forwarded")
	}
	if events[0].Data == nil {
		t.Error("expected non-nil Data map")
	}
}

func TestBridgeTranslatesAssistantTextDelta(t *testing.T) {
	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ts := time.Now()
	evt := agent.SessionEvent{
		Kind:      agent.EventAssistantTextDelta,
		Timestamp: ts,
		SessionID: "s1",
		Data:      map[string]any{"text": "Hello, world!"},
	}

	bridgeSessionEvent(evt, "delta_node", handler, nil, nil, nil)

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventAgentTextDelta {
		t.Errorf("expected EventAgentTextDelta, got %q", events[0].Type)
	}
	if events[0].NodeID != "delta_node" {
		t.Errorf("expected nodeID 'delta_node', got %q", events[0].NodeID)
	}
	if events[0].Timestamp != ts {
		t.Errorf("expected timestamp to be forwarded")
	}
	if events[0].Data["text"] != "Hello, world!" {
		t.Errorf("expected text 'Hello, world!', got %v", events[0].Data["text"])
	}
}

func TestBridgeAssistantTextDeltaEmptyText(t *testing.T) {
	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	evt := agent.SessionEvent{
		Kind:      agent.EventAssistantTextDelta,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{},
	}

	bridgeSessionEvent(evt, "empty_node", handler, nil, nil, nil)

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data["text"] != "" {
		t.Errorf("expected empty text for missing data field, got %v", events[0].Data["text"])
	}
}

func TestBridgeToolCallStartForwardsArguments(t *testing.T) {
	var events []EngineEvent
	var mu sync.Mutex
	toolStarts := &sync.Map{}

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	evt := agent.SessionEvent{
		Kind:      agent.EventToolCallStart,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data: map[string]any{
			"tool_name": "file_write",
			"call_id":   "tc_args",
			"arguments": `{"path":"/tmp/test.txt","content":"hello"}`,
		},
	}

	bridgeSessionEvent(evt, "args_node", handler, toolStarts, nil, nil)

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data["arguments"] != `{"path":"/tmp/test.txt","content":"hello"}` {
		t.Errorf("expected arguments to be forwarded, got %v", events[0].Data["arguments"])
	}
}

func TestBridgeToolCallStartOmitsEmptyArguments(t *testing.T) {
	var events []EngineEvent
	var mu sync.Mutex
	toolStarts := &sync.Map{}

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	evt := agent.SessionEvent{
		Kind:      agent.EventToolCallStart,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data: map[string]any{
			"tool_name": "file_read",
			"call_id":   "tc_noargs",
		},
	}

	bridgeSessionEvent(evt, "noargs_node", handler, toolStarts, nil, nil)

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if _, hasArgs := events[0].Data["arguments"]; hasArgs {
		t.Error("expected arguments key to be absent when arguments are empty")
	}
}

func TestBridgeForwardsTokenDataInLLMTurnEvent(t *testing.T) {
	emitter := agent.NewEventEmitter()
	defer emitter.Close()

	var events []EngineEvent
	var mu sync.Mutex

	handler := func(evt EngineEvent) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	}

	ch := emitter.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			bridgeSessionEvent(evt, "test_node", handler, nil, nil, nil)
		}
	}()

	reasoning := 50
	cacheRead := 200
	cacheWrite := 100
	emitter.Emit(agent.SessionEvent{
		Kind:      agent.EventAssistantTextEnd,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data: map[string]any{
			"text":               "output",
			"reasoning":          "thinking...",
			"input_tokens":       1000,
			"output_tokens":      500,
			"total_tokens":       1500,
			"reasoning_tokens":   &reasoning,
			"cache_read_tokens":  &cacheRead,
			"cache_write_tokens": &cacheWrite,
		},
	})

	emitter.Unsubscribe(ch)
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]
	if evt.Data["input_tokens"] != 1000 {
		t.Errorf("expected input_tokens=1000, got %v", evt.Data["input_tokens"])
	}
	if evt.Data["output_tokens"] != 500 {
		t.Errorf("expected output_tokens=500, got %v", evt.Data["output_tokens"])
	}
	if evt.Data["total_tokens"] != 1500 {
		t.Errorf("expected total_tokens=1500, got %v", evt.Data["total_tokens"])
	}
	if evt.Data["reasoning_tokens"] == nil {
		t.Error("expected reasoning_tokens to be present")
	}
	if evt.Data["cache_read_tokens"] == nil {
		t.Error("expected cache_read_tokens to be present")
	}
	if evt.Data["cache_write_tokens"] == nil {
		t.Error("expected cache_write_tokens to be present")
	}
}

func TestTokenUsageFromLLMConvertsAllFields(t *testing.T) {
	reasoning := 50
	cacheRead := 200
	cacheWrite := 100

	usage := llm.Usage{
		InputTokens:      1000,
		OutputTokens:     500,
		TotalTokens:      1500,
		ReasoningTokens:  &reasoning,
		CacheReadTokens:  &cacheRead,
		CacheWriteTokens: &cacheWrite,
	}

	tu := tokenUsageFromLLM(usage)

	if tu.InputTokens != 1000 {
		t.Errorf("expected InputTokens=1000, got %d", tu.InputTokens)
	}
	if tu.OutputTokens != 500 {
		t.Errorf("expected OutputTokens=500, got %d", tu.OutputTokens)
	}
	if tu.TotalTokens != 1500 {
		t.Errorf("expected TotalTokens=1500, got %d", tu.TotalTokens)
	}
	if tu.ReasoningTokens != 50 {
		t.Errorf("expected ReasoningTokens=50, got %d", tu.ReasoningTokens)
	}
	if tu.CacheReadTokens != 200 {
		t.Errorf("expected CacheReadTokens=200, got %d", tu.CacheReadTokens)
	}
	if tu.CacheWriteTokens != 100 {
		t.Errorf("expected CacheWriteTokens=100, got %d", tu.CacheWriteTokens)
	}
}

func TestTokenUsageFromLLMHandlesNilPointers(t *testing.T) {
	usage := llm.Usage{
		InputTokens:  500,
		OutputTokens: 300,
		TotalTokens:  800,
		// All pointer fields are nil
	}

	tu := tokenUsageFromLLM(usage)

	if tu.ReasoningTokens != 0 {
		t.Errorf("expected ReasoningTokens=0 for nil, got %d", tu.ReasoningTokens)
	}
	if tu.CacheReadTokens != 0 {
		t.Errorf("expected CacheReadTokens=0 for nil, got %d", tu.CacheReadTokens)
	}
	if tu.CacheWriteTokens != 0 {
		t.Errorf("expected CacheWriteTokens=0 for nil, got %d", tu.CacheWriteTokens)
	}
}

func TestBridgeToolCallStartRecordsStartTime(t *testing.T) {
	toolStarts := &sync.Map{}

	evt := agent.SessionEvent{
		Kind:      agent.EventToolCallStart,
		Timestamp: time.Now(),
		SessionID: "s1",
		Data:      map[string]any{"tool_name": "file_read", "call_id": "tc_42"},
	}

	bridgeSessionEvent(evt, "n", func(EngineEvent) {}, toolStarts, nil, nil)

	// Verify that start time was recorded
	if _, ok := toolStarts.Load("tc_42"); !ok {
		t.Error("expected tool start time to be recorded for call_id 'tc_42'")
	}
	if name, ok := toolStarts.Load("tc_42_name"); !ok || name != "file_read" {
		t.Errorf("expected tool name 'file_read' recorded, got %v", name)
	}
}
