# Beacon

[![Go Reference](https://pkg.go.dev/badge/github.com/Edartruwu/beacon.svg)](https://pkg.go.dev/github.com/Edartruwu/beacon)
[![Go Report Card](https://goreportcard.com/badge/github.com/Edartruwu/beacon)](https://goreportcard.com/report/github.com/Edartruwu/beacon)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**A guiding light for your search needs. `beacon` is a powerful, hybrid search library for Go, designed to deliver intuitive and relevant results.**

`beacon` acts as a lighthouse for your data, cutting through the noise to guide users to the most relevant items. It combines multiple search algorithms into a sophisticated, weighted scoring model, providing fault-tolerant search that feels intelligent and natural.

Built with Go generics, `beacon` is type-safe and can be easily integrated into any project to search over slices of your custom structs.

## Key Features

- **Hybrid Scoring Model:** Combines exact match, prefix match, word match, substring, n-grams (bigram & trigram), Levenshtein distance, and acronym matching for nuanced and accurate ranking.
- **Fuzzy & Fault-Tolerant:** Finds results even with typos, partial words, or different word orders.
- **Type-Safe with Go Generics:** Works with any of your data structures by simply implementing the `Searchable` interface.
- **Fast Pre-computation:** Indexes your data on initialization for high-performance searching.
- **Customizable Filtering:** Allows pre-filtering of the search space based on your own logic before scoring begins.
- **Transparent Debugging:** An optional debug mode provides detailed insight into the scoring components for every result.
- **Simple API:** A clean and straightforward API makes `beacon` easy to integrate and use.

## Installation

```sh
go get github.com/Edartruwu/beacon
```

## Quick Start

Here's how to get up and running with `beacon` in just a few steps.

### 1. Define Your Searchable Type

First, create a struct for the items you want to search and implement the `beacon.Searchable` interface.

```go
package main

import "fmt"

// Book represents our searchable item.
type Book struct {
	ID     int
	Title  string
	Author string
	Genre  string
}

// GetSearchText returns the primary text to search against (the book's title).
func (b Book) GetSearchText() string {
	return b.Title
}

// GetSearchFields returns additional data for filtering.
func (b Book) GetSearchFields() map[string]string {
	return map[string]string{
		"author": b.Author,
		"genre":  b.Genre,
	}
}
```

### 2. Create and Use the Searcher

Now, create a `beacon` searcher, add your data, and perform a search.

```go
package main

import (
	"fmt"
	"github.com/Edartruwu/beacon"
)

// ... (Book struct and interface methods from above)

func main() {
	// 1. Your collection of items
	books := []Book{
		{ID: 1, Title: "The Go Programming Language", Author: "Alan Donovan", Genre: "Tech"},
		{ID: 2, Title: "Structure and Interpretation of Computer Programs", Author: "Harold Abelson", Genre: "Tech"},
		{ID: 3, Title: "The Lord of the Rings", Author: "J.R.R. Tolkien", Genre: "Fantasy"},
		{ID: 4, Title: "A Game of Thrones", Author: "George R.R. Martin", Genre: "Fantasy"},
	}

	// 2. Create a new searcher
	// NewSearcher(items, minSimilarity, debugMode)
	// minSimilarity is a float64 (0.0 to 1.0) to filter out low-quality matches. 0.3 is a good start.
	searcher := beacon.NewSearcher[Book](books, 0.3, false)

	// 3. Perform a search
	query := "go lang"
	topN := 3 // Request the top 3 results
	req := beacon.SearchRequest[Book]{
		Query: query,
		TopN:  &topN,
	}

	// The searcher returns an interface{} and an error.
	// You need to type-assert the result to either MultiSearchResponse or SearchResponse.
	result, err := searcher.Search(req)
	if err != nil {
		panic(err)
	}

	// 4. Process the results
	if multiResponse, ok := result.(beacon.MultiSearchResponse[Book]); ok {
		fmt.Printf("Found %d results for '%s':\n", len(multiResponse.Results), query)
		for _, res := range multiResponse.Results {
			fmt.Printf(
				"  - ID: %d, Title: '%s' (Similarity: %.2f%%)\n",
				res.Item.ID,
				res.Item.GetSearchText(),
				res.Similarity*100,
			)
		}
	} else if singleResponse, ok := result.(beacon.SearchResponse[Book]); ok {
		// This block handles cases where TopN is 1 or omitted.
		if singleResponse.Error != "" {
			fmt.Printf("Search failed: %s\n", singleResponse.Error)
		} else {
			fmt.Printf(
				"Found best match for '%s': '%s' (Similarity: %.2f%%)\n",
				query,
				singleResponse.Item.GetSearchText(),
				singleResponse.Similarity*100,
			)
		}
	}
}
```

**Output:**

```
Found 1 results for 'go lang':
  - ID: 1, Title: 'The Go Programming Language' (Similarity: 88.54%)
```

## Advanced Usage

### Custom Filtering

You can narrow the search space by providing a custom `Filter`. A filter is any struct that implements the `beacon.Filter` interface.

```go
// GenreFilter will only match books of a specific genre.
type GenreFilter struct {
	TargetGenre string
}

func (f GenreFilter) Matches(item beacon.Searchable) bool {
	// GetSearchFields is the method we defined on our Book struct.
	if genre, ok := item.GetSearchFields()["genre"]; ok {
		return genre == f.TargetGenre
	}
	return false
}

func (f GenreFilter) Description() string {
	return fmt.Sprintf("genre is '%s'", f.TargetGenre)
}

// --- In your main function ---
// Now, let's search for "program" but only in the "Fantasy" genre.
fantasyFilter := GenreFilter{TargetGenre: "Fantasy"}
reqWithFilter := beacon.SearchRequest[Book]{
	Query:   "program",
	Filters: fantasyFilter,
}

result, _ = searcher.Search(reqWithFilter)

// This search will now fail to find "Structure and Interpretation of Computer Programs"
// because it will be filtered out before scoring.
if response, ok := result.(beacon.SearchResponse[Book]); ok && response.Error != "" {
    fmt.Println(response.Error)
}
```

**Output:**

```
No items found matching filter: genre is 'Fantasy'
```

### Debugging a Search

To understand _why_ an item received a certain score, you can enable debug mode. This can be done globally on the searcher or on a per-request basis.

```go
// Enable debug mode for a single request
debug := true
reqDebug := beacon.SearchRequest[Book]{
	Query: "lord rings",
	Debug: &debug,
}

result, _ = searcher.Search(reqDebug)

if res, ok := result.(beacon.SearchResponse[Book]); ok {
    // Use a JSON marshaller for pretty printing the debug info
    // debugJSON, _ := json.MarshalIndent(res.Debug, "", "  ")
    // fmt.Println(string(debugJSON))
    fmt.Printf("Debug Info for '%s':\n", res.Item.Title)
    fmt.Printf("  - Normalized Query: %s\n", res.Debug.QueryNormalized)
    fmt.Printf("  - Final Score: %.4f\n", res.Debug.ScoreComponents.FinalScore)
    fmt.Printf("  - Word Match Score: %.4f\n", res.Debug.ScoreComponents.WordMatch)
    fmt.Printf("  - Trigram Score: %.4f\n", res.Debug.ScoreComponents.TrigramSimilarity)
    fmt.Printf("  - Levenshtein Score: %.4f\n", res.Debug.ScoreComponents.LevenshteinSim)
}
```

**Example Debug Output:**

```
Debug Info for 'The Lord of the Rings':
  - Normalized Query: lord rings
  - Final Score: 0.8911
  - Word Match Score: 1.0000
  - Trigram Score: 0.5294
  - Levenshtein Score: 0.6522
```

## How It Works: The Scoring Model

`beacon` achieves its accuracy through a two-stage process:

1.  **Indexing:** When you create a `NewSearcher`, `beacon` pre-processes each item. It normalizes the text, splits it into words, and generates character n-grams (bigrams and trigrams). This `SearchIndex` is stored in memory for fast access.

2.  **Scoring:** When a search is performed, `beacon` calculates a score for each candidate item against the query. This isn't a single calculation, but a weighted average of several metrics:
    - **Exact Match:** The highest possible score.
    - **Prefix Match:** If the item's text starts with the query.
    - **Word Match:** How many query words are present in the item's text.
    - **Substring Match:** If the query appears as a substring.
    - **N-Gram Similarity:** Jaccard similarity of character trigrams and bigrams. This is excellent for catching typos and related words.
    - **Acronym Match:** Matches queries like "LOTR" to "The Lord of the Rings".
    - **Levenshtein Similarity:** A normalized score based on the edit distance between the query and the text.

The final score is a weighted sum of these components, which is then normalized. This hybrid approach ensures that results are ranked in a way that feels intuitive to a human user.

## Contributing

Contributions are welcome! If you find a bug, have a feature request, or want to improve the code, please feel free to open an issue or submit a pull request.

## License

`beacon` is licensed under the [MIT License](LICENSE).
