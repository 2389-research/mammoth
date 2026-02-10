// ABOUTME: Tests for Bubble Tea message types used in the TUI message loop.
// ABOUTME: Validates construction and field access for all Msg types with table-driven tests.
package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/2389-research/makeatron/attractor"
)

func TestEngineEventMsg(t *testing.T) {
	tests := []struct {
		name     string
		event    attractor.EngineEvent
		wantType attractor.EngineEventType
		wantNode string
	}{
		{
			name: "pipeline started event",
			event: attractor.EngineEvent{
				Type:      attractor.EventPipelineStarted,
				NodeID:    "",
				Data:      nil,
				Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantType: attractor.EventPipelineStarted,
			wantNode: "",
		},
		{
			name: "stage started event preserves node ID",
			event: attractor.EngineEvent{
				Type:      attractor.EventStageStarted,
				NodeID:    "codergen_1",
				Data:      map[string]any{"model": "gpt-4"},
				Timestamp: time.Date(2026, 2, 9, 12, 0, 0, 0, time.UTC),
			},
			wantType: attractor.EventStageStarted,
			wantNode: "codergen_1",
		},
		{
			name: "stage failed event with data",
			event: attractor.EngineEvent{
				Type:      attractor.EventStageFailed,
				NodeID:    "validate_3",
				Data:      map[string]any{"error": "timeout", "retries": 3},
				Timestamp: time.Date(2026, 6, 15, 8, 30, 0, 0, time.UTC),
			},
			wantType: attractor.EventStageFailed,
			wantNode: "validate_3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := EngineEventMsg{Event: tt.event}

			if msg.Event.Type != tt.wantType {
				t.Errorf("Event.Type = %q, want %q", msg.Event.Type, tt.wantType)
			}
			if msg.Event.NodeID != tt.wantNode {
				t.Errorf("Event.NodeID = %q, want %q", msg.Event.NodeID, tt.wantNode)
			}
			if msg.Event.Timestamp != tt.event.Timestamp {
				t.Errorf("Event.Timestamp = %v, want %v", msg.Event.Timestamp, tt.event.Timestamp)
			}
			if tt.event.Data != nil {
				if msg.Event.Data == nil {
					t.Fatal("Event.Data is nil, want non-nil")
				}
				for k, v := range tt.event.Data {
					if msg.Event.Data[k] != v {
						t.Errorf("Event.Data[%q] = %v, want %v", k, msg.Event.Data[k], v)
					}
				}
			}
		})
	}
}

func TestPipelineResultMsg(t *testing.T) {
	tests := []struct {
		name       string
		result     *attractor.RunResult
		err        error
		wantErr    bool
		wantResult bool
	}{
		{
			name: "success with result",
			result: &attractor.RunResult{
				CompletedNodes: []string{"start", "codergen", "exit"},
				NodeOutcomes:   map[string]*attractor.Outcome{},
			},
			err:        nil,
			wantErr:    false,
			wantResult: true,
		},
		{
			name:       "failure with error",
			result:     nil,
			err:        errors.New("pipeline execution failed"),
			wantErr:    true,
			wantResult: false,
		},
		{
			name: "partial failure with both result and error",
			result: &attractor.RunResult{
				CompletedNodes: []string{"start"},
			},
			err:        errors.New("stage 2 timed out"),
			wantErr:    true,
			wantResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := PipelineResultMsg{Result: tt.result, Err: tt.err}

			if (msg.Err != nil) != tt.wantErr {
				t.Errorf("Err presence = %v, want %v", msg.Err != nil, tt.wantErr)
			}
			if (msg.Result != nil) != tt.wantResult {
				t.Errorf("Result presence = %v, want %v", msg.Result != nil, tt.wantResult)
			}
			if tt.wantResult && msg.Result != nil {
				if len(msg.Result.CompletedNodes) != len(tt.result.CompletedNodes) {
					t.Errorf("CompletedNodes length = %d, want %d",
						len(msg.Result.CompletedNodes), len(tt.result.CompletedNodes))
				}
			}
			if tt.wantErr && msg.Err != nil {
				if msg.Err.Error() != tt.err.Error() {
					t.Errorf("Err = %q, want %q", msg.Err.Error(), tt.err.Error())
				}
			}
		})
	}
}

func TestTickMsg(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
	}{
		{
			name: "zero time",
			time: time.Time{},
		},
		{
			name: "specific time",
			time: time.Date(2026, 2, 9, 15, 30, 45, 0, time.UTC),
		},
		{
			name: "now",
			time: time.Now(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := TickMsg{Time: tt.time}

			if !msg.Time.Equal(tt.time) {
				t.Errorf("Time = %v, want %v", msg.Time, tt.time)
			}
		})
	}
}

func TestHumanGateRequestMsg(t *testing.T) {
	tests := []struct {
		name     string
		question string
		options  []string
		wantLen  int
	}{
		{
			name:     "simple yes/no question",
			question: "Approve deployment?",
			options:  []string{"yes", "no"},
			wantLen:  2,
		},
		{
			name:     "multi-option question",
			question: "Select environment:",
			options:  []string{"dev", "staging", "production"},
			wantLen:  3,
		},
		{
			name:     "open-ended question with no options",
			question: "Please describe the desired behavior:",
			options:  nil,
			wantLen:  0,
		},
		{
			name:     "empty question",
			question: "",
			options:  []string{},
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := HumanGateRequestMsg{Question: tt.question, Options: tt.options}

			if msg.Question != tt.question {
				t.Errorf("Question = %q, want %q", msg.Question, tt.question)
			}
			if len(msg.Options) != tt.wantLen {
				t.Errorf("Options length = %d, want %d", len(msg.Options), tt.wantLen)
			}
			for i, opt := range tt.options {
				if msg.Options[i] != opt {
					t.Errorf("Options[%d] = %q, want %q", i, msg.Options[i], opt)
				}
			}
		})
	}
}

func TestHumanGateResponseMsg(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		err     error
		wantErr bool
	}{
		{
			name:    "successful response",
			answer:  "yes",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "response with error",
			answer:  "",
			err:     errors.New("user cancelled"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := HumanGateResponseMsg{Answer: tt.answer, Err: tt.err}

			if msg.Answer != tt.answer {
				t.Errorf("Answer = %q, want %q", msg.Answer, tt.answer)
			}
			if (msg.Err != nil) != tt.wantErr {
				t.Errorf("Err presence = %v, want %v", msg.Err != nil, tt.wantErr)
			}
		})
	}
}

func TestWindowSizeMsg(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{
			name:   "standard terminal",
			width:  80,
			height: 24,
		},
		{
			name:   "wide terminal",
			width:  200,
			height: 50,
		},
		{
			name:   "zero size",
			width:  0,
			height: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := WindowSizeMsg{Width: tt.width, Height: tt.height}

			if msg.Width != tt.width {
				t.Errorf("Width = %d, want %d", msg.Width, tt.width)
			}
			if msg.Height != tt.height {
				t.Errorf("Height = %d, want %d", msg.Height, tt.height)
			}
		})
	}
}
