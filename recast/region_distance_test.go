package recast

import (
	"context"
	"testing"
)

func TestBuildDistanceFieldStoresBlurredDistances(t *testing.T) {
	const width = 5
	chf := &CompactHeightfield{Width: width, Height: width, SpanCount: width * width,
		Cells: make([]CompactCell, width*width), Spans: make([]CompactSpan, width*width), Areas: make([]uint8, width*width)}
	for y := 0; y < width; y++ {
		for x := 0; x < width; x++ {
			i := x + y*width
			chf.Cells[i] = CompactCell{Index: uint32(i), Count: 1}
			chf.Areas[i] = WalkableArea
			for dir := 0; dir < 4; dir++ {
				nx, ny := x+DirOffsetX(dir), y+DirOffsetZ(dir)
				connection := 0
				if nx < 0 || nx >= width || ny < 0 || ny >= width {
					connection = notConnected
				}
				SetCon(&chf.Spans[i], dir, connection)
			}
		}
	}
	BuildDistanceField(context.Background(), chf)
	if chf.MaxDistance != 4 {
		t.Fatalf("maximum unblurred distance = %d, want 4", chf.MaxDistance)
	}
	for y := 0; y < width; y++ {
		for x := 0; x < width; x++ {
			want := uint16(2)
			if x == 0 || y == 0 || x == width-1 || y == width-1 {
				want = 0
			}
			if got := chf.Dist[x+y*width]; got != want {
				t.Fatalf("blurred distance at (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
}
