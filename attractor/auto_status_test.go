// ABOUTME: Tests for the applyAutoStatus function that auto-generates SUCCESS outcomes.
// ABOUTME: Covers auto_status=true, explicit status preservation, false, and missing attribute cases.
package attractor

import "testing"

func TestAutoStatusGeneratesSuccessWhenStatusEmpty(t *testing.T) {
	node := &Node{
		ID:    "n1",
		Attrs: map[string]string{"auto_status": "true"},
	}
	outcome := &Outcome{Status: ""}

	result := applyAutoStatus(node, outcome)

	if result.Status != StatusSuccess {
		t.Errorf("expected status %q, got %q", StatusSuccess, result.Status)
	}
	if result.Notes == "" {
		t.Error("expected Notes to contain auto_status message, got empty string")
	}
}

func TestAutoStatusDoesNotOverrideExplicitStatus(t *testing.T) {
	node := &Node{
		ID:    "n2",
		Attrs: map[string]string{"auto_status": "true"},
	}
	outcome := &Outcome{Status: StatusFail, FailureReason: "something broke"}

	result := applyAutoStatus(node, outcome)

	if result.Status != StatusFail {
		t.Errorf("expected status %q, got %q", StatusFail, result.Status)
	}
	if result.FailureReason != "something broke" {
		t.Errorf("expected FailureReason preserved, got %q", result.FailureReason)
	}
}

func TestAutoStatusFalseDoesNothing(t *testing.T) {
	node := &Node{
		ID:    "n3",
		Attrs: map[string]string{"auto_status": "false"},
	}
	outcome := &Outcome{Status: ""}

	result := applyAutoStatus(node, outcome)

	if result.Status != "" {
		t.Errorf("expected empty status, got %q", result.Status)
	}
}

func TestAutoStatusMissingAttrDoesNothing(t *testing.T) {
	node := &Node{
		ID:    "n4",
		Attrs: map[string]string{"label": "some node"},
	}
	outcome := &Outcome{Status: ""}

	result := applyAutoStatus(node, outcome)

	if result.Status != "" {
		t.Errorf("expected empty status, got %q", result.Status)
	}
}
