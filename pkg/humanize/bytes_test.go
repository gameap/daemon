package humanize

import (
	"testing"
)

func TestIBytes(t *testing.T) {
	tests := []struct {
		input    uint64
		expected string
	}{
		// Small values (< 10 bytes)
		{0, "0 B"},
		{1, "1 B"},
		{9, "9 B"},

		// Bytes
		{10, "10 B"},
		{100, "100 B"},
		{999, "999 B"},

		// KiB
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{10240, "10 KiB"},
		{102400, "100 KiB"},

		// MiB
		{1048576, "1.0 MiB"},
		{1572864, "1.5 MiB"},
		{82854982, "79 MiB"},
		{104857600, "100 MiB"},

		// GiB
		{1073741824, "1.0 GiB"},
		{1610612736, "1.5 GiB"},
		{10737418240, "10 GiB"},

		// TiB
		{1099511627776, "1.0 TiB"},
		{1649267441664, "1.5 TiB"},

		// PiB
		{1125899906842624, "1.0 PiB"},

		// EiB
		{1152921504606846976, "1.0 EiB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := IBytes(tt.input)
			if result != tt.expected {
				t.Errorf("IBytes(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func BenchmarkIBytes(b *testing.B) {
	benchmarks := []struct {
		name  string
		input uint64
	}{
		{"SmallBytes", 5},
		{"Bytes", 500},
		{"KiB", 102400},
		{"MiB", 104857600},
		{"GiB", 10737418240},
		{"TiB", 1099511627776},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				IBytes(bm.input)
			}
		})
	}
}
