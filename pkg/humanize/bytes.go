package humanize

import (
	"math/bits"
	"strconv"
	"strings"
)

// Bytes produces a human-readable representation of a size in bytes.
// This is based on "go-humanize" library (https://github.com/dustin/go-humanize) but 40x faster

var sizes = [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

var smallBytes = [...]string{"0 B", "1 B", "2 B", "3 B", "4 B", "5 B", "6 B", "7 B", "8 B", "9 B"}

// IBytes produces a human-readable representation of an IEC size.
// IBytes(82854982) -> 79 MiB
func IBytes(s uint64) string {
	if s < 10 {
		return smallBytes[s]
	}

	// Find exponent using bit length.
	// 1024 = 2^10, so divide bit position by 10.
	e := (bits.Len64(s) - 1) / 10
	if e >= len(sizes) {
		e = len(sizes) - 1
	}

	// Calculate value: s / 1024^e using bit shift.
	divisor := uint64(1) << (10 * e)
	val := float64(s) / float64(divisor)

	// Round to 1 decimal place.
	val = float64(int64(val*10+0.5)) / 10

	suffix := sizes[e]

	var sb strings.Builder
	sb.Grow(12)

	if val < 10 {
		sb.WriteString(strconv.FormatFloat(val, 'f', 1, 64))
	} else {
		sb.WriteString(strconv.FormatFloat(val, 'f', 0, 64))
	}

	sb.WriteByte(' ')
	sb.WriteString(suffix)

	return sb.String()
}
