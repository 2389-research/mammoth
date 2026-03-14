// ABOUTME: Creates ChannelInterviewer instances for web build execution.
// ABOUTME: The interviewer bridges pipeline human gates to SSE events for the browser.
package web

import "context"

// newBuildInterviewer creates a ChannelInterviewer wired to the given
// build run's SSE broadcast function. The context is used for cancellation.
func newBuildInterviewer(ctx context.Context, broadcast func(BuildEvent)) *ChannelInterviewer {
	return NewChannelInterviewer(ctx, broadcast)
}
