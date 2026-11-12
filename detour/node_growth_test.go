package detour

import (
	"testing"
	"unsafe"
)

func TestNodePoolGrowthPreservesPointersParentsAndQueue(t *testing.T) {
	const limit = 2*nodeBlockSize + 5
	pool := DtAllocNodePool(limit, 16)
	queue := DtAllocNodeQueue(limit)
	if len(pool.m_blocks) != 0 || len(queue.m_heap) != 0 {
		t.Fatal("empty search allocated nodes or heap storage")
	}
	if pool.GetNodeAtIdx(1) != nil || pool.GetNodeAtIdx(65537) != nil {
		t.Fatal("unallocated index returned a node")
	}
	nodes := make([]*DtNode, limit)
	for i := range nodes {
		node := pool.GetNode(DtPolyRef(i/4+1), uint8(i%4))
		if node == nil {
			t.Fatalf("exhausted before configured limit at %d", i)
		}
		node.Total = float32(limit - i)
		if i > 0 {
			node.Pidx = pool.GetNodeIdx(nodes[i-1])
		}
		nodes[i] = node
		queue.Push(node)
	}
	if pool.GetNode(1000, 0) != nil || pool.GetMaxNodes() != limit || pool.GetNodeCount() != limit || queue.GetCapacity() != limit {
		t.Fatal("growth changed configured limits")
	}
	for i, node := range nodes {
		if pool.GetNodeIdx(node) != uint32(i+1) || pool.GetNodeAtIdx(uint32(i+1)) != node || pool.FindNode(node.Id, node.State) != node {
			t.Fatalf("node %d moved or changed identity across growth", i)
		}
		if i > 0 && pool.GetNodeAtIdx(node.Pidx) != nodes[i-1] {
			t.Fatalf("parent of node %d changed", i)
		}
	}
	var sameRef [4]*DtNode
	if n := pool.FindNodes(33, sameRef[:], 4); n != 4 {
		t.Fatalf("same-reference state lookup across blocks = %d", n)
	}
	// Modify an early node after both storage types have grown. Every queued
	// pointer must still observe writes through the original returned address.
	nodes[0].Total = -1
	queue.Modify(nodes[0])
	if queue.Pop() != nodes[0] {
		t.Fatal("queue no longer points at the first node")
	}
	for i := limit - 1; i >= 1; i-- {
		if got := queue.Pop(); got != nodes[i] {
			t.Fatalf("queue order/identity changed at %d", i)
		}
	}
	allocated := 0
	for _, block := range pool.m_blocks {
		allocated += len(block.nodes)
	}
	if allocated != limit || len(queue.m_heap) > limit+1 || !queue.Empty() {
		t.Fatal("allocated storage exceeded its bound")
	}
	before := pool.GetMemUsed()
	pool.Clear()
	if pool.FindNode(1, 0) != nil || pool.GetNodeCount() != 0 || pool.GetMemUsed() != before {
		t.Fatal("Clear retained lookup state or discarded reusable blocks")
	}
	for i := range nodes {
		if got := pool.GetNode(DtPolyRef(i+500), 0); got != nodes[i] || got.Pidx != 0 || got.Flags != 0 {
			t.Fatalf("reuse retained search state at %d", i)
		}
	}
	if unsafe.Sizeof(DtNode{}) != 32 {
		t.Fatal("stable index should fit the original node's padding")
	}
}

func TestSlicedSearchAcrossNodeGrowth(t *testing.T) {
	const count = 2*nodeBlockSize + 5
	mesh, q, ref := queryForTile(t, chainTile(t, count, false, false))
	if s := q.Init(mesh, count); DtStatusFailed(s) {
		t.Fatal(s)
	}
	filter := DtAllocDtQueryFilter()
	status := q.InitSlicedFindPath(ref, ref+count-1, []float32{1, 0, 1}, []float32{count*2 - 1, 0, 1}, filter, 0)
	first := q.GetNodePool().FindNode(ref, 0)
	for DtStatusInProgress(status) {
		status = q.UpdateSlicedFindPath(1, nil)
		if q.GetNodePool().FindNode(ref, 0) != first {
			t.Fatal("sliced search moved a retained node")
		}
	}
	path := make([]DtPolyRef, count)
	var n int
	status = q.FinalizeSlicedFindPath(path, &n, len(path))
	if DtStatusFailed(status) || DtStatusDetail(status, DT_PARTIAL_RESULT|DT_OUT_OF_NODES|DT_BUFFER_TOO_SMALL) || n != count {
		t.Fatalf("sliced search failed after growth: %#x count=%d", status, n)
	}
	for i, got := range path {
		if got != ref+DtPolyRef(i) {
			t.Fatalf("wrong parent chain at %d: %d", i, got)
		}
	}
}

func TestLocalMovementSpareSlotPreservesExactNodeLimit(t *testing.T) {
	_, q, ref := queryForTile(t, chainTile(t, DT_MAX_LOCAL_NODES, false, false))
	filter := DtAllocDtQueryFilter()
	for _, size := range []int{DT_MAX_LOCAL_NODES + 1, 2048} {
		visited := make([]DtPolyRef, size)
		var count int
		var position [3]float32
		var hit bool
		status := q.MoveAlongSurface(ref, []float32{1, 0, 1}, []float32{DT_MAX_LOCAL_NODES*2 - 1, 0, 1}, filter, position[:], visited, &count, size, &hit)
		if status != DT_SUCCESS || count != DT_MAX_LOCAL_NODES || position[0] != DT_MAX_LOCAL_NODES*2-1 {
			t.Fatalf("buffer=%d status=%#x count=%d position=%v", size, status, count, position)
		}
	}
}
