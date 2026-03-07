// ABOUTME: Tests for the applyAutoStatus function that auto-generates SUCCESS outcomes.
// ABOUTME: Uses table-driven tests covering enabled, explicit status, disabled, missing attr, and nil attrs cases.
package attractor

import "testing"

func TestApplyAutoStatus(t *testing.T) {
	tests := []struct {
		name           string
		node           *Node
		outcome        *Outcome
		wantStatus     StageStatus
		wantNotes      string
		wantFailReason string
	}{
		{
			name: "auto_status true with empty status generates SUCCESS",
			node: &Node{
				ID:    "n1",
				Attrs: map[string]string{"auto_status": "true"},
			},
			outcome:    &Outcome{Status: ""},
			wantStatus: StatusSuccess,
			wantNotes:  "auto_status: generated SUCCESS for node n1",
		},
		{
			name: "auto_status true with explicit StatusFail preserves fail",
			node: &Node{
				ID:    "n2",
				Attrs: map[string]string{"auto_status": "true"},
			},
			outcome:        &Outcome{Status: StatusFail, FailureReason: "something broke"},
			wantStatus:     StatusFail,
			wantFailReason: "something broke",
		},
		{
			name: "auto_status false with empty status stays empty",
			node: &Node{
				ID:    "n3",
				Attrs: map[string]string{"auto_status": "false"},
			},
			outcome:    &Outcome{Status: ""},
			wantStatus: "",
		},
		{
			name: "missing auto_status attr with empty status stays empty",
			node: &Node{
				ID:    "n4",
				Attrs: map[string]string{"label": "some node"},
			},
			outcome:    &Outcome{Status: ""},
			wantStatus: "",
		},
		{
			name:       "nil Attrs map with empty status stays empty",
			node:       &Node{ID: "n5", Attrs: nil},
			outcome:    &Outcome{Status: ""},
			wantStatus: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyAutoStatus(tt.node, tt.outcome)

			if result.Status != tt.wantStatus {
				t.Errorf("Status: got %q, want %q", result.Status, tt.wantStatus)
			}
			if tt.wantNotes != "" && result.Notes != tt.wantNotes {
				t.Errorf("Notes: got %q, want %q", result.Notes, tt.wantNotes)
			}
			if tt.wantFailReason != "" && result.FailureReason != tt.wantFailReason {
				t.Errorf("FailureReason: got %q, want %q", result.FailureReason, tt.wantFailReason)
			}
		})
	}
}
