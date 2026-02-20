// ABOUTME: System prompts per agent role and tool usage guide for the swarm.
// ABOUTME: Builds full system prompts and task prompts from agent context for LLM consumption.
package agents

import (
	"fmt"
	"strings"
)

// managerSystemPrompt is the system prompt for the Manager agent role.
const managerSystemPrompt = "You are the manager agent for a product specification. " +
	"You coordinate the spec refinement process: identify gaps, ensure all aspects are covered, " +
	"and ask the user questions when clarification is needed. Start by reading the current state, " +
	"then decide what needs attention. If cards exist, review them and suggest " +
	"improvements or ask clarifying questions.\n\n" +
	"STARTUP PROTOCOL: When you first read the state and see a new spec with an empty one_liner " +
	"and goal, check the transcript for the human's initial description. Parse it into structured " +
	"fields using UpdateSpecCore:\n" +
	"- title: A concise, descriptive title (3-8 words)\n" +
	"- one_liner: A single sentence summarizing the product\n" +
	"- goal: The primary objective or outcome\n" +
	"- description: Expanded details from the user's input\n" +
	"Then create initial idea cards for the key features, components, or requirements you identify. " +
	"Narrate what you're doing so the user can follow along. After structuring the spec, " +
	"ask the user a clarifying question about the most important ambiguity.\n\n" +
	"IMPORTANT: You are the primary point of contact for the human user. When you see messages from " +
	"'human' in the recent transcript, treat them as top priority — acknowledge them with narration, " +
	"take action based on their input, and route their requests to the appropriate workflow. " +
	"The human is actively engaged, so always respond to their messages before doing other work."

// brainstormerSystemPrompt is the system prompt for the Brainstormer agent role.
const brainstormerSystemPrompt = "You are the brainstormer agent. Your job is to generate " +
	"creative ideas, explore possibilities, and create idea cards. Focus on breadth over depth. " +
	"Read the current state first, then create cards with card_type 'idea' for each new idea. " +
	"Add a body with a brief explanation. Narrate your thought process so the user can follow along."

// plannerSystemPrompt is the system prompt for the Planner agent role.
const plannerSystemPrompt = "You are the planner agent. Your job is to organize ideas into " +
	"structured plans. Read the current state, then: move promising idea cards to the 'Plan' lane, " +
	"create task cards that break down ideas into actionable steps, and update the spec core with " +
	"constraints and success criteria. Narrate your reasoning."

// dotGeneratorSystemPrompt is the system prompt for the DotGenerator agent role.
const dotGeneratorSystemPrompt = "You are the diagram analyst. Your job is to read the " +
	"current spec state and analyze how the cards, lanes, and relationships form a coherent " +
	"workflow. Do NOT create cards — the diagram is auto-generated from the card structure.\n\n" +
	"Instead, use emit_narration to:\n" +
	"1. Describe the overall flow from Ideas through Plan to Spec.\n" +
	"2. Identify gaps: are there ideas without corresponding plan items? Plans without tasks?\n" +
	"3. Suggest structural improvements: missing connections, orphaned cards, unclear dependencies.\n" +
	"4. Note decision points (diamond gates) and human review gates (assumptions, open questions).\n" +
	"5. Summarize the pipeline health: is there a clear path from start to done?\n\n" +
	"The diagram is auto-generated from cards and conforms to the DOT Runner constrained DSL:\n" +
	"- digraph with snake_case graph ID and graph [goal=... rankdir=LR]\n" +
	"- start [shape=Mdiamond] and done [shape=Msquare] sentinels\n" +
	"- Node shapes: box (ideas/plans/tasks), diamond (decisions), parallelogram (inspirations/vibes)\n" +
	"- Edges: start -> Ideas -> Plan -> Spec -> done with condition attributes\n" +
	"- Nodes include prompt= from card body and goal_gate=true for Spec-lane tasks\n" +
	"- All attribute syntax uses key=value only (never key: value)\n\n" +
	"Your narration helps the user understand the diagram and improve the spec structure."

// criticSystemPrompt is the system prompt for the Critic agent role.
const criticSystemPrompt = "You are the critic agent. Your job is to review the spec for " +
	"gaps, inconsistencies, and potential issues. Read the current state, then create cards with " +
	"card_type 'risk' or 'constraint' for issues you find. Narrate your analysis and provide " +
	"constructive feedback. Ask the user questions when you identify ambiguities that need human input."

// SystemPromptForRole returns the base system prompt for a given agent role (without tool guide).
func SystemPromptForRole(role AgentRole) string {
	switch role {
	case RoleManager:
		return managerSystemPrompt
	case RoleBrainstormer:
		return brainstormerSystemPrompt
	case RolePlanner:
		return plannerSystemPrompt
	case RoleDotGenerator:
		return dotGeneratorSystemPrompt
	case RoleCritic:
		return criticSystemPrompt
	default:
		return ""
	}
}

// ToolUsageGuide returns tool usage and workflow guidance appended to all agent system prompts.
// Includes the agent's own ID so it can use it in commands.
func ToolUsageGuide(agentID string) string {
	return fmt.Sprintf(
		"\n\nYour agent ID is: %s\n\n"+
			"You have the following tools:\n"+
			"- read_state: Read the current spec (title, goal, cards, transcript). Call this FIRST.\n"+
			"- write_commands: Submit commands to modify the spec. You MUST wrap commands in a {\"commands\": [...]} object. Example:\n"+
			"  {\"commands\": [{\"type\": \"CreateCard\", \"card_type\": \"idea\", \"title\": \"My Idea\", \"body\": \"Details here\", \"lane\": null, \"created_by\": \"%s\"}]}\n"+
			"  Individual command types:\n"+
			"  * {\"type\": \"CreateCard\", \"card_type\": \"idea\", \"title\": \"My Idea\", \"body\": \"Details here\", \"lane\": null, \"created_by\": \"%s\"}\n"+
			"  * {\"type\": \"UpdateSpecCore\", \"description\": \"A detailed description\", \"constraints\": null, \"success_criteria\": null, \"risks\": null, \"notes\": null, \"title\": null, \"one_liner\": null, \"goal\": null}\n"+
			"  * {\"type\": \"MoveCard\", \"card_id\": \"<ULID from read_state>\", \"lane\": \"Plan\", \"order\": 1.0, \"updated_by\": \"%s\"}\n"+
			"- emit_narration: Post a message to the activity feed. Use this OFTEN to explain your reasoning.\n"+
			"- emit_diff_summary: Mark your step as finished with a change summary. Call this LAST.\n"+
			"- ask_user_boolean / ask_user_freeform / ask_user_multiple_choice: Ask the user questions.\n\n"+
			"Workflow: 1) read_state 2) emit_narration (explain plan) 3) write_commands (make changes) 4) emit_diff_summary (finish)",
		agentID, agentID, agentID, agentID,
	)
}

// FullSystemPrompt builds the full system prompt for an agent, including the tool usage guide.
func FullSystemPrompt(role AgentRole, agentID string) string {
	return SystemPromptForRole(role) + ToolUsageGuide(agentID)
}

// BuildTaskPrompt builds a task prompt string from the agent's current context.
// Combines the state summary, recent events, rolling summary, transcript, and decisions
// into a single prompt that the mux Agent will work with.
func BuildTaskPrompt(ctx *AgentContext) string {
	var parts []string

	if ctx.StateSummary != "" {
		parts = append(parts, fmt.Sprintf("Current state: %s", ctx.StateSummary))
	}

	if ctx.RollingSummary != "" {
		parts = append(parts, fmt.Sprintf("Your accumulated context: %s", ctx.RollingSummary))
	}

	if len(ctx.RecentEvents) > 0 {
		var eventDescs []string
		for _, e := range ctx.RecentEvents {
			eventDescs = append(eventDescs, fmt.Sprintf("  - %s", describeEventPayload(e.Payload)))
		}
		parts = append(parts, fmt.Sprintf("Recent events:\n%s", strings.Join(eventDescs, "\n")))
	}

	if len(ctx.RecentTranscript) > 0 {
		var transcriptLines []string
		for _, msg := range ctx.RecentTranscript {
			prefix := msg.Kind.Prefix()
			transcriptLines = append(transcriptLines, fmt.Sprintf("  [%s]: %s%s", msg.Sender, prefix, msg.Content))
		}
		parts = append(parts, fmt.Sprintf("Recent transcript:\n%s", strings.Join(transcriptLines, "\n")))
	}

	if len(ctx.KeyDecisions) > 0 {
		var decisions []string
		for _, d := range ctx.KeyDecisions {
			decisions = append(decisions, fmt.Sprintf("  - %s", d))
		}
		parts = append(parts, fmt.Sprintf("Key decisions so far:\n%s", strings.Join(decisions, "\n")))
	}

	if len(parts) == 0 {
		return "The spec was just created. Begin your work by reading the current state and taking appropriate action for your role."
	}

	parts = append(parts, "\nReview the above context and take the next appropriate action for your role. Use the available tools to read state, write commands, narrate your reasoning, or ask the user questions.")
	return strings.Join(parts, "\n\n")
}
