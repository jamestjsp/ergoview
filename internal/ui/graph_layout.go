package ui

import (
	"math"
	"sort"

	"github.com/jamestjsp/ergoview/internal/ergo"
)

type graphScope uint8

const (
	graphScopeDirect graphScope = iota
	graphScopeAdaptive
	graphScopeLineage
)

type graphOrientation uint8

const (
	graphHorizontal graphOrientation = iota
	graphVertical
)

type graphNodeKind uint8

const (
	graphTaskNode graphNodeKind = iota
	graphUpstreamOverflow
	graphDownstreamOverflow
)

const (
	upstreamOverflowID   = "\x00upstream"
	downstreamOverflowID = "\x00downstream"
)

type graphRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

func (r graphRect) contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

func (r graphRect) center() graphPoint {
	return graphPoint{X: r.X + r.Width/2, Y: r.Y + r.Height/2}
}

type graphPoint struct {
	X int
	Y int
}

type dependencyGraphNode struct {
	ID        string
	Task      ergo.Task
	Kind      graphNodeKind
	HiddenIDs []string
	Rank      int
	Rect      graphRect
}

type dependencyGraphEdge struct {
	From string
	To   string
}

type dependencyGraphLayout struct {
	FocusID     string
	Scope       graphScope
	Orientation graphOrientation
	NodeWidth   int
	NodeHeight  int
	Width       int
	Height      int
	Nodes       []dependencyGraphNode
	Edges       []dependencyGraphEdge
}

func (l dependencyGraphLayout) node(id string) (dependencyGraphNode, bool) {
	for _, node := range l.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return dependencyGraphNode{}, false
}

type dependencyGraphRequest struct {
	Snapshot ergo.Snapshot
	FocusID  string
	Scope    graphScope
	Width    int
	Height   int
}

type graphTopology struct {
	tasks    map[string]ergo.Task
	order    map[string]int
	forward  map[string][]string
	backward map[string][]string
}

func buildDependencyGraphLayout(request dependencyGraphRequest) dependencyGraphLayout {
	topology := newGraphTopology(request.Snapshot)
	if _, ok := topology.tasks[request.FocusID]; !ok {
		return dependencyGraphLayout{
			FocusID: request.FocusID,
			Scope:   request.Scope,
			Width:   max(1, request.Width),
			Height:  max(1, request.Height),
		}
	}

	lineage := topology.lineage(request.FocusID)
	orientation := chooseGraphOrientation(lineage, topology, request.Width, request.Height)
	nodeWidth := graphNodeWidth(request.Width)
	nodeHeight := 3
	included := topology.project(request.FocusID, request.Scope, lineage, orientation, nodeWidth, nodeHeight, request.Width, request.Height)
	nodes, edges := topology.visibleGraph(included)
	nodes, edges = topology.addOverflowNodes(nodes, edges, request.FocusID, included)

	ranks := rankDependencyGraph(nodes, edges, topology.order)
	layers := orderDependencyLayers(ranks, edges, topology.order)
	positioned, width, height := positionDependencyNodes(nodes, layers, orientation, nodeWidth, nodeHeight, request.Width, request.Height)

	return dependencyGraphLayout{
		FocusID:     request.FocusID,
		Scope:       request.Scope,
		Orientation: orientation,
		NodeWidth:   nodeWidth,
		NodeHeight:  nodeHeight,
		Width:       width,
		Height:      height,
		Nodes:       positioned,
		Edges:       edges,
	}
}

func newGraphTopology(snapshot ergo.Snapshot) graphTopology {
	tasks := snapshot.AllTasks()
	topology := graphTopology{
		tasks:    make(map[string]ergo.Task, len(tasks)),
		order:    make(map[string]int, len(tasks)+2),
		forward:  make(map[string][]string, len(tasks)),
		backward: make(map[string][]string, len(tasks)),
	}
	for index, task := range tasks {
		topology.tasks[task.ID] = task
		topology.order[task.ID] = index
	}
	topology.order[upstreamOverflowID] = -2
	topology.order[downstreamOverflowID] = len(tasks) + 1
	for _, task := range tasks {
		for _, dependencyID := range task.Dependencies {
			if _, ok := topology.tasks[dependencyID]; !ok {
				continue
			}
			topology.forward[dependencyID] = append(topology.forward[dependencyID], task.ID)
			topology.backward[task.ID] = append(topology.backward[task.ID], dependencyID)
		}
	}
	for id := range topology.tasks {
		sortGraphIDs(topology.forward[id], topology.order)
		sortGraphIDs(topology.backward[id], topology.order)
	}
	return topology
}

func (t graphTopology) lineage(focusID string) map[string]bool {
	visible := map[string]bool{focusID: true}
	queue := []string{focusID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		neighbors := append([]string(nil), t.backward[current]...)
		neighbors = append(neighbors, t.forward[current]...)
		sortGraphIDs(neighbors, t.order)
		for _, neighbor := range neighbors {
			if visible[neighbor] {
				continue
			}
			visible[neighbor] = true
			queue = append(queue, neighbor)
		}
	}
	return visible
}

func (t graphTopology) project(
	focusID string,
	scope graphScope,
	lineage map[string]bool,
	orientation graphOrientation,
	nodeWidth int,
	nodeHeight int,
	width int,
	height int,
) map[string]bool {
	if scope == graphScopeLineage {
		return cloneIDSet(lineage)
	}
	if scope == graphScopeDirect {
		visible := map[string]bool{focusID: true}
		for _, id := range t.backward[focusID] {
			visible[id] = true
		}
		for _, id := range t.forward[focusID] {
			visible[id] = true
		}
		return visible
	}

	visible := map[string]bool{focusID: true}
	for _, candidate := range t.breadthFirst(focusID) {
		if !t.touchesVisible(candidate, visible) {
			continue
		}
		trial := cloneIDSet(visible)
		trial[candidate] = true
		nodes, edges := t.visibleGraph(trial)
		ranks := rankDependencyGraph(nodes, edges, t.order)
		if graphRanksFit(ranks, orientation, nodeWidth, nodeHeight, width, height) {
			visible = trial
		}
	}
	return visible
}

func (t graphTopology) breadthFirst(focusID string) []string {
	visited := map[string]bool{focusID: true}
	queue := []string{focusID}
	var result []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		neighbors := append([]string(nil), t.backward[current]...)
		neighbors = append(neighbors, t.forward[current]...)
		sortGraphIDs(neighbors, t.order)
		for _, neighbor := range neighbors {
			if visited[neighbor] {
				continue
			}
			visited[neighbor] = true
			queue = append(queue, neighbor)
			result = append(result, neighbor)
		}
	}
	return result
}

func (t graphTopology) touchesVisible(id string, visible map[string]bool) bool {
	for _, neighbor := range t.backward[id] {
		if visible[neighbor] {
			return true
		}
	}
	for _, neighbor := range t.forward[id] {
		if visible[neighbor] {
			return true
		}
	}
	return false
}

func (t graphTopology) visibleGraph(visible map[string]bool) ([]dependencyGraphNode, []dependencyGraphEdge) {
	ids := make([]string, 0, len(visible))
	for id := range visible {
		if _, ok := t.tasks[id]; ok {
			ids = append(ids, id)
		}
	}
	sortGraphIDs(ids, t.order)
	nodes := make([]dependencyGraphNode, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, dependencyGraphNode{ID: id, Task: t.tasks[id], Kind: graphTaskNode})
	}
	var edges []dependencyGraphEdge
	for _, from := range ids {
		for _, to := range t.forward[from] {
			if visible[to] {
				edges = append(edges, dependencyGraphEdge{From: from, To: to})
			}
		}
	}
	sortGraphEdges(edges, t.order)
	return nodes, edges
}

func (t graphTopology) addOverflowNodes(
	nodes []dependencyGraphNode,
	edges []dependencyGraphEdge,
	focusID string,
	visible map[string]bool,
) ([]dependencyGraphNode, []dependencyGraphEdge) {
	var upstream []string
	var downstream []string
	upstreamTargets := make(map[string]bool)
	downstreamSources := make(map[string]bool)
	for id := range t.reachable(focusID, t.backward) {
		if visible[id] {
			continue
		}
		upstream = append(upstream, id)
		for _, to := range t.forward[id] {
			if visible[to] {
				upstreamTargets[to] = true
			}
		}
	}
	for id := range t.reachable(focusID, t.forward) {
		if visible[id] {
			continue
		}
		downstream = append(downstream, id)
		for _, from := range t.backward[id] {
			if visible[from] {
				downstreamSources[from] = true
			}
		}
	}
	upstream = uniqueGraphIDs(upstream, t.order)
	downstream = uniqueGraphIDs(downstream, t.order)
	if len(upstream) > 0 {
		nodes = append(nodes, dependencyGraphNode{
			ID:        upstreamOverflowID,
			Kind:      graphUpstreamOverflow,
			HiddenIDs: upstream,
		})
		targets := idSetKeys(upstreamTargets, t.order)
		for _, target := range targets {
			edges = append(edges, dependencyGraphEdge{From: upstreamOverflowID, To: target})
		}
	}
	if len(downstream) > 0 {
		nodes = append(nodes, dependencyGraphNode{
			ID:        downstreamOverflowID,
			Kind:      graphDownstreamOverflow,
			HiddenIDs: downstream,
		})
		sources := idSetKeys(downstreamSources, t.order)
		for _, source := range sources {
			edges = append(edges, dependencyGraphEdge{From: source, To: downstreamOverflowID})
		}
	}
	sort.SliceStable(nodes, func(left, right int) bool {
		return graphIDLess(nodes[left].ID, nodes[right].ID, t.order)
	})
	sortGraphEdges(edges, t.order)
	return nodes, edges
}

func (t graphTopology) reachable(focusID string, adjacency map[string][]string) map[string]bool {
	seen := make(map[string]bool)
	queue := append([]string(nil), adjacency[focusID]...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if seen[current] {
			continue
		}
		seen[current] = true
		queue = append(queue, adjacency[current]...)
	}
	return seen
}

func chooseGraphOrientation(
	visible map[string]bool,
	topology graphTopology,
	width int,
	height int,
) graphOrientation {
	nodes, edges := topology.visibleGraph(visible)
	ranks := rankDependencyGraph(nodes, edges, topology.order)
	layerCount, widestLayer := graphRankShape(ranks)
	nodeWidth := graphNodeWidth(width)
	horizontalWidth := layerCount*nodeWidth + max(0, layerCount-1)*5
	horizontalHeight := widestLayer*3 + max(0, widestLayer-1)
	verticalWidth := widestLayer*nodeWidth + max(0, widestLayer-1)*2
	verticalHeight := layerCount*3 + max(0, layerCount-1)*2
	horizontalScore := graphFitScore(horizontalWidth, horizontalHeight, width, height)
	verticalScore := graphFitScore(verticalWidth, verticalHeight, width, height)
	if horizontalScore == verticalScore {
		if width >= height*3 {
			return graphHorizontal
		}
		return graphVertical
	}
	if horizontalScore > verticalScore {
		return graphHorizontal
	}
	return graphVertical
}

func graphFitScore(neededWidth, neededHeight, width, height int) float64 {
	width = max(1, width)
	height = max(1, height)
	widthRatio := math.Min(1, float64(width)/float64(max(1, neededWidth)))
	heightRatio := math.Min(1, float64(height)/float64(max(1, neededHeight)))
	return widthRatio * heightRatio
}

func graphNodeWidth(width int) int {
	return min(26, max(14, width/4))
}

func graphRanksFit(
	ranks map[string]int,
	orientation graphOrientation,
	nodeWidth int,
	nodeHeight int,
	width int,
	height int,
) bool {
	layerCount, widestLayer := graphRankShape(ranks)
	if orientation == graphHorizontal {
		neededWidth := layerCount*nodeWidth + max(0, layerCount-1)*5
		neededHeight := widestLayer*nodeHeight + max(0, widestLayer-1)
		return neededWidth <= max(1, width) && neededHeight <= max(1, height)
	}
	neededWidth := widestLayer*nodeWidth + max(0, widestLayer-1)*2
	neededHeight := layerCount*nodeHeight + max(0, layerCount-1)*2
	return neededWidth <= max(1, width) && neededHeight <= max(1, height)
}

func rankDependencyGraph(
	nodes []dependencyGraphNode,
	edges []dependencyGraphEdge,
	order map[string]int,
) map[string]int {
	ranks := make(map[string]int, len(nodes))
	indegree := make(map[string]int, len(nodes))
	forward := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		indegree[node.ID] = 0
	}
	for _, edge := range edges {
		if _, fromOK := indegree[edge.From]; !fromOK {
			continue
		}
		if _, toOK := indegree[edge.To]; toOK {
			indegree[edge.To]++
			forward[edge.From] = append(forward[edge.From], edge.To)
		}
	}
	for id := range forward {
		sortGraphIDs(forward[id], order)
	}
	var ready []string
	for id, degree := range indegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}
	sortGraphIDs(ready, order)
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]
		for _, successor := range forward[current] {
			if ranks[successor] < ranks[current]+1 {
				ranks[successor] = ranks[current] + 1
			}
			indegree[successor]--
			if indegree[successor] == 0 {
				ready = append(ready, successor)
				sortGraphIDs(ready, order)
			}
		}
	}
	for _, node := range nodes {
		if _, ok := ranks[node.ID]; !ok {
			ranks[node.ID] = 0
		}
	}
	return ranks
}

func orderDependencyLayers(
	ranks map[string]int,
	edges []dependencyGraphEdge,
	order map[string]int,
) [][]string {
	layerCount, _ := graphRankShape(ranks)
	layers := make([][]string, layerCount)
	for id, rank := range ranks {
		layers[rank] = append(layers[rank], id)
	}
	for index := range layers {
		sortGraphIDs(layers[index], order)
	}

	for range 4 {
		positions := graphLayerPositions(layers)
		for rank := 1; rank < len(layers); rank++ {
			sortLayerByBarycenter(layers[rank], incomingGraphNeighbors(edges), positions, order)
			positions = graphLayerPositions(layers)
		}
		for rank := len(layers) - 2; rank >= 0; rank-- {
			sortLayerByBarycenter(layers[rank], outgoingGraphNeighbors(edges), positions, order)
			positions = graphLayerPositions(layers)
		}
	}
	return layers
}

func sortLayerByBarycenter(
	layer []string,
	neighbors map[string][]string,
	positions map[string]int,
	order map[string]int,
) {
	type weightedID struct {
		id         string
		barycenter float64
		connected  bool
		position   int
	}
	weighted := make([]weightedID, 0, len(layer))
	for position, id := range layer {
		var sum int
		var count int
		for _, neighbor := range neighbors[id] {
			if neighborPosition, ok := positions[neighbor]; ok {
				sum += neighborPosition
				count++
			}
		}
		item := weightedID{id: id, position: position}
		if count > 0 {
			item.connected = true
			item.barycenter = float64(sum) / float64(count)
		}
		weighted = append(weighted, item)
	}
	sort.SliceStable(weighted, func(left, right int) bool {
		a, b := weighted[left], weighted[right]
		if a.connected && b.connected && a.barycenter != b.barycenter {
			return a.barycenter < b.barycenter
		}
		if a.connected != b.connected {
			return a.connected
		}
		if a.position != b.position {
			return a.position < b.position
		}
		return graphIDLess(a.id, b.id, order)
	})
	for index := range layer {
		layer[index] = weighted[index].id
	}
}

func positionDependencyNodes(
	nodes []dependencyGraphNode,
	layers [][]string,
	orientation graphOrientation,
	nodeWidth int,
	nodeHeight int,
	availableWidth int,
	availableHeight int,
) ([]dependencyGraphNode, int, int) {
	const horizontalRankGap = 5
	const verticalRankGap = 2
	const horizontalNodeGap = 1
	const verticalNodeGap = 2

	byID := make(map[string]int, len(nodes))
	for index, node := range nodes {
		byID[node.ID] = index
	}
	canvasWidth := max(1, availableWidth)
	canvasHeight := max(1, availableHeight)
	if orientation == graphHorizontal {
		canvasWidth = max(canvasWidth, len(layers)*nodeWidth+max(0, len(layers)-1)*horizontalRankGap)
		for rank, layer := range layers {
			layerHeight := len(layer)*nodeHeight + max(0, len(layer)-1)*horizontalNodeGap
			canvasHeight = max(canvasHeight, layerHeight)
			y := max(0, (canvasHeight-layerHeight)/2)
			for _, id := range layer {
				index := byID[id]
				nodes[index].Rank = rank
				nodes[index].Rect = graphRect{
					X:      rank * (nodeWidth + horizontalRankGap),
					Y:      y,
					Width:  nodeWidth,
					Height: nodeHeight,
				}
				y += nodeHeight + horizontalNodeGap
			}
		}
		return nodes, canvasWidth, canvasHeight
	}

	canvasHeight = max(canvasHeight, len(layers)*nodeHeight+max(0, len(layers)-1)*verticalRankGap)
	for rank, layer := range layers {
		layerWidth := len(layer)*nodeWidth + max(0, len(layer)-1)*verticalNodeGap
		canvasWidth = max(canvasWidth, layerWidth)
		x := max(0, (canvasWidth-layerWidth)/2)
		for _, id := range layer {
			index := byID[id]
			nodes[index].Rank = rank
			nodes[index].Rect = graphRect{
				X:      x,
				Y:      rank * (nodeHeight + verticalRankGap),
				Width:  nodeWidth,
				Height: nodeHeight,
			}
			x += nodeWidth + verticalNodeGap
		}
	}
	return nodes, canvasWidth, canvasHeight
}

func graphRankShape(ranks map[string]int) (int, int) {
	if len(ranks) == 0 {
		return 0, 0
	}
	counts := make(map[int]int)
	maxRank := 0
	widest := 0
	for _, rank := range ranks {
		counts[rank]++
		maxRank = max(maxRank, rank)
		widest = max(widest, counts[rank])
	}
	return maxRank + 1, widest
}

func graphLayerPositions(layers [][]string) map[string]int {
	positions := make(map[string]int)
	for _, layer := range layers {
		for position, id := range layer {
			positions[id] = position
		}
	}
	return positions
}

func incomingGraphNeighbors(edges []dependencyGraphEdge) map[string][]string {
	neighbors := make(map[string][]string)
	for _, edge := range edges {
		neighbors[edge.To] = append(neighbors[edge.To], edge.From)
	}
	return neighbors
}

func outgoingGraphNeighbors(edges []dependencyGraphEdge) map[string][]string {
	neighbors := make(map[string][]string)
	for _, edge := range edges {
		neighbors[edge.From] = append(neighbors[edge.From], edge.To)
	}
	return neighbors
}

func cloneIDSet(source map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(source))
	for id, visible := range source {
		if visible {
			clone[id] = true
		}
	}
	return clone
}

func uniqueGraphIDs(ids []string, order map[string]int) []string {
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	sortGraphIDs(result, order)
	return result
}

func idSetKeys(values map[string]bool, order map[string]int) []string {
	ids := make([]string, 0, len(values))
	for id, included := range values {
		if included {
			ids = append(ids, id)
		}
	}
	sortGraphIDs(ids, order)
	return ids
}

func sortGraphIDs(ids []string, order map[string]int) {
	sort.SliceStable(ids, func(left, right int) bool {
		return graphIDLess(ids[left], ids[right], order)
	})
}

func graphIDLess(left, right string, order map[string]int) bool {
	leftOrder, leftOK := order[left]
	rightOrder, rightOK := order[right]
	if leftOK && rightOK && leftOrder != rightOrder {
		return leftOrder < rightOrder
	}
	if leftOK != rightOK {
		return leftOK
	}
	return left < right
}

func sortGraphEdges(edges []dependencyGraphEdge, order map[string]int) {
	sort.SliceStable(edges, func(left, right int) bool {
		if edges[left].From != edges[right].From {
			return graphIDLess(edges[left].From, edges[right].From, order)
		}
		return graphIDLess(edges[left].To, edges[right].To, order)
	})
}
