package diagram

// NodeKind classifies a diagram node by its workflow step type.
type NodeKind string

const (
	NodeKindAction    NodeKind = "action"
	NodeKindCondition NodeKind = "condition"
	NodeKindReasoning NodeKind = "reasoning"
	NodeKindParallel  NodeKind = "parallel"
	NodeKindLoop      NodeKind = "loop"
	NodeKindWait      NodeKind = "wait"
	NodeKindStart     NodeKind = "start"
	NodeKindEnd       NodeKind = "end"
)

// DiagramModel is the intermediate representation used by all renderers.
type DiagramModel struct {
	Title  string
	Nodes  []*Node
	Edges  []Edge
	Levels [][]string
}

// Node represents a single step in the diagram.
type Node struct {
	ID       string
	Label    string
	Kind     NodeKind
	Status   *StatusOverlay
	Children []*SubGraph // condition branches, parallel branches, loop body
}

// SubGraph holds nested steps for flow control nodes (condition, parallel, loop).
type SubGraph struct {
	Label string
	Nodes []*Node
	Edges []Edge
}

// StatusOverlay carries runtime state for a node.
type StatusOverlay struct {
	Status     string // from schema.StepStatus
	DurationMs int64
	RetryCount int
	Error      string
}

// Edge represents a dependency between two nodes.
type Edge struct {
	From  string
	To    string
	Label string
}
