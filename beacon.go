package beacon

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Searchable defines the interface that items must implement to be searchable
type Searchable interface {
	// GetSearchText returns the primary text to search against
	GetSearchText() string
	// GetSearchFields returns additional fields that can be used for filtering
	// The map keys are field names and values are the field values
	GetSearchFields() map[string]string
}

// Filter represents filtering criteria for search
type Filter interface {
	// Matches returns true if the item matches the filter criteria
	Matches(item Searchable) bool
	// Description returns a human-readable description of the filter
	Description() string
}

// SearchRequest represents the incoming search request
type SearchRequest[T Searchable] struct {
	Query   string `json:"query"`
	Filters Filter `json:"filters,omitempty"`
	TopN    *int   `json:"topN,omitempty"`  // Number of results to return (1-10)
	Debug   *bool  `json:"debug,omitempty"` // Enable debug information
}

// SearchResponse represents the response structure for a single result
type SearchResponse[T Searchable] struct {
	Item       *T         `json:"item,omitempty"`
	Similarity float64    `json:"similarity,omitempty"`
	Message    string     `json:"message,omitempty"`
	Error      string     `json:"error,omitempty"`
	Debug      *DebugInfo `json:"debug,omitempty"`
}

// MultiSearchResponse represents the response for multiple results
type MultiSearchResponse[T Searchable] struct {
	Results    []SearchResponse[T] `json:"results"`
	Query      string              `json:"query"`
	TotalFound int                 `json:"totalFound"`
	Filters    Filter              `json:"filters,omitempty"`
	Message    string              `json:"message"`
}

// DebugInfo provides insight into the search process
type DebugInfo struct {
	QueryNormalized    string           `json:"queryNormalized"`
	ScoreComponents    *ScoreComponents `json:"scoreComponents,omitempty"`
	FilteredCandidates int              `json:"filteredCandidates,omitempty"`
	TotalCandidates    int              `json:"totalCandidates"`
}

// ScoreComponents tracks individual scoring components for debugging
type ScoreComponents struct {
	ExactMatch        float64 `json:"exactMatch"`
	PrefixMatch       float64 `json:"prefixMatch"`
	WordMatch         float64 `json:"wordMatch"`
	SubstringMatch    float64 `json:"substringMatch"`
	TrigramSimilarity float64 `json:"trigramSimilarity"`
	BigramSimilarity  float64 `json:"bigramSimilarity"`
	AcronymMatch      float64 `json:"acronymMatch"`
	LevenshteinSim    float64 `json:"levenshteinSim"`
	FinalScore        float64 `json:"finalScore"`
}

// ImprovedSearcher combines multiple search techniques for optimal results
type ImprovedSearcher[T Searchable] struct {
	items         []T
	searchIndex   []SearchIndex[T]
	debugMode     bool
	minSimilarity float64
}

// SearchIndex contains pre-computed search data for each item
type SearchIndex[T Searchable] struct {
	Item           T
	NormalizedText string
	LowercaseText  string
	Words          []string        // Individual words
	WordSet        map[string]bool // Set of words for fast lookup
	Trigrams       map[string]bool // Character trigrams
	Bigrams        map[string]bool // Character bigrams
	FirstLetters   string          // First letter of each word
	Acronym        string          // Acronym from capitalized words
	TextLength     int
}

// NewImprovedSearcher creates a new searcher with enhanced indexing
func NewImprovedSearcher[T Searchable](items []T, minSimilarity float64, debugMode bool) *ImprovedSearcher[T] {
	searchIndex := make([]SearchIndex[T], len(items))

	for i, item := range items {
		searchText := item.GetSearchText()
		normalizedText := normalizeText(searchText)
		lowercaseText := strings.ToLower(normalizedText)
		words := extractWords(lowercaseText)

		searchIndex[i] = SearchIndex[T]{
			Item:           item,
			NormalizedText: normalizedText,
			LowercaseText:  lowercaseText,
			Words:          words,
			WordSet:        createWordSet(words),
			Trigrams:       createCharNgrams(lowercaseText, 3),
			Bigrams:        createCharNgrams(lowercaseText, 2),
			FirstLetters:   extractFirstLetters(words),
			Acronym:        extractAcronym(searchText),
			TextLength:     len(lowercaseText),
		}
	}

	return &ImprovedSearcher[T]{
		items:         items,
		searchIndex:   searchIndex,
		debugMode:     debugMode,
		minSimilarity: minSimilarity,
	}
}

// Search performs the improved search with multiple algorithms
func (is *ImprovedSearcher[T]) Search(req SearchRequest[T]) (interface{}, error) {
	if strings.TrimSpace(req.Query) == "" {
		return SearchResponse[T]{Error: "Query cannot be empty"}, nil
	}

	// Prepare query
	normalizedQuery := normalizeText(req.Query)
	lowercaseQuery := strings.ToLower(normalizedQuery)
	queryWords := extractWords(lowercaseQuery)
	queryWordSet := createWordSet(queryWords)
	queryTrigrams := createCharNgrams(lowercaseQuery, 3)
	queryBigrams := createCharNgrams(lowercaseQuery, 2)
	queryAcronym := extractAcronym(req.Query)

	// Filter candidates if filter is provided
	candidates := is.filterItems(req.Filters)

	if len(candidates) == 0 {
		message := "No items found matching the filter criteria"
		if req.Filters != nil {
			message = fmt.Sprintf("No items found matching filter: %s", req.Filters.Description())
		}
		return SearchResponse[T]{Error: message}, nil
	}

	// Score all candidates
	type scoredResult struct {
		index      *SearchIndex[T]
		score      float64
		components ScoreComponents
	}

	results := make([]scoredResult, 0, len(candidates))

	for i := range candidates {
		idx := &candidates[i]
		components := is.calculateScore(
			idx,
			lowercaseQuery,
			queryWords,
			queryWordSet,
			queryTrigrams,
			queryBigrams,
			queryAcronym,
		)

		if components.FinalScore >= is.minSimilarity*0.5 { // Lower threshold for initial filtering
			results = append(results, scoredResult{
				index:      idx,
				score:      components.FinalScore,
				components: components,
			})
		}
	}

	// Sort by score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Prepare response
	topN := 1
	if req.TopN != nil {
		topN = *req.TopN
		if topN < 1 {
			topN = 1
		} else if topN > 10 {
			topN = 10
		}
	}

	if topN > len(results) {
		topN = len(results)
	}

	if topN == 0 {
		return SearchResponse[T]{
			Error: fmt.Sprintf("No matches found for '%s'", req.Query),
		}, nil
	}

	// Return multiple results if requested
	if topN > 1 {
		responses := make([]SearchResponse[T], topN)
		for i := 0; i < topN; i++ {
			result := results[i]
			responses[i] = SearchResponse[T]{
				Item:       &result.index.Item,
				Similarity: result.score,
				Message:    fmt.Sprintf("Rank %d with %.2f%% similarity", i+1, result.score*100),
			}

			if is.debugMode || (req.Debug != nil && *req.Debug) {
				responses[i].Debug = &DebugInfo{
					QueryNormalized:    normalizedQuery,
					ScoreComponents:    &result.components,
					FilteredCandidates: len(candidates),
					TotalCandidates:    len(is.searchIndex),
				}
			}
		}

		filterDesc := ""
		if req.Filters != nil {
			filterDesc = fmt.Sprintf(" with filter: %s", req.Filters.Description())
		}

		return MultiSearchResponse[T]{
			Results:    responses,
			Query:      req.Query,
			TotalFound: len(candidates),
			Filters:    req.Filters,
			Message:    fmt.Sprintf("Found %d items%s matching '%s'", len(candidates), filterDesc, req.Query),
		}, nil
	}

	// Return single best result
	best := results[0]
	response := SearchResponse[T]{
		Item:       &best.index.Item,
		Similarity: best.score,
		Message:    fmt.Sprintf("Found item with %.2f%% similarity", best.score*100),
	}

	if is.debugMode || (req.Debug != nil && *req.Debug) {
		response.Debug = &DebugInfo{
			QueryNormalized:    normalizedQuery,
			ScoreComponents:    &best.components,
			FilteredCandidates: len(candidates),
			TotalCandidates:    len(is.searchIndex),
		}
	}

	return response, nil
}

// calculateScore computes a weighted score using multiple algorithms
func (is *ImprovedSearcher[T]) calculateScore(
	idx *SearchIndex[T],
	queryLower string,
	queryWords []string,
	queryWordSet map[string]bool,
	queryTrigrams map[string]bool,
	queryBigrams map[string]bool,
	queryAcronym string,
) ScoreComponents {
	components := ScoreComponents{}

	// 1. Exact match (highest priority)
	if idx.LowercaseText == queryLower {
		components.ExactMatch = 1.0
	}

	// 2. Prefix match
	if strings.HasPrefix(idx.LowercaseText, queryLower) {
		components.PrefixMatch = 0.9
	} else if len(queryLower) >= 3 && strings.HasPrefix(idx.LowercaseText, queryLower[:3]) {
		components.PrefixMatch = 0.5
	}

	// 3. Word-level matching
	matchedWords := 0
	totalWords := len(queryWords)

	// Check each query word against item words
	for _, qWord := range queryWords {
		for _, sWord := range idx.Words {
			if sWord == qWord {
				matchedWords++
				break
			} else if len(qWord) >= 3 && strings.HasPrefix(sWord, qWord) {
				matchedWords++
				break
			}
		}
	}

	if totalWords > 0 {
		components.WordMatch = float64(matchedWords) / float64(totalWords)

		// Bonus for matching all words
		if matchedWords == totalWords {
			components.WordMatch = math.Min(components.WordMatch*1.2, 1.0)
		}
	}

	// 4. Substring match
	if strings.Contains(idx.LowercaseText, queryLower) {
		lengthRatio := float64(len(queryLower)) / float64(idx.TextLength)
		components.SubstringMatch = 0.7 + (0.3 * lengthRatio)
	}

	// 5. N-gram similarity
	components.TrigramSimilarity = calculateJaccardSimilarity(queryTrigrams, idx.Trigrams)
	components.BigramSimilarity = calculateJaccardSimilarity(queryBigrams, idx.Bigrams)

	// 6. Acronym matching
	if queryAcronym != "" && queryAcronym == idx.Acronym {
		components.AcronymMatch = 0.8
	}

	// 7. Levenshtein distance (normalized)
	levDist := calculateLevenshteinDistance(queryLower, idx.LowercaseText)
	maxLen := max(len(queryLower), idx.TextLength)
	if maxLen > 0 {
		components.LevenshteinSim = 1.0 - (float64(levDist) / float64(maxLen))
	}

	// Calculate weighted final score
	weights := map[string]float64{
		"exact":       1.0,
		"prefix":      0.9,
		"word":        0.85,
		"substring":   0.75,
		"trigram":     0.6,
		"bigram":      0.4,
		"acronym":     0.7,
		"levenshtein": 0.5,
	}

	totalWeight := 0.0
	weightedSum := 0.0

	// Add weighted components
	if components.ExactMatch > 0 {
		weightedSum += components.ExactMatch * weights["exact"]
		totalWeight += weights["exact"]
	}
	if components.PrefixMatch > 0 {
		weightedSum += components.PrefixMatch * weights["prefix"]
		totalWeight += weights["prefix"]
	}
	if components.WordMatch > 0 {
		weightedSum += components.WordMatch * weights["word"]
		totalWeight += weights["word"]
	}
	if components.SubstringMatch > 0 {
		weightedSum += components.SubstringMatch * weights["substring"]
		totalWeight += weights["substring"]
	}
	if components.TrigramSimilarity > 0 {
		weightedSum += components.TrigramSimilarity * weights["trigram"]
		totalWeight += weights["trigram"]
	}
	if components.BigramSimilarity > 0 {
		weightedSum += components.BigramSimilarity * weights["bigram"]
		totalWeight += weights["bigram"]
	}
	if components.AcronymMatch > 0 {
		weightedSum += components.AcronymMatch * weights["acronym"]
		totalWeight += weights["acronym"]
	}
	if components.LevenshteinSim > 0 {
		weightedSum += components.LevenshteinSim * weights["levenshtein"]
		totalWeight += weights["levenshtein"]
	}

	// Calculate final score
	if totalWeight > 0 {
		components.FinalScore = weightedSum / totalWeight

		// Apply bonuses
		// Bonus for short queries that match exactly
		if len(queryLower) <= 10 && components.ExactMatch > 0 {
			components.FinalScore = 1.0
		}

		// Bonus for high word match with substring match
		if components.WordMatch >= 0.8 && components.SubstringMatch > 0 {
			components.FinalScore = math.Min(components.FinalScore*1.1, 1.0)
		}
	}

	return components
}

// filterItems filters items based on the provided filter
func (is *ImprovedSearcher[T]) filterItems(filter Filter) []SearchIndex[T] {
	if filter == nil {
		return is.searchIndex
	}

	var filtered []SearchIndex[T]
	for i := range is.searchIndex {
		idx := &is.searchIndex[i]
		if filter.Matches(idx.Item) {
			filtered = append(filtered, *idx)
		}
	}

	return filtered
}

// GetItemCount returns the total number of indexed items
func (is *ImprovedSearcher[T]) GetItemCount() int {
	return len(is.items)
}

// GetMinSimilarity returns the minimum similarity threshold
func (is *ImprovedSearcher[T]) GetMinSimilarity() float64 {
	return is.minSimilarity
}

// SetMinSimilarity updates the minimum similarity threshold
func (is *ImprovedSearcher[T]) SetMinSimilarity(minSimilarity float64) {
	is.minSimilarity = minSimilarity
}

// SetDebugMode enables or disables debug mode
func (is *ImprovedSearcher[T]) SetDebugMode(debug bool) {
	is.debugMode = debug
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

// normalizeText applies Unicode normalization and cleans the text
func normalizeText(text string) string {
	// Apply Unicode NFC normalization
	transformer := transform.Chain(norm.NFC)
	normalized, _, _ := transform.String(transformer, text)

	// Remove extra spaces and trim
	normalized = strings.Join(strings.Fields(normalized), " ")

	return normalized
}

// extractWords splits text into words, handling common separators
func extractWords(text string) []string {
	// Split on spaces and common separators
	words := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == '-' || r == ',' || r == '.'
	})

	// Filter out empty strings
	filtered := make([]string, 0, len(words))
	for _, word := range words {
		if word != "" {
			filtered = append(filtered, word)
		}
	}

	return filtered
}

// createWordSet creates a set from word slice for fast lookup
func createWordSet(words []string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

// createCharNgrams creates character n-grams with padding
func createCharNgrams(text string, n int) map[string]bool {
	if len(text) < n {
		return map[string]bool{text: true}
	}

	// Add padding for better edge matching
	padding := strings.Repeat("#", n-1)
	padded := padding + text + padding

	ngrams := make(map[string]bool)
	for i := 0; i <= len(padded)-n; i++ {
		ngrams[padded[i:i+n]] = true
	}

	return ngrams
}

// extractFirstLetters gets the first letter of each word
func extractFirstLetters(words []string) string {
	var letters []rune
	for _, word := range words {
		if len(word) > 0 {
			letters = append(letters, rune(word[0]))
		}
	}
	return string(letters)
}

// extractAcronym extracts acronym from uppercase letters
func extractAcronym(text string) string {
	var acronym []rune
	words := strings.Fields(text)

	for _, word := range words {
		// Skip common articles and prepositions (can be customized per language)
		lower := strings.ToLower(word)
		if lower == "de" || lower == "del" || lower == "la" || lower == "el" ||
			lower == "los" || lower == "las" || lower == "y" || lower == "e" ||
			lower == "of" || lower == "the" || lower == "and" || lower == "or" {
			continue
		}

		// Get first character if it's uppercase in original
		if len(word) > 0 && unicode.IsUpper(rune(word[0])) {
			acronym = append(acronym, rune(word[0]))
		}
	}

	return strings.ToLower(string(acronym))
}

// calculateJaccardSimilarity calculates Jaccard index between two sets
func calculateJaccardSimilarity(set1, set2 map[string]bool) float64 {
	if len(set1) == 0 && len(set2) == 0 {
		return 1.0
	}
	if len(set1) == 0 || len(set2) == 0 {
		return 0.0
	}

	intersection := 0
	for item := range set1 {
		if set2[item] {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// calculateLevenshteinDistance computes edit distance between strings
func calculateLevenshteinDistance(s1, s2 string) int {
	if s1 == s2 {
		return 0
	}

	len1, len2 := len(s1), len(s2)
	if len1 == 0 {
		return len2
	}
	if len2 == 0 {
		return len1
	}

	// Create distance matrix
	dist := make([][]int, len1+1)
	for i := range dist {
		dist[i] = make([]int, len2+1)
		dist[i][0] = i
	}
	for j := 1; j <= len2; j++ {
		dist[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len1; i++ {
		for j := 1; j <= len2; j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}

			dist[i][j] = min(
				dist[i-1][j]+1,      // deletion
				dist[i][j-1]+1,      // insertion
				dist[i-1][j-1]+cost, // substitution
			)
		}
	}

	return dist[len1][len2]
}

// Helper functions
func min(values ...int) int {
	if len(values) == 0 {
		return 0
	}

	minVal := values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
	}
	return minVal
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
