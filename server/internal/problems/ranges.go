package problems

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParseRanges reads a test list such as "3-5, 8" into sorted, distinct
// numbers from 1 to MaxTests.
func ParseRanges(s string) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b := a
		var err2 error
		if isRange {
			b, err2 = strconv.Atoi(strings.TrimSpace(hi))
		}
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%q is not a test number or a range like 3-5.", part)
		}
		if a < 1 || b > MaxTests || a > b {
			return nil, fmt.Errorf("%q must run upwards between 1 and %d.", part, MaxTests)
		}
		for n := a; n <= b; n++ {
			if seen[n] {
				return nil, errors.New("a test is listed twice.")
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out, nil
}

// FormatRanges writes sorted numbers in the shortest range form: "1-3,5".
func FormatRanges(nums []int) string {
	var parts []string
	for i := 0; i < len(nums); {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		if j == i {
			parts = append(parts, strconv.Itoa(nums[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", nums[i], nums[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}
