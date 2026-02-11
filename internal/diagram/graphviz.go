package diagram

import (
	"bytes"
	"context"
	"fmt"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// RenderImage renders a DiagramModel as a PNG image using graphviz.
// Returns the PNG bytes.
func RenderImage(model *DiagramModel) ([]byte, error) {
	ctx := context.Background()

	gv, err := graphviz.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("diagram: create graphviz: %w", err)
	}
	defer gv.Close()

	gv.SetLayout(graphviz.DOT)

	graph, err := gv.Graph()
	if err != nil {
		return nil, fmt.Errorf("diagram: create graph: %w", err)
	}
	defer graph.Close()

	graph.SetRankDir(cgraph.TBRank)
	if model.Title != "" {
		graph.SetLabel(model.Title)
	}

	// Create nodes.
	gvNodes := make(map[string]*cgraph.Node, len(model.Nodes))
	for _, node := range model.Nodes {
		gvNode, nErr := graph.CreateNodeByName(node.ID)
		if nErr != nil {
			return nil, fmt.Errorf("diagram: create node %s: %w", node.ID, nErr)
		}
		gvNode.SetLabel(firstLine(node.Label))
		applyNodeStyle(gvNode, node)
		gvNodes[node.ID] = gvNode
	}

	// Create subgraph clusters for children.
	for _, node := range model.Nodes {
		for _, sg := range node.Children {
			clusterName := "cluster_" + node.ID + "_" + sg.Label
			sub, subErr := graph.CreateSubGraphByName(clusterName)
			if subErr != nil {
				continue
			}
			sub.SetLabel(sg.Label)
			sub.SetStyle(cgraph.DashedGraphStyle)

			for _, subNode := range sg.Nodes {
				gvSub, nErr := sub.CreateNodeByName(subNode.ID)
				if nErr != nil {
					continue
				}
				gvSub.SetLabel(firstLine(subNode.Label))
				applyNodeStyle(gvSub, subNode)
				gvNodes[subNode.ID] = gvSub
			}
			for _, edge := range sg.Edges {
				fromGV, toGV := gvNodes[edge.From], gvNodes[edge.To]
				if fromGV != nil && toGV != nil {
					e, eErr := graph.CreateEdgeByName("", fromGV, toGV)
					if eErr == nil && edge.Label != "" {
						e.SetLabel(edge.Label)
					}
				}
			}
		}
	}

	// Create edges.
	for _, edge := range model.Edges {
		fromGV, toGV := gvNodes[edge.From], gvNodes[edge.To]
		if fromGV != nil && toGV != nil {
			e, eErr := graph.CreateEdgeByName("", fromGV, toGV)
			if eErr == nil && edge.Label != "" {
				e.SetLabel(edge.Label)
			}
		}
	}

	// Render to PNG.
	var buf bytes.Buffer
	if err := gv.Render(ctx, graph, graphviz.PNG, &buf); err != nil {
		return nil, fmt.Errorf("diagram: render PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// applyNodeStyle sets graphviz attributes based on node kind and status.
func applyNodeStyle(gvNode *cgraph.Node, node *Node) {
	// Shape by kind.
	switch node.Kind {
	case NodeKindAction:
		gvNode.SetShape(cgraph.BoxShape)
	case NodeKindCondition:
		gvNode.SetShape(cgraph.DiamondShape)
	case NodeKindReasoning:
		gvNode.SetShape(cgraph.HexagonShape)
	case NodeKindWait:
		gvNode.SetShape(cgraph.EllipseShape)
	case NodeKindParallel, NodeKindLoop:
		gvNode.SetShape(cgraph.BoxShape) // no record shape in go-graphviz v0.2; box is sufficient
	case NodeKindStart, NodeKindEnd:
		gvNode.SetShape(cgraph.CircleShape)
		gvNode.SetWidth(0.5)
		gvNode.SetHeight(0.5)
	}

	// Color by status.
	if node.Status != nil {
		applyStatusColor(gvNode, node.Status.Status)
	}
}

// applyStatusColor sets fill color and style based on status.
func applyStatusColor(gvNode *cgraph.Node, status string) {
	gvNode.SetStyle(cgraph.FilledNodeStyle)
	switch status {
	case "completed":
		gvNode.SetFillColor("#2d6a2d")
		gvNode.SetFontColor("white")
	case "failed":
		gvNode.SetFillColor("#8b1a1a")
		gvNode.SetFontColor("white")
	case "running":
		gvNode.SetFillColor("#1a5276")
		gvNode.SetFontColor("white")
	case "suspended":
		gvNode.SetFillColor("#b7791a")
		gvNode.SetFontColor("white")
	case "pending", "scheduled":
		gvNode.SetFillColor("#d3d3d3")
		gvNode.SetFontColor("black")
	case "skipped":
		gvNode.SetFillColor("#e8e8e8")
		gvNode.SetFontColor("#888888")
		gvNode.SetStyle(cgraph.DashedNodeStyle)
	}
}
