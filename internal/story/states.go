package story

import "slices"

type Type string

const (
	TypeFeature Type = "feature"
	TypeBug     Type = "bug"
	TypeChore   Type = "chore"
)

func (t Type) Valid() bool { return t == TypeFeature || t == TypeBug || t == TypeChore }

type State string

const (
	StateIcebox    State = "icebox"
	StateBacklog   State = "backlog"
	StateUnstarted State = "unstarted"
	StateStarted   State = "started"
	StateFinished  State = "finished"
	StateDelivered State = "delivered"
	StateAccepted  State = "accepted"
	StateRejected  State = "rejected"
)

// Section is a panel on the board and, for the first three, an independent
// ordering scope.
type Section string

const (
	SectionIcebox  Section = "icebox"
	SectionBacklog Section = "backlog"
	SectionCurrent Section = "current"
	// SectionDone holds stories accepted before the current iteration. It is
	// read-only history, never a move target.
	SectionDone Section = "done"
)

// transitions is the whole workflow. accepted is terminal.
var transitions = map[State][]State{
	StateIcebox:    {StateBacklog, StateUnstarted, StateStarted},
	StateBacklog:   {StateIcebox, StateUnstarted, StateStarted},
	StateUnstarted: {StateIcebox, StateBacklog, StateStarted},
	StateStarted:   {StateFinished, StateUnstarted},
	StateFinished:  {StateDelivered, StateStarted},
	StateDelivered: {StateAccepted, StateRejected},
	StateRejected:  {StateStarted},
	StateAccepted:  {},
}

func (s State) Valid() bool { _, ok := transitions[s]; return ok }

// CanTransition reports whether the workflow allows from → to.
func CanTransition(from, to State) bool { return slices.Contains(transitions[from], to) }

// SectionOf maps a state to its ordering scope. Accepted stories are frozen:
// they keep their last position but no longer take part in ordering.
func SectionOf(s State) Section {
	switch s {
	case StateIcebox:
		return SectionIcebox
	case StateBacklog:
		return SectionBacklog
	default:
		return SectionCurrent
	}
}

// orderedStates lists the states that share one ordering scope.
func orderedStates(sec Section) []string {
	switch sec {
	case SectionIcebox:
		return []string{string(StateIcebox)}
	case SectionBacklog:
		return []string{string(StateBacklog)}
	case SectionCurrent:
		return []string{string(StateUnstarted), string(StateStarted), string(StateFinished), string(StateDelivered), string(StateRejected)}
	}
	return nil
}

// entryState is the state a story takes when it is dragged into a section.
func entryState(sec Section) State {
	switch sec {
	case SectionIcebox:
		return StateIcebox
	case SectionBacklog:
		return StateBacklog
	default:
		return StateUnstarted
	}
}

// inProgress states pin a story to the current iteration.
func inProgress(s State) bool {
	return s == StateStarted || s == StateFinished || s == StateDelivered || s == StateRejected
}

// Estimates is the point scale.
var Estimates = []int64{0, 1, 2, 3, 5, 8}

func validEstimate(v int64) bool { return slices.Contains(Estimates, v) }
