// Modified Go translation for navmesh-go; see PROVENANCE.md.
// Copyright (c) 2009-2010 Mikko Mononen memon@inside.org
//
// This software is provided 'as-is', without any express or implied
// warranty.  In no event will the authors be held liable for any damages
// arising from the use of this software.
// Permission is granted to anyone to use this software for any purpose,
// including commercial applications, and to alter it and redistribute it
// freely, subject to the following restrictions:
// 1. The origin of this software must not be misrepresented; you must not
//    claim that you wrote the original software. If you use this software
//    in a product, an acknowledgment in the product documentation would be
//    appreciated but is not required.
// 2. Altered source versions must be plainly marked as such, and must not be
//    misrepresented as being the original software.
// 3. This notice may not be removed or altered from any source distribution.
//

package detour

import (
	"unsafe"
)

type DtNodeFlags uint8

const (
	DT_NODE_OPEN            DtNodeFlags = 0x01
	DT_NODE_CLOSED          DtNodeFlags = 0x02
	DT_NODE_PARENT_DETACHED DtNodeFlags = 0x04 // parent of the node is not adjacent. Found using raycast.
)

type DtNodeIndex uint16

const DT_NULL_IDX DtNodeIndex = ^DtNodeIndex(0)

const DT_NODE_PARENT_BITS uint32 = 24
const DT_NODE_STATE_BITS uint32 = 2
const DT_NODE_FLAGS_BITS uint32 = 3

type DtNode struct {
	Pos       [3]float32  ///< Position of the node.
	Cost      float32     ///< Cost from previous node to current node.
	Total     float32     ///< Cost up to the node.
	Pidx      uint32      ///< Index to parent node.
	State     uint8       ///< extra state information. A polyRef can have multiple nodes with different extra info. see DT_MAX_STATES_PER_NODE
	Flags     DtNodeFlags ///< Node flags. A combination of dtNodeFlags.
	poolIndex DtNodeIndex
	Id        DtPolyRef ///< Polygon ref the node corresponds to.
}

const DT_MAX_STATES_PER_NODE int = 1 << DT_NODE_STATE_BITS // number of extra states per node. See dtNode::state

const nodeBlockSize = 128

type nodeBlock struct {
	nodes []DtNode
	next  []DtNodeIndex
}

type DtNodePool struct {
	m_blocks    []nodeBlock
	m_first     []DtNodeIndex
	m_maxNodes  uint32
	m_hashSize  uint32
	m_nodeCount uint32
}

// GetNodeIdx returns the one-based index of a node allocated by this pool.
func (this *DtNodePool) GetNodeIdx(node *DtNode) uint32 {
	if node == nil {
		return 0
	}
	return uint32(node.poolIndex) + 1
}

// GetNodeAtIdx returns the currently allocated node at idx, or nil if invalid.
func (this *DtNodePool) GetNodeAtIdx(idx uint32) *DtNode {
	if idx == 0 || idx > this.m_nodeCount {
		return nil
	}
	return this.nodeAt(DtNodeIndex(idx - 1))
}

// GetMemUsed reports allocated pool storage, which grows within GetMaxNodes.
func (this *DtNodePool) GetMemUsed() uint32 {
	n := uint32(unsafe.Sizeof(*this)) + uint32(cap(this.m_blocks))*uint32(unsafe.Sizeof(nodeBlock{})) +
		uint32(cap(this.m_first))*uint32(unsafe.Sizeof(DtNodeIndex(0)))
	for _, block := range this.m_blocks {
		n += uint32(cap(block.nodes))*uint32(unsafe.Sizeof(DtNode{})) + uint32(cap(block.next))*uint32(unsafe.Sizeof(DtNodeIndex(0)))
	}
	return n
}

func (this *DtNodePool) GetMaxNodes() uint32             { return this.m_maxNodes }
func (this *DtNodePool) GetHashSize() uint32             { return this.m_hashSize }
func (this *DtNodePool) GetFirst(bucket int) DtNodeIndex { return this.m_first[bucket] }
func (this *DtNodePool) GetNext(i int) DtNodeIndex {
	return this.m_blocks[i/nodeBlockSize].next[i%nodeBlockSize]
}
func (this *DtNodePool) GetNodeCount() uint32 { return this.m_nodeCount }

func DtAllocNodePool(maxNodes, hashSize uint32) *DtNodePool {
	pool := &DtNodePool{}
	pool.constructor(maxNodes, hashSize)
	return pool
}

func DtFreeNodePool(pool *DtNodePool) {
	if pool == nil {
		return
	}
	pool.destructor()
}

type DtNodeQueue struct {
	m_heap     []*DtNode
	m_capacity int
	m_size     int
}

func (this *DtNodeQueue) Clear() { this.m_size = 0 }

func (this *DtNodeQueue) Top() *DtNode { return this.m_heap[0] }

func (this *DtNodeQueue) Pop() *DtNode {
	result := this.m_heap[0]
	this.m_size--
	this.trickleDown(0, this.m_heap[this.m_size])
	return result
}

func (this *DtNodeQueue) Push(node *DtNode) {
	DtAssert(this.m_size < this.m_capacity)
	if this.m_size == len(this.m_heap) {
		n := len(this.m_heap) * 2
		if n < 64 {
			n = 64
		}
		if n > this.m_capacity+1 {
			n = this.m_capacity + 1
		}
		heap := make([]*DtNode, n)
		copy(heap, this.m_heap)
		this.m_heap = heap
	}
	this.m_size++
	this.bubbleUp(this.m_size-1, node)
}

// / Repositions a node that is already in the queue (its sort key changed).
// /
// / This mirrors the C++ original, which scans the heap for the node.
// /
// / An O(log n) variant was implemented and measured (caching each node's heap
// / position on the node): maintaining that index costs a read-modify-write per
// / heap level on every push/pop, and on the pathfinding workload that outweighed
// / the scan it removed (FindPath +1.8%, SlicedFindPath +4.5% over 6 samples
// / each). The linear scan over a contiguous pointer array wins on this workload,
// / so it is kept deliberately.
func (this *DtNodeQueue) Modify(node *DtNode) {
	for i := 0; i < this.m_size; i++ {
		if this.m_heap[i] == node {
			this.bubbleUp(i, node)
			return
		}
	}
}

func (this *DtNodeQueue) Empty() bool { return this.m_size == 0 }

// GetMemUsed reports allocated queue storage rather than its configured limit.
func (this *DtNodeQueue) GetMemUsed() uint32 {
	return uint32(unsafe.Sizeof(*this)) +
		uint32(unsafe.Sizeof((*DtNode)(nil)))*uint32(cap(this.m_heap))
}

func (this *DtNodeQueue) GetCapacity() int { return this.m_capacity }

func DtAllocNodeQueue(n int) *DtNodeQueue {
	queue := &DtNodeQueue{}
	queue.constructor(n)
	return queue
}

func DtFreeNodeQueue(queue *DtNodeQueue) {
	if queue == nil {
		return
	}
	queue.destructor()
}
