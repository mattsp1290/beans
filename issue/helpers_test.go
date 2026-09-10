package issue

import "strings"

// lineDiff computes a minimal line-level diff between a and b using a
// classic LCS alignment. It returns the lines present in a but not in the
// common subsequence (removed) and the lines present in b but not in the
// common subsequence (added). It is intended for asserting that a mutation
// touched only the expected lines of a frontmatter/body text.
func lineDiff(a, b string) (removed, added []string) {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	n, m := len(al), len(bl)

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	i, j := 0, 0
	for i < n && j < m {
		switch {
		case al[i] == bl[j]:
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			removed = append(removed, al[i])
			i++
		default:
			added = append(added, bl[j])
			j++
		}
	}
	for ; i < n; i++ {
		removed = append(removed, al[i])
	}
	for ; j < m; j++ {
		added = append(added, bl[j])
	}
	return removed, added
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
