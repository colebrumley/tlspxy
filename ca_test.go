package main

import (
	"testing"
)

func BenchmarkSystemCAPool(b *testing.B) {
	for n := 0; n < b.N; n++ {
		if _, err := SystemCAPool(); err != nil {
			b.Error("Failed to load system root CA pool")
		}
	}
}
