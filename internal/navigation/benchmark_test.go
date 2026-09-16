package navigation

import "testing"

func BenchmarkShortest(b *testing.B) {
	for _, blocked := range []bool{false, true} {
		name := "open"
		if blocked {
			name = "wall_with_gap"
		}
		b.Run(name, func(b *testing.B) {
			g := New(41, 32)
			if blocked {
				for y := 0; y < 31; y++ {
					g.Block[y*41+20] = true
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if path := g.Shortest(0, []int{40}); len(path) == 0 {
					b.Fatal("expected reachable goal")
				}
			}
		})
	}
}
