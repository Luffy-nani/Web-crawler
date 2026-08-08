package search

import (
	"sort"
	"math"
	"webcrawler/internal/embedder"
	"webcrawler/internal/store"
)

type ScoredChunk struct {
	Chunk store.Chunk
	Score float64
}

// TopK embeds the query, scores it against every stored chunk, and
// returns the topK most similar ones, highest score first.
func TopK(query string, topK int, embed *embedder.Embedder, db *store.Store) ([]ScoredChunk, error) {
	vectors, err := embed.GenerateEmbeddings([]string{query}, "query")
	if err != nil {
		return nil, err
	}
	queryVec := vectors[0]

	chunks, err := db.GetAllChunks()
	if err != nil {
		return nil, err
	}

	scored := make([]ScoredChunk, len(chunks))
	for i, c := range chunks {
		scored[i] = ScoredChunk{
			Chunk: c,
			Score: cosineSimilarity(queryVec, c.Embedding),
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > topK {
		scored = scored[:topK]
	}
	return scored, nil
}


func cosineSimilarity(a, b []float32) float64 {
	var dotProduct, normA, normB float64

	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0 // guard against divide-by-zero for a zero vector
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}