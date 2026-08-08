package main

import (
	"log"
	"fmt"
	"os"
	"strings"

	"webcrawler/internal/analyzer"
	"webcrawler/internal/embedder"
	"webcrawler/internal/search"
	"webcrawler/internal/store"
)

func main() {
	if len(os.Args)<2{
		log.Fatalf("Usage: %s <query>", os.Args[0])
	}

	question := strings.Join(os.Args[1:], " ") //joins all command line arguments into a single string, separated by spaces
	embed, err := embedder.New()
	if err != nil {
		log.Fatalf("failed to init embedder: %v", err)
	}
		analyze := analyzer.New()

	db, err := store.New("crawler.db")
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}

	defer db.Close() // because we are done with the database, we close it to free up resources

	results, err := search.TopK(question, 5, embed, db)
	if err != nil {
		log.Fatalf("search failed: %v", err)
	}

	if len(results)==0{
		fmt.Println("No results found.")
		return
	}


	var context []string
	for _, r := range results {
		context = append(context, fmt.Sprintf("[%s]\n%s", r.Chunk.URL, r.Chunk.Text))
	}

	answer, err := analyze.Answer(question, context)
	if err != nil {
		log.Fatalf("failed to generate answer: %v", err)
	}

	fmt.Println(answer)
}