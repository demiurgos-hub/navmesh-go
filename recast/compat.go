package recast

// Local integer helpers keep the translated algorithms usable with Go 1.20.
func min[T ~int | ~float32](a, b T) T {
	if a < b {
		return a
	}
	return b
}
func max[T ~int | ~float32](a, b T) T {
	if a > b {
		return a
	}
	return b
}
