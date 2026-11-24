package app

import (
	"strconv"
	"strings"
	"unicode"
)

// fuzzy reports whether the runes of q appear in s, in order.
func fuzzy(s, q string) bool {
	_, _, ok := fuzzyScore(s, q)
	return ok
}

// fuzzyScore scores a case-insensitive subsequence match of query inside text,
// returning the score and the matched rune indices in text.
func fuzzyScore(text, query string) (int, []int, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return 0, nil, true
	}
	tr := []rune(text)
	qr := []rune(strings.ToLower(q))
	if len(qr) == 0 {
		return 0, nil, true
	}
	if len(qr) > len(tr) {
		return 0, nil, false
	}

	lowText := strings.ToLower(text)
	lowQuery := strings.ToLower(q)

	// Fast greedy pass first to verify match and collect initial positions.
	pos := make([]int, 0, len(qr))
	qi := 0
	for i, r := range tr {
		if qi < len(qr) && unicode.ToLower(r) == qr[qi] {
			pos = append(pos, i)
			qi++
		}
	}
	if qi != len(qr) {
		return 0, nil, false
	}

	// Prefer an exact contiguous substring match when one exists so its
	// highlighted positions match the contiguous occurrence.
	if byteIdx := strings.Index(lowText, lowQuery); byteIdx >= 0 {
		runeStart := len([]rune(lowText[:byteIdx]))
		if runeStart+len(qr) <= len(tr) {
			for i := range qr {
				pos[i] = runeStart + i
			}
		}
	}

	score := len(qr) * 10
	if strings.HasPrefix(lowText, lowQuery) {
		score += 20
	} else if strings.Contains(lowText, lowQuery) {
		score += 10
	}
	for i, idx := range pos {
		if i > 0 && idx == pos[i-1]+1 {
			score += 12
		}
		if isMatchBoundary(tr, idx) {
			score += 15
		}
	}
	score -= pos[0]
	score -= len(tr) / 4
	return score, pos, true
}

func isMatchBoundary(tr []rune, idx int) bool {
	if idx <= 0 {
		return true
	}
	prev := tr[idx-1]
	cur := tr[idx]
	switch prev {
	case '/', '\\', '_', '-', '.', ' ', ':':
		return true
	}
	return unicode.IsLower(prev) && unicode.IsUpper(cur)
}

// scoreFileMatch scores query against a file's title and project-relative path.
func scoreFileMatch(title, rel, query string) (int, []int, []int, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return 0, nil, nil, true
	}
	tScore, tPos, tOK := fuzzyScore(title, q)
	rScore, rPos, rOK := fuzzyScore(rel, q)
	switch {
	case tOK && rOK:
		best := max(tScore+35, rScore)
		return best, tPos, rPos, true
	case tOK:
		return tScore + 35, tPos, nil, true
	case rOK:
		return rScore, nil, rPos, true
	default:
		if cScore, _, cOK := fuzzyScore(title+" "+rel, q); cOK {
			return cScore - 10, nil, nil, true
		}
		return 0, nil, nil, false
	}
}

// parseFileLineQuery splits an optional :line or :line:col suffix from a Quick Open query.
func parseFileLineQuery(raw string) (fileQuery string, line, col int, hasLine bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", 0, 0, false
	}
	last := strings.LastIndexByte(s, ':')
	if last < 0 {
		return s, 0, 0, false
	}
	v1, err1 := strconv.Atoi(strings.TrimSpace(s[last+1:]))
	if err1 != nil || v1 <= 0 {
		return s, 0, 0, false
	}
	prefix := s[:last]
	if prev := strings.LastIndexByte(prefix, ':'); prev >= 0 {
		if v2, err2 := strconv.Atoi(strings.TrimSpace(prefix[prev+1:])); err2 == nil && v2 > 0 {
			return strings.TrimSpace(prefix[:prev]), v2, v1, true
		}
	}
	return strings.TrimSpace(prefix), v1, 0, true
}
