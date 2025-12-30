package ui

import (
	"strings"

	"github.com/levineuwirth/gophermark/internal/models"
)

func levenshteinDistance(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)

	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}

	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func fuzzyMatch(query, text string) int {
	query = strings.ToLower(query)
	text = strings.ToLower(text)

	if strings.Contains(text, query) {
		return 0
	}

	distance := levenshteinDistance(query, text)

	threshold := len(query) / 2
	if threshold < 2 {
		threshold = 2
	}

	if distance <= threshold {
		return distance
	}

	return -1
}

func SearchBookmarks(root *models.Bookmark, query string) []*models.Bookmark {
	if query == "" {
		return nil
	}

	var results []*models.Bookmark

	var search func(*models.Bookmark)
	search = func(node *models.Bookmark) {
		if node.IsBookmark() {
			titleScore := fuzzyMatch(query, node.Title)
			urlScore := fuzzyMatch(query, node.URL)

			if titleScore >= 0 || urlScore >= 0 {
				results = append(results, node)
			}
		}

		for _, child := range node.Children {
			search(child)
		}
	}

	search(root)
	return results
}
