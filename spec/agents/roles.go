// ABOUTME: AgentRole type identifies the functional role an agent plays within the swarm.
// ABOUTME: Four variants (Manager, Brainstormer, Planner, Critic) with label methods.
package agents

// AgentRole identifies the functional role an agent plays within the swarm.
type AgentRole int

const (
	RoleManager AgentRole = iota
	RoleBrainstormer
	RolePlanner
	RoleCritic
)

// Label returns a human-readable lowercase label for this role.
func (r AgentRole) Label() string {
	switch r {
	case RoleManager:
		return "manager"
	case RoleBrainstormer:
		return "brainstormer"
	case RolePlanner:
		return "planner"
	case RoleCritic:
		return "critic"
	default:
		return "unknown"
	}
}

// String implements fmt.Stringer.
func (r AgentRole) String() string {
	return r.Label()
}
