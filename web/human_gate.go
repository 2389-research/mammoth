// ABOUTME: Creates ChannelInterviewer instances for web build execution.
// ABOUTME: The interviewer bridges pipeline human gates to SSE events for the browser.
package web

// newBuildInterviewer creates a ChannelInterviewer wired to the given
// build run's SSE broadcast function.
func newBuildInterviewer(broadcast func(BuildEvent)) *ChannelInterviewer {
	return NewChannelInterviewer(broadcast)
}
