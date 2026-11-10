//
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

// Note: This header file's only purpose is to include define assert.
// Feel free to change the file and include your own implementation instead.

package detour

import (
	"encoding/binary"
	"math"
	"sort"
)

var MESH_NULL_IDX uint16 = 0xffff

type BVItem struct {
	bmin [3]uint16
	bmax [3]uint16
	i    int
}

func compareItemX(a, b *BVItem) bool {
	return a.bmin[0] < b.bmin[0]
}

func compareItemY(a, b *BVItem) bool {
	return a.bmin[1] < b.bmin[1]
}

func compareItemZ(a, b *BVItem) bool {
	return a.bmin[2] < b.bmin[2]
}

func calcExtends(items []BVItem, _ /*nitems*/, imin, imax int,
	bmin, bmax []uint16) {
	bmin[0] = items[imin].bmin[0]
	bmin[1] = items[imin].bmin[1]
	bmin[2] = items[imin].bmin[2]

	bmax[0] = items[imin].bmax[0]
	bmax[1] = items[imin].bmax[1]
	bmax[2] = items[imin].bmax[2]

	for i := imin + 1; i < imax; i++ {
		it := &items[i]
		if it.bmin[0] < bmin[0] {
			bmin[0] = it.bmin[0]
		}
		if it.bmin[1] < bmin[1] {
			bmin[1] = it.bmin[1]
		}
		if it.bmin[2] < bmin[2] {
			bmin[2] = it.bmin[2]
		}

		if it.bmax[0] > bmax[0] {
			bmax[0] = it.bmax[0]
		}
		if it.bmax[1] > bmax[1] {
			bmax[1] = it.bmax[1]
		}
		if it.bmax[2] > bmax[2] {
			bmax[2] = it.bmax[2]
		}
	}
}

func longestAxis(x, y, z uint16) int {
	axis := 0
	maxVal := x
	if y > maxVal {
		axis = 1
		maxVal = y
	}
	if z > maxVal {
		axis = 2
	}
	return axis
}

type sorter struct {
	lst      []BVItem
	compFunc func(a, b *BVItem) bool
}

func newSorter(lst []BVItem, compFunc func(a, b *BVItem) bool) *sorter {
	return &sorter{
		lst:      lst,
		compFunc: compFunc,
	}
}

func (this *sorter) Len() int {
	return len(this.lst)
}

func (this *sorter) Swap(i, j int) {
	this.lst[i], this.lst[j] = this.lst[j], this.lst[i]
}

func (this *sorter) Less(i, j int) bool {
	return this.compFunc(&this.lst[i], &this.lst[j])
}

func subdivide(items []BVItem, nitems, imin, imax int, curNode *int, nodes []DtBVNode) {
	inum := imax - imin
	icur := *curNode

	node := &nodes[*curNode]
	(*curNode)++

	if inum == 1 {
		// Leaf
		node.Bmin[0] = items[imin].bmin[0]
		node.Bmin[1] = items[imin].bmin[1]
		node.Bmin[2] = items[imin].bmin[2]

		node.Bmax[0] = items[imin].bmax[0]
		node.Bmax[1] = items[imin].bmax[1]
		node.Bmax[2] = items[imin].bmax[2]

		node.I = int32(items[imin].i)
	} else {
		// Split
		calcExtends(items, nitems, imin, imax, node.Bmin[:], node.Bmax[:])

		axis := longestAxis(node.Bmax[0]-node.Bmin[0],
			node.Bmax[1]-node.Bmin[1],
			node.Bmax[2]-node.Bmin[2])

		if axis == 0 {
			// Sort along x-axis
			sort.Sort(newSorter(items[imin:imin+inum], compareItemX))
		} else if axis == 1 {
			// Sort along y-axis
			sort.Sort(newSorter(items[imin:imin+inum], compareItemY))
		} else {
			// Sort along z-axis
			sort.Sort(newSorter(items[imin:imin+inum], compareItemZ))
		}

		isplit := imin + inum/2

		// Left
		subdivide(items, nitems, imin, isplit, curNode, nodes)
		// Right
		subdivide(items, nitems, isplit, imax, curNode, nodes)

		iescape := *curNode - icur
		// Negative index means escape.
		node.I = int32(-iescape)
	}
}

func createBVTree(params *DtNavMeshCreateParams, nodes []DtBVNode, _ int /*nnodes*/) int {
	// Build tree
	quantFactor := 1 / float32(params.Cs)
	items := make([]BVItem, params.PolyCount)
	for i := 0; i < int(params.PolyCount); i++ {
		it := &items[i]
		it.i = i
		// Calc polygon bounds. Use detail meshes if available.
		if params.DetailMeshes != nil {
			vb := (int)(params.DetailMeshes[i*4+0])
			ndv := (int)(params.DetailMeshes[i*4+1])
			var bmin [3]float32
			var bmax [3]float32

			dv := params.DetailVerts[vb*3:]
			DtVcopy(bmin[:], dv)
			DtVcopy(bmax[:], dv)

			for j := 1; j < ndv; j++ {
				DtVmin(bmin[:], dv[j*3:])
				DtVmax(bmax[:], dv[j*3:])
			}

			// BV-tree uses cs for all dimensions
			it.bmin[0] = (uint16)(DtClampUInt32((uint32)((bmin[0]-params.Bmin[0])*quantFactor), 0, 0xffff))
			it.bmin[1] = (uint16)(DtClampUInt32((uint32)((bmin[1]-params.Bmin[1])*quantFactor), 0, 0xffff))
			it.bmin[2] = (uint16)(DtClampUInt32((uint32)((bmin[2]-params.Bmin[2])*quantFactor), 0, 0xffff))

			it.bmax[0] = (uint16)(DtClampUInt32((uint32)((bmax[0]-params.Bmin[0])*quantFactor), 0, 0xffff))
			it.bmax[1] = (uint16)(DtClampUInt32((uint32)((bmax[1]-params.Bmin[1])*quantFactor), 0, 0xffff))
			it.bmax[2] = (uint16)(DtClampUInt32((uint32)((bmax[2]-params.Bmin[2])*quantFactor), 0, 0xffff))
		} else {
			p := params.Polys[i*int(params.Nvp)*2:]
			it.bmax[0] = params.Verts[p[0]*3+0]
			it.bmin[0] = it.bmax[0]
			it.bmax[1] = params.Verts[p[0]*3+1]
			it.bmin[1] = it.bmax[1]
			it.bmax[2] = params.Verts[p[0]*3+2]
			it.bmin[2] = it.bmax[2]

			for j := 1; j < int(params.Nvp); j++ {
				if p[j] == MESH_NULL_IDX {
					break
				}
				x := params.Verts[p[j]*3+0]
				y := params.Verts[p[j]*3+1]
				z := params.Verts[p[j]*3+2]

				if x < it.bmin[0] {
					it.bmin[0] = x
				}
				if y < it.bmin[1] {
					it.bmin[1] = y
				}
				if z < it.bmin[2] {
					it.bmin[2] = z
				}

				if x > it.bmax[0] {
					it.bmax[0] = x
				}
				if y > it.bmax[1] {
					it.bmax[1] = y
				}
				if z > it.bmax[2] {
					it.bmax[2] = z
				}
			}
			// Remap y
			it.bmin[1] = (uint16)(DtMathFloorf((float32)(it.bmin[1]) * params.Ch / params.Cs))
			it.bmax[1] = (uint16)(DtMathCeilf((float32)(it.bmax[1]) * params.Ch / params.Cs))
		}
	}

	curNode := 0
	subdivide(items, int(params.PolyCount), 0, int(params.PolyCount), &curNode, nodes)

	items = nil

	return curNode
}

func classifyOffMeshPoint(pt, bmin, bmax []float32) uint8 {
	const XP uint8 = 1 << 0
	const ZP uint8 = 1 << 1
	const XM uint8 = 1 << 2
	const ZM uint8 = 1 << 3

	var outcode uint8
	if pt[0] >= bmax[0] {
		outcode |= XP
	}
	if pt[2] >= bmax[2] {
		outcode |= ZP
	}
	if pt[0] < bmin[0] {
		outcode |= XM
	}
	if pt[2] < bmin[2] {
		outcode |= ZM
	}

	switch outcode {
	case XP:
		return 0
	case XP | ZP:
		return 1
	case ZP:
		return 2
	case XM | ZP:
		return 3
	case XM:
		return 4
	case XM | ZM:
		return 5
	case ZM:
		return 6
	case XP | ZM:
		return 7
	}

	return 0xff
}

// / Builds navigation mesh tile data from the provided tile creation data.
// / @ingroup detour
// /  @param[in]		params		Tile creation data.
// /  @param[out]	outData		The resulting tile data.
// /  @param[out]	outDataSize	The size of the tile data array.
// / @return True if the tile data was successfully created.
// / @par
// /
// / The output data array is allocated using the detour allocator (dtAlloc()).  The method
// / used to free the memory will be determined by how the tile is added to the navigation
// / mesh.
// /
// / @see dtNavMesh, dtNavMesh::addTile()
func DtCreateNavMeshData(params *DtNavMeshCreateParams, outData *[]byte, outDataSize *int) bool {
	if outData == nil || outDataSize == nil {
		return false
	}
	*outData = nil
	*outDataSize = 0
	if !validCreateParams(params) {
		return false
	}
	nvp := int(params.Nvp)

	// Classify off-mesh connection points. We store only the connections
	// whose start point is inside the tile.
	var offMeshConClass []byte
	var storedOffMeshConCount int
	var offMeshConLinkCount int

	if params.OffMeshConCount > 0 {
		offMeshConClass = make([]byte, params.OffMeshConCount*2)
		if offMeshConClass == nil {
			return false
		}
		// Find tight heigh bounds, used for culling out off-mesh start locations.
		hmin := float32(math.MaxFloat32)
		hmax := -float32(math.MaxFloat32)

		if params.DetailVerts != nil && params.DetailVertsCount != 0 {
			for i := 0; i < int(params.DetailVertsCount); i++ {
				h := params.DetailVerts[i*3+1]
				hmin = DtMinFloat32(hmin, h)
				hmax = DtMaxFloat32(hmax, h)
			}
		} else {
			for i := 0; i < int(params.VertCount); i++ {
				iv := params.Verts[i*3:]
				h := params.Bmin[1] + float32(iv[1])*params.Ch
				hmin = DtMinFloat32(hmin, h)
				hmax = DtMaxFloat32(hmax, h)
			}
		}
		hmin -= params.WalkableClimb
		hmax += params.WalkableClimb
		var bmin, bmax [3]float32
		DtVcopy(bmin[:], params.Bmin[:])
		DtVcopy(bmax[:], params.Bmax[:])
		bmin[1] = hmin
		bmax[1] = hmax

		for i := 0; i < int(params.OffMeshConCount); i++ {
			p0 := params.OffMeshConVerts[(i*2+0)*3:]
			p1 := params.OffMeshConVerts[(i*2+1)*3:]
			offMeshConClass[i*2+0] = classifyOffMeshPoint(p0, bmin[:], bmax[:])
			offMeshConClass[i*2+1] = classifyOffMeshPoint(p1, bmin[:], bmax[:])

			// Zero out off-mesh start positions which are not even potentially touching the mesh.
			if offMeshConClass[i*2+0] == 0xff {
				if p0[1] < bmin[1] || p0[1] > bmax[1] {
					offMeshConClass[i*2+0] = 0
				}
			}

			// Cound how many links should be allocated for off-mesh connections.
			if offMeshConClass[i*2+0] == 0xff {
				offMeshConLinkCount++
			}
			if offMeshConClass[i*2+1] == 0xff {
				offMeshConLinkCount++
			}
			if offMeshConClass[i*2+0] == 0xff {
				storedOffMeshConCount++
			}
		}
	}

	// Off-mesh connectionss are stored as polygons, adjust values.
	totPolyCount := int(params.PolyCount) + storedOffMeshConCount
	totVertCount := int(params.VertCount) + storedOffMeshConCount*2

	// Find portal edges which are at tile borders.
	edgeCount := 0
	portalCount := 0
	for i := 0; i < int(params.PolyCount); i++ {
		p := params.Polys[i*2*nvp:]
		for j := 0; j < nvp; j++ {
			if p[j] == MESH_NULL_IDX {
				break
			}
			edgeCount++

			if (p[nvp+j] & 0x8000) != 0 {
				dir := p[nvp+j] & 0xf
				if dir != 0xf {
					portalCount++
				}
			}
		}
	}

	maxLinkCount := edgeCount + portalCount*2 + offMeshConLinkCount*2

	// Find unique detail vertices.
	uniqueDetailVertCount := 0
	detailTriCount := 0
	if params.DetailMeshes != nil {
		// Has detail mesh, count unique detail vertex count and use input detail tri count.
		detailTriCount = int(params.DetailTriCount)
		for i := 0; i < int(params.PolyCount); i++ {
			p := params.Polys[i*nvp*2:]
			ndv := int(params.DetailMeshes[i*4+1])
			nv := 0
			for j := 0; j < nvp; j++ {
				if p[j] == MESH_NULL_IDX {
					break
				}
				nv++
			}
			ndv -= nv
			uniqueDetailVertCount += ndv
		}
	} else {
		// No input detail mesh, build detail mesh from nav polys.
		uniqueDetailVertCount = 0 // No extra detail verts.
		detailTriCount = 0
		for i := 0; i < int(params.PolyCount); i++ {
			p := params.Polys[i*nvp*2:]
			nv := 0
			for j := 0; j < nvp; j++ {
				if p[j] == MESH_NULL_IDX {
					break
				}
				nv++
			}
			detailTriCount += nv - 2
		}
	}

	// Build typed Go arrays; serialization uses an explicit wire layout.
	header := new(DtMeshHeader)
	navVerts := make([]float32, totVertCount*3)
	navPolys := make([]DtPoly, totPolyCount)
	navDMeshes := make([]DtPolyDetail, params.PolyCount)
	navDVerts := make([]float32, uniqueDetailVertCount*3)
	navDTris := make([]byte, detailTriCount*4)
	var navBvtree []DtBVNode
	if params.BuildBvTree {
		navBvtree = make([]DtBVNode, params.PolyCount*2)
	}
	offMeshCons := make([]DtOffMeshConnection, storedOffMeshConCount)

	// Store header
	header.Magic = DT_NAVMESH_MAGIC
	header.Version = DT_NAVMESH_VERSION
	header.X = params.TileX
	header.Y = params.TileY
	header.Layer = params.TileLayer
	header.UserId = params.UserId
	header.PolyCount = int32(totPolyCount)
	header.VertCount = int32(totVertCount)
	header.MaxLinkCount = int32(maxLinkCount)
	DtVcopy(header.Bmin[:], params.Bmin[:])
	DtVcopy(header.Bmax[:], params.Bmax[:])
	header.DetailMeshCount = params.PolyCount
	header.DetailVertCount = int32(uniqueDetailVertCount)
	header.DetailTriCount = int32(detailTriCount)
	header.BvQuantFactor = 1.0 / float32(params.Cs)
	header.OffMeshBase = params.PolyCount
	header.WalkableHeight = params.WalkableHeight
	header.WalkableRadius = params.WalkableRadius
	header.WalkableClimb = params.WalkableClimb
	header.OffMeshConCount = int32(storedOffMeshConCount)
	if params.BuildBvTree {
		header.BvNodeCount = params.PolyCount * 2
	} else {
		header.BvNodeCount = 0
	}

	offMeshVertsBase := params.VertCount
	offMeshPolyBase := params.PolyCount

	// Store vertices
	// Mesh vertices
	for i := 0; i < int(params.VertCount); i++ {
		iv := params.Verts[i*3:]
		v := navVerts[i*3:]
		v[0] = params.Bmin[0] + float32(iv[0])*params.Cs
		v[1] = params.Bmin[1] + float32(iv[1])*params.Ch
		v[2] = params.Bmin[2] + float32(iv[2])*params.Cs
	}
	// Off-mesh link vertices.
	n := 0
	for i := 0; i < int(params.OffMeshConCount); i++ {
		// Only store connections which start from this tile.
		if offMeshConClass[i*2+0] == 0xff {
			linkv := params.OffMeshConVerts[i*2*3:]
			v := navVerts[(int(offMeshVertsBase)+n*2)*3:]
			DtVcopy(v[0:], linkv[0:])
			DtVcopy(v[3:], linkv[3:])
			n++
		}
	}

	// Store polygons
	// Mesh polys
	srcIndex := 0
	src := params.Polys[srcIndex:]
	for i := 0; i < int(params.PolyCount); i++ {
		p := &navPolys[i]
		p.VertCount = 0
		p.Flags = params.PolyFlags[i]
		p.SetArea(params.PolyAreas[i])
		p.SetType(DT_POLYTYPE_GROUND)
		for j := 0; j < nvp; j++ {
			if src[j] == MESH_NULL_IDX {
				break
			}
			p.Verts[j] = src[j]
			if (src[nvp+j] & 0x8000) != 0 {
				// Border or portal edge.
				dir := src[nvp+j] & 0xf
				if dir == 0xf { // Border
					p.Neis[j] = 0
				} else if dir == 0 { // Portal x-
					p.Neis[j] = DT_EXT_LINK | 4
				} else if dir == 1 { // Portal z+
					p.Neis[j] = DT_EXT_LINK | 2
				} else if dir == 2 { // Portal x+
					p.Neis[j] = DT_EXT_LINK | 0
				} else if dir == 3 { // Portal z-
					p.Neis[j] = DT_EXT_LINK | 6
				}
			} else {
				// Normal connection
				p.Neis[j] = src[nvp+j] + 1
			}

			p.VertCount++
		}
		srcIndex += nvp * 2
		src = params.Polys[srcIndex:]
	}
	// Off-mesh connection vertices.
	n = 0
	for i := 0; i < int(params.OffMeshConCount); i++ {
		// Only store connections which start from this tile.
		if offMeshConClass[i*2+0] == 0xff {
			p := &navPolys[int(offMeshPolyBase)+n]
			p.VertCount = 2
			p.Verts[0] = (uint16)(int(offMeshVertsBase) + n*2 + 0)
			p.Verts[1] = (uint16)(int(offMeshVertsBase) + n*2 + 1)
			p.Flags = params.OffMeshConFlags[i]
			p.SetArea(params.OffMeshConAreas[i])
			p.SetType(DT_POLYTYPE_OFFMESH_CONNECTION)
			n++
		}
	}

	// Store detail meshes and vertices.
	// The nav polygon vertices are stored as the first vertices on each mesh.
	// We compress the mesh data by skipping them and using the navmesh coordinates.
	if params.DetailMeshes != nil {
		vbase := 0
		for i := 0; i < int(params.PolyCount); i++ {
			dtl := &navDMeshes[i]
			vb := (int)(params.DetailMeshes[i*4+0])
			ndv := (int)(params.DetailMeshes[i*4+1])
			nv := int(navPolys[i].VertCount)
			dtl.VertBase = (uint32)(vbase)
			dtl.VertCount = (uint8)(ndv - nv)
			dtl.TriBase = (uint32)(params.DetailMeshes[i*4+2])
			dtl.TriCount = (uint8)(params.DetailMeshes[i*4+3])
			// Copy vertices except the first 'nv' verts which are equal to nav poly verts.
			if (ndv - nv) != 0 {
				copy(navDVerts[vbase*3:], params.DetailVerts[(vb+nv)*3:(vb+nv)*3+3*(ndv-nv)])
				vbase += (int)(ndv - nv)
			}
		}
		// Store triangles.
		copy(navDTris, params.DetailTris[:4*params.DetailTriCount])
	} else {
		// Create dummy detail mesh by triangulating polys.
		tbase := 0
		for i := 0; i < int(params.PolyCount); i++ {
			dtl := &navDMeshes[i]
			nv := int(navPolys[i].VertCount)
			dtl.VertBase = 0
			dtl.VertCount = 0
			dtl.TriBase = (uint32)(tbase)
			dtl.TriCount = (uint8)(nv - 2)
			// Triangulate polygon (local indices).
			for j := 2; j < nv; j++ {
				t := navDTris[tbase*4:]
				t[0] = 0
				t[1] = (uint8)(j - 1)
				t[2] = (uint8)(j)
				// Bit for each edge that belongs to poly boundary.
				t[3] = (1 << 2)
				if j == 2 {
					t[3] |= (1 << 0)
				}
				if j == nv-1 {
					t[3] |= (1 << 4)
				}
				tbase++
			}
		}
	}

	// Store and create BVtree.
	if params.BuildBvTree {
		createBVTree(params, navBvtree, int(2*params.PolyCount))
	}

	// Store Off-Mesh connections.
	n = 0
	for i := 0; i < int(params.OffMeshConCount); i++ {
		// Only store connections which start from this tile.
		if offMeshConClass[i*2+0] == 0xff {
			con := &offMeshCons[n]
			con.Poly = (uint16)(int(offMeshPolyBase) + n)
			// Copy connection end-points.
			endPts := params.OffMeshConVerts[i*2*3:]
			DtVcopy(con.Pos[0:], endPts[0:])
			DtVcopy(con.Pos[3:], endPts[3:])
			con.Rad = params.OffMeshConRad[i]

			if params.OffMeshConDir[i] != 0 {
				con.Flags = DT_OFFMESH_CON_BIDIR
			} else {
				con.Flags = 0
			}
			con.Side = offMeshConClass[i*2+1]
			if params.OffMeshConUserID != nil {
				con.UserId = params.OffMeshConUserID[i]
			}
			n++
		}
	}

	offMeshConClass = nil

	data, err := EncodeNavMeshData(&DtMeshTile{Header: header, Verts: navVerts, Polys: navPolys, DetailMeshes: navDMeshes, DetailVerts: navDVerts, DetailTris: navDTris, BvTree: navBvtree, OffMeshCons: offMeshCons})
	if err != nil {
		return false
	}
	*outData = data
	*outDataSize = len(data)

	return true
}

// / Swaps the endianess of the tile data's header (#dtMeshHeader).
// /  @param[in,out]	data		The tile data array.
// /  @param[in]		dataSize	The size of the data array.
func DtNavMeshHeaderSwapEndian(data []byte, dataSize int) bool {
	if dataSize < NavMeshHeaderSize || dataSize > len(data) {
		return false
	}
	le, be := binary.LittleEndian, binary.BigEndian
	if !(le.Uint32(data) == uint32(DT_NAVMESH_MAGIC) && le.Uint32(data[4:]) == uint32(DT_NAVMESH_VERSION)) && !(be.Uint32(data) == uint32(DT_NAVMESH_MAGIC) && be.Uint32(data[4:]) == uint32(DT_NAVMESH_VERSION)) {
		return false
	}
	for i := 0; i < NavMeshHeaderSize; i += 4 {
		reverse4(data[i:])
	}
	return true
}

// / Swaps endianess of the tile data.
// /  @param[in,out]	data		The tile data array.
// /  @param[in]		dataSize	The size of the data array.
// / @par
// /
// / @warning This function assumes that the header is in the correct endianess already.
// / Call #dtNavMeshHeaderSwapEndian() first on the data if the data is expected to be in wrong endianess
// / to start with. Call #dtNavMeshHeaderSwapEndian() after the data has been swapped if converting from
// / native to foreign endianess.
func DtNavMeshDataSwapEndian(data []byte, dataSize int) bool {
	if dataSize < NavMeshHeaderSize || dataSize > len(data) {
		return false
	}
	h, err := tileHeader(data[:dataSize])
	if err != nil {
		return false
	}
	size, err := tileEncodedSize(&h)
	if err != nil || size != dataSize {
		return false
	}
	offset := NavMeshHeaderSize
	for i := 0; i < int(h.VertCount)*3; i++ {
		reverse4(data[offset:])
		offset += 4
	}
	for i := 0; i < int(h.PolyCount); i++ {
		p := data[offset:]
		reverse4(p)
		for j := 4; j < 30; j += 2 {
			p[j], p[j+1] = p[j+1], p[j]
		}
		offset += 32
	}
	offset += int(h.MaxLinkCount) * 12
	for i := 0; i < int(h.DetailMeshCount); i++ {
		reverse4(data[offset:])
		reverse4(data[offset+4:])
		offset += 12
	}
	for i := 0; i < int(h.DetailVertCount)*3; i++ {
		reverse4(data[offset:])
		offset += 4
	}
	offset += int(h.DetailTriCount) * 4
	for i := 0; i < int(h.BvNodeCount); i++ {
		p := data[offset:]
		for j := 0; j < 12; j += 2 {
			p[j], p[j+1] = p[j+1], p[j]
		}
		reverse4(p[12:])
		offset += 16
	}
	for i := 0; i < int(h.OffMeshConCount); i++ {
		p := data[offset:]
		for j := 0; j < 28; j += 4 {
			reverse4(p[j:])
		}
		p[28], p[29] = p[29], p[28]
		reverse4(p[32:])
		offset += 36
	}
	return true
}

func reverse4(b []byte) { b[0], b[3] = b[3], b[0]; b[1], b[2] = b[2], b[1] }
