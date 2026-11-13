package geom

import "sort"

// Index is an immutable AABB tree over resolved source triangles. Queries return
// original indices in source order, preserving deterministic rasterization.
type Index struct {
	bounds []Bounds
	order  []int
	nodes  []indexNode
}
type indexNode struct {
	box                       Bounds
	left, right, start, count int
}

// NewIndex copies bounds and builds a deterministic median-split spatial index.
func NewIndex(triangles []ResolvedTriangle) *Index {
	bounds := make([]Bounds, len(triangles))
	for i, t := range triangles {
		bounds[i] = t.Bounds
	}
	return newBoundsIndex(bounds)
}

func newBoundsIndex(bounds []Bounds) *Index {
	x := &Index{bounds: bounds, order: make([]int, len(bounds))}
	for i := range x.order {
		x.order[i] = i
	}
	if len(bounds) > 0 {
		x.build(0, len(bounds))
	}
	return x
}

func (x *Index) build(start, end int) int {
	box := x.bounds[x.order[start]]
	for _, i := range x.order[start+1 : end] {
		box = box.Union(x.bounds[i])
	}
	node := len(x.nodes)
	x.nodes = append(x.nodes, indexNode{box: box, start: start, count: end - start, left: -1, right: -1})
	if end-start <= 16 {
		return node
	}
	dx, dy, dz := box.Max.X-box.Min.X, box.Max.Y-box.Min.Y, box.Max.Z-box.Min.Z
	axis := 0
	if dy > dx && dy > dz {
		axis = 1
	} else if dz > dx {
		axis = 2
	}
	center := func(i int) float64 {
		b := x.bounds[i]
		if axis == 1 {
			return b.Min.Y + b.Max.Y
		}
		if axis == 2 {
			return b.Min.Z + b.Max.Z
		}
		return b.Min.X + b.Max.X
	}
	sort.Slice(x.order[start:end], func(a, b int) bool {
		i, j := x.order[start+a], x.order[start+b]
		ca, cb := center(i), center(j)
		if ca == cb {
			return i < j
		}
		return ca < cb
	})
	mid := (start + end) / 2
	left, right := x.build(start, mid), x.build(mid, end)
	x.nodes[node].left = left
	x.nodes[node].right = right
	return node
}

// Query returns triangle indices whose bounds intersect the query box.
func (x *Index) Query(box Bounds) []int {
	return x.query(func(candidate Bounds) bool { return candidate.Intersects(box) })
}

func (x *Index) query(intersects func(Bounds) bool) []int {
	if x == nil || len(x.nodes) == 0 {
		return nil
	}
	var out []int
	stack := []int{0}
	for len(stack) > 0 {
		n := x.nodes[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		if !intersects(n.box) {
			continue
		}
		if n.left >= 0 {
			stack = append(stack, n.left, n.right)
			continue
		}
		for _, i := range x.order[n.start : n.start+n.count] {
			if intersects(x.bounds[i]) {
				out = append(out, i)
			}
		}
	}
	sort.Ints(out)
	return out
}
