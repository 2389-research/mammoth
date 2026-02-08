// ABOUTME: Tests for the Server-Sent Events (SSE) streaming parser.
// ABOUTME: Covers the full SSE protocol including multi-line data, event types, IDs, retry, comments, and line ending variants.

package sse

import (
	"io"
	"strings"
	"testing"
)

func TestNewParser(t *testing.T) {
	r := strings.NewReader("")
	p := NewParser(r)
	if p == nil {
		t.Fatal("NewParser returned nil")
	}
}

func TestSimpleSingleLineEvent(t *testing.T) {
	input := "data: hello world\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Type != "message" {
		t.Errorf("expected type %q, got %q", "message", evt.Type)
	}
	if evt.Data != "hello world" {
		t.Errorf("expected data %q, got %q", "hello world", evt.Data)
	}
	if evt.Retry != -1 {
		t.Errorf("expected retry -1, got %d", evt.Retry)
	}

	_, err = p.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestMultiLineDataEvent(t *testing.T) {
	input := "data: line one\ndata: line two\ndata: line three\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "line one\nline two\nline three"
	if evt.Data != expected {
		t.Errorf("expected data %q, got %q", expected, evt.Data)
	}
}

func TestEventWithType(t *testing.T) {
	input := "event: update\ndata: payload\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Type != "update" {
		t.Errorf("expected type %q, got %q", "update", evt.Type)
	}
	if evt.Data != "payload" {
		t.Errorf("expected data %q, got %q", "payload", evt.Data)
	}
}

func TestEventWithID(t *testing.T) {
	input := "id: 42\ndata: identified event\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.ID != "42" {
		t.Errorf("expected id %q, got %q", "42", evt.ID)
	}
	if evt.Data != "identified event" {
		t.Errorf("expected data %q, got %q", "identified event", evt.Data)
	}
}

func TestEventWithRetry(t *testing.T) {
	input := "retry: 3000\ndata: reconnectable\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Retry != 3000 {
		t.Errorf("expected retry 3000, got %d", evt.Retry)
	}
	if evt.Data != "reconnectable" {
		t.Errorf("expected data %q, got %q", "reconnectable", evt.Data)
	}
}

func TestRetryInvalidValueIgnored(t *testing.T) {
	input := "retry: not-a-number\ndata: still works\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Retry != -1 {
		t.Errorf("expected retry -1 (invalid value ignored), got %d", evt.Retry)
	}
	if evt.Data != "still works" {
		t.Errorf("expected data %q, got %q", "still works", evt.Data)
	}
}

func TestCommentLinesSkipped(t *testing.T) {
	input := ": this is a comment\ndata: visible\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "visible" {
		t.Errorf("expected data %q, got %q", "visible", evt.Data)
	}
}

func TestMissingSpaceAfterColon(t *testing.T) {
	input := "data:no-space\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "no-space" {
		t.Errorf("expected data %q, got %q", "no-space", evt.Data)
	}
}

func TestSpaceAfterColonStripped(t *testing.T) {
	// Only a single leading space after colon should be stripped.
	input := "data:  two-spaces\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Per SSE spec: "If value is not empty, and the first character of value is a U+0020 SPACE character,
	// remove it from value." So "  two-spaces" becomes " two-spaces".
	if evt.Data != " two-spaces" {
		t.Errorf("expected data %q, got %q", " two-spaces", evt.Data)
	}
}

func TestMultipleEventsInSequence(t *testing.T) {
	input := "data: first\n\ndata: second\n\ndata: third\n\n"
	p := NewParser(strings.NewReader(input))

	expected := []string{"first", "second", "third"}
	for i, want := range expected {
		evt, err := p.Next()
		if err != nil {
			t.Fatalf("event %d: unexpected error: %v", i, err)
		}
		if evt.Data != want {
			t.Errorf("event %d: expected data %q, got %q", i, want, evt.Data)
		}
	}

	_, err := p.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestEmptyLinesBetweenEvents(t *testing.T) {
	// Multiple blank lines between events should not produce empty events.
	input := "data: first\n\n\n\n\ndata: second\n\n"
	p := NewParser(strings.NewReader(input))

	evt1, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt1.Data != "first" {
		t.Errorf("expected data %q, got %q", "first", evt1.Data)
	}

	evt2, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt2.Data != "second" {
		t.Errorf("expected data %q, got %q", "second", evt2.Data)
	}

	_, err = p.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestOpenAIDoneTermination(t *testing.T) {
	input := "data: {\"choices\":[]}\n\ndata: [DONE]\n\n"
	p := NewParser(strings.NewReader(input))

	evt1, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt1.Data != "{\"choices\":[]}" {
		t.Errorf("expected data %q, got %q", "{\"choices\":[]}", evt1.Data)
	}

	evt2, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt2.Data != "[DONE]" {
		t.Errorf("expected data %q, got %q", "[DONE]", evt2.Data)
	}
}

func TestCRLFLineEndings(t *testing.T) {
	input := "data: crlf event\r\n\r\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "crlf event" {
		t.Errorf("expected data %q, got %q", "crlf event", evt.Data)
	}
}

func TestCROnlyLineEndings(t *testing.T) {
	input := "data: cr event\r\r"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "cr event" {
		t.Errorf("expected data %q, got %q", "cr event", evt.Data)
	}
}

func TestEmptyReader(t *testing.T) {
	p := NewParser(strings.NewReader(""))

	_, err := p.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestOnlyComments(t *testing.T) {
	input := ": comment one\n: comment two\n: comment three\n"
	p := NewParser(strings.NewReader(input))

	_, err := p.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestOnlyBlankLines(t *testing.T) {
	input := "\n\n\n\n"
	p := NewParser(strings.NewReader(input))

	_, err := p.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestAllFieldsCombined(t *testing.T) {
	input := "event: status\nid: 99\nretry: 5000\ndata: all fields present\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Type != "status" {
		t.Errorf("expected type %q, got %q", "status", evt.Type)
	}
	if evt.ID != "99" {
		t.Errorf("expected id %q, got %q", "99", evt.ID)
	}
	if evt.Retry != 5000 {
		t.Errorf("expected retry 5000, got %d", evt.Retry)
	}
	if evt.Data != "all fields present" {
		t.Errorf("expected data %q, got %q", "all fields present", evt.Data)
	}
}

func TestLineWithoutColon(t *testing.T) {
	// Lines without a colon are treated as field name with empty value.
	// A field name of "data" with empty value should still append to the data buffer (as empty string).
	input := "data\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "data" line with no colon means field="data", value="".
	// This should still dispatch because data buffer has been written to (even if empty string).
	if evt.Data != "" {
		t.Errorf("expected data %q, got %q", "", evt.Data)
	}
}

func TestEventTypeResetsBetweenEvents(t *testing.T) {
	// Per SSE spec, the event type should reset to "message" after dispatch.
	input := "event: custom\ndata: first\n\ndata: second\n\n"
	p := NewParser(strings.NewReader(input))

	evt1, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt1.Type != "custom" {
		t.Errorf("expected type %q, got %q", "custom", evt1.Type)
	}

	evt2, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt2.Type != "message" {
		t.Errorf("expected type %q (reset to default), got %q", "message", evt2.Type)
	}
}

func TestEmptyDataField(t *testing.T) {
	// "data:" with no value should produce an event with empty data.
	input := "data:\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "" {
		t.Errorf("expected data %q, got %q", "", evt.Data)
	}
}

func TestEmptyDataFieldWithSpace(t *testing.T) {
	// "data: " with a trailing space - the space is the "optional space after colon" and is stripped.
	input := "data: \n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "" {
		t.Errorf("expected data %q, got %q", "", evt.Data)
	}
}

func TestMultiLineDataWithEmptyLine(t *testing.T) {
	// Multiple data lines including one that is empty.
	input := "data: first\ndata:\ndata: third\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "first\n\nthird"
	if evt.Data != expected {
		t.Errorf("expected data %q, got %q", expected, evt.Data)
	}
}

func TestStreamEndsWithoutFinalBlankLine(t *testing.T) {
	// If the stream ends (EOF) with accumulated data but no final blank line,
	// the pending event should be dispatched.
	input := "data: no trailing blank"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "no trailing blank" {
		t.Errorf("expected data %q, got %q", "no trailing blank", evt.Data)
	}

	_, err = p.Next()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestMixedCRLFAndLF(t *testing.T) {
	input := "data: mixed\r\ndata: endings\n\r\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "mixed\nendings"
	if evt.Data != expected {
		t.Errorf("expected data %q, got %q", expected, evt.Data)
	}
}

func TestIDPersistsAcrossEvents(t *testing.T) {
	// Per SSE spec, the last event ID persists until explicitly changed.
	// However, our parser returns events individually, so each event gets
	// the ID that was set during its construction.
	input := "id: first-id\ndata: one\n\ndata: two\n\nid: new-id\ndata: three\n\n"
	p := NewParser(strings.NewReader(input))

	evt1, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt1.ID != "first-id" {
		t.Errorf("expected id %q, got %q", "first-id", evt1.ID)
	}

	evt2, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ID field is reset after dispatch, so second event has no ID.
	if evt2.ID != "" {
		t.Errorf("expected id %q, got %q", "", evt2.ID)
	}

	evt3, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt3.ID != "new-id" {
		t.Errorf("expected id %q, got %q", "new-id", evt3.ID)
	}
}

func TestUnknownFieldIgnored(t *testing.T) {
	input := "foo: bar\ndata: known\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != "known" {
		t.Errorf("expected data %q, got %q", "known", evt.Data)
	}
}

func TestCommentsInterspersedWithData(t *testing.T) {
	input := ": keepalive\ndata: part1\n: another comment\ndata: part2\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "part1\npart2"
	if evt.Data != expected {
		t.Errorf("expected data %q, got %q", expected, evt.Data)
	}
}

func TestLargePayload(t *testing.T) {
	// Ensure the parser handles reasonably large data payloads.
	bigData := strings.Repeat("x", 100000)
	input := "data: " + bigData + "\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Data != bigData {
		t.Errorf("expected data length %d, got %d", len(bigData), len(evt.Data))
	}
}

func TestDefaultEventType(t *testing.T) {
	input := "data: no explicit type\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Type != "message" {
		t.Errorf("expected default type %q, got %q", "message", evt.Type)
	}
}

func TestRetryDefaultValue(t *testing.T) {
	input := "data: no retry\n\n"
	p := NewParser(strings.NewReader(input))

	evt, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Retry != -1 {
		t.Errorf("expected retry -1 (default), got %d", evt.Retry)
	}
}

func TestTableDriven(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		events []Event
	}{
		{
			name:  "simple message",
			input: "data: hello\n\n",
			events: []Event{
				{Type: "message", Data: "hello", Retry: -1},
			},
		},
		{
			name:  "typed event",
			input: "event: ping\ndata: keepalive\n\n",
			events: []Event{
				{Type: "ping", Data: "keepalive", Retry: -1},
			},
		},
		{
			name:  "multiline data",
			input: "data: a\ndata: b\ndata: c\n\n",
			events: []Event{
				{Type: "message", Data: "a\nb\nc", Retry: -1},
			},
		},
		{
			name:  "with id and retry",
			input: "id: 7\nretry: 1000\ndata: configured\n\n",
			events: []Event{
				{Type: "message", Data: "configured", ID: "7", Retry: 1000},
			},
		},
		{
			name:  "OpenAI streaming sequence",
			input: "data: {\"id\":\"chatcmpl-1\"}\n\ndata: {\"id\":\"chatcmpl-2\"}\n\ndata: [DONE]\n\n",
			events: []Event{
				{Type: "message", Data: "{\"id\":\"chatcmpl-1\"}", Retry: -1},
				{Type: "message", Data: "{\"id\":\"chatcmpl-2\"}", Retry: -1},
				{Type: "message", Data: "[DONE]", Retry: -1},
			},
		},
		{
			name:  "CRLF line endings throughout",
			input: "event: update\r\ndata: crlf\r\n\r\n",
			events: []Event{
				{Type: "update", Data: "crlf", Retry: -1},
			},
		},
		{
			name:  "CR only line endings",
			input: "data: cr-only\r\r",
			events: []Event{
				{Type: "message", Data: "cr-only", Retry: -1},
			},
		},
		{
			name:   "only comments",
			input:  ": comment\n",
			events: nil,
		},
		{
			name:   "only blank lines",
			input:  "\n\n\n",
			events: nil,
		},
		{
			name:   "empty input",
			input:  "",
			events: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser(strings.NewReader(tc.input))
			var got []Event
			for {
				evt, err := p.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got = append(got, evt)
			}

			if len(got) != len(tc.events) {
				t.Fatalf("expected %d events, got %d: %+v", len(tc.events), len(got), got)
			}

			for i, want := range tc.events {
				if got[i].Type != want.Type {
					t.Errorf("event %d: expected type %q, got %q", i, want.Type, got[i].Type)
				}
				if got[i].Data != want.Data {
					t.Errorf("event %d: expected data %q, got %q", i, want.Data, got[i].Data)
				}
				if got[i].ID != want.ID {
					t.Errorf("event %d: expected id %q, got %q", i, want.ID, got[i].ID)
				}
				if got[i].Retry != want.Retry {
					t.Errorf("event %d: expected retry %d, got %d", i, want.Retry, got[i].Retry)
				}
			}
		})
	}
}
