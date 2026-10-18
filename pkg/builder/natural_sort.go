package builder

import (
	"strconv"
	"strings"
	"unicode"
)

// NaturalLess compares two strings using natural/alphanumeric sort order.
// Embedded numbers are compared by numerical value so that "chapter 2" < "chapter 10".
func NaturalLess(a, b string) bool {
	chunksA := splitChunks(a)
	chunksB := splitChunks(b)

	minLen := len(chunksA)
	if len(chunksB) < minLen {
		minLen = len(chunksB)
	}

	for i := 0; i < minLen; i++ {
		ca, cb := chunksA[i], chunksB[i]
		isDigitA := isAllDigits(ca)
		isDigitB := isAllDigits(cb)

		if isDigitA && isDigitB {
			numA, errA := strconv.ParseUint(ca, 10, 64)
			numB, errB := strconv.ParseUint(cb, 10, 64)
			if errA == nil && errB == nil {
				if numA != numB {
					return numA < numB
				}
				if len(ca) != len(cb) {
					return len(ca) < len(cb)
				}
			} else {
				cmp := strings.Compare(ca, cb)
				if cmp != 0 {
					return cmp < 0
				}
			}
		} else {
			cmp := strings.Compare(strings.ToLower(ca), strings.ToLower(cb))
			if cmp != 0 {
				return cmp < 0
			}
		}
	}

	return len(chunksA) < len(chunksB)
}

func splitChunks(s string) []string {
	var chunks []string
	if s == "" {
		return chunks
	}

	runes := []rune(s)
	start := 0
	isDigit := unicode.IsDigit(runes[0])

	for i := 1; i < len(runes); i++ {
		curDigit := unicode.IsDigit(runes[i])
		if curDigit != isDigit {
			chunks = append(chunks, string(runes[start:i]))
			start = i
			isDigit = curDigit
		}
	}
	chunks = append(chunks, string(runes[start:]))
	return chunks
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
