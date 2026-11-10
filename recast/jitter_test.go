package recast

import "testing"

func TestDetailJitterUsesFloatingPointFractions(t *testing.T) {
	if getJitterX(0) != -1 || getJitterY(0) != -1 {
		t.Fatal("zero hash changed")
	}
	for i := 1; i < 100; i++ {
		x, y := getJitterX(i), getJitterY(i)
		if x <= -1 || x >= 1 || y <= -1 || y >= 1 {
			t.Fatalf("jitter lost fractional part for %d: %g,%g", i, x, y)
		}
	}
}
