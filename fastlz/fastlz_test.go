package fastlz

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestRoundTripLevelsAndLengths(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	for _, level := range []int{1, 2} {
		for _, n := range []int{1, 2, 3, 4, 7, 12, 15, 16, 31, 32, 33, 255, 1024, 65535, 65536, 100000} {
			for _, repeat := range []bool{false, true} {
				input := make([]byte, n)
				if repeat {
					for i := range input {
						input[i] = byte(i % 17)
					}
				} else {
					_, _ = rng.Read(input)
				}
				compressed := make([]byte, MaxCompressedSize(n))
				size := Fastlz_compress_level(level, input, n, compressed)
				if size <= 0 {
					t.Fatalf("compress level=%d size=%d", level, n)
				}
				out := make([]byte, n)
				got := Fastlz_decompress(compressed, size, out, n)
				if got != n || !bytes.Equal(input, out) {
					t.Fatalf("roundtrip level=%d n=%d repeated=%v got=%d", level, n, repeat, got)
				}
			}
		}
	}
}

func TestRejectMalformedBuffers(t *testing.T) {
	for _, input := range [][]byte{nil, {}, {255}, {0}, {31, 1}, {0, 1, 255}, {0, 1, 224}} {
		out := make([]byte, 256)
		if n := Fastlz_decompress(input, len(input), out, len(out)); n != 0 {
			t.Fatalf("accepted %v: %d", input, n)
		}
	}
	if Fastlz_compress([]byte{1}, 2, make([]byte, 100)) != 0 {
		t.Fatal("accepted input length")
	}
	if Fastlz_decompress([]byte{0, 1}, 2, make([]byte, 1), 2) != 0 {
		t.Fatal("accepted output limit")
	}
}

func FuzzDecompress(f *testing.F) {
	f.Add([]byte{0, 1})
	f.Add([]byte{255})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65536 {
			t.Skip()
		}
		out := make([]byte, 65536)
		n := Fastlz_decompress(b, len(b), out, len(out))
		if n < 0 || n > len(out) {
			t.Fatal(n)
		}
	})
}
