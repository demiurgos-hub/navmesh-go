package recast

// A distance-bounded wall chord can cross another contour edge in a narrow
// region. Retain raw samples until those new intersections disappear. Region
// portals are not refined here: independently changing their subdivisions would
// break adjacency to the neighboring contour. Existing shared endpoints remain
// valid for weakly simple contours; an already crossing raw edge is not repaired.
func refineCrossingWallEdges(points, simplified []int) []int {
	pn := len(points) / 4
	for {
		split := -1
		for i := 0; i < len(simplified)/4 && split < 0; i++ {
			ni := (i + 1) % (len(simplified) / 4)
			a, b := simplified[i*4:], simplified[ni*4:]
			for j := i + 1; j < len(simplified)/4; j++ {
				nj := (j + 1) % (len(simplified) / 4)
				if j == ni || nj == i {
					continue
				}
				c, d := simplified[j*4:], simplified[nj*4:]
				if vequalInt(a, c) || vequalInt(a, d) || vequalInt(b, c) || vequalInt(b, d) || !intersectInt(a, b, c, d) {
					continue
				}
				for _, edge := range []int{i, j} {
					start := simplified[edge*4+3]
					end := simplified[((edge+1)%(len(simplified)/4))*4+3]
					count := (end - start + pn) % pn
					if count > 1 && points[((start+1)%pn)*4+3]&contourRegMask == 0 {
						split = edge
						break
					}
				}
				if split >= 0 {
					break
				}
			}
		}
		if split < 0 {
			return simplified
		}
		start := simplified[split*4+3]
		end := simplified[((split+1)%(len(simplified)/4))*4+3]
		index := (start + ((end-start+pn)%pn)/2) % pn
		at := (split + 1) * 4
		simplified = append(simplified, 0, 0, 0, 0)
		copy(simplified[at+4:], simplified[at:len(simplified)-4])
		copy(simplified[at:at+3], points[index*4:index*4+3])
		simplified[at+3] = index
	}
}
