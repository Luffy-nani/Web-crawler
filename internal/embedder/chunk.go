package embedder

import "strings"

func ChunkText(text string, chunkSize int, overlapSize int) []string {
	words := strings.Fields(text)
	var chunks []string

	for i := 0; i < len(words); i += chunkSize - overlapSize {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))

		if end == len(words) {
			break 
		}
	}
	return chunks
}