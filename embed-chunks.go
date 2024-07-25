package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
	_ "github.com/tursodatabase/go-libsql"
	"log"
	"os"
	"sync"
)

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Connect to SQLite database
	db, err := loadDb()
	if err != nil {
		log.Fatal("❌ Error in loading db: err")
	}

	// Create embeddings table if it doesn't exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS embeddings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chunk_id INTEGER,
		embedding TEXT,
		FOREIGN KEY (chunk_id) REFERENCES chunks(id)
	)`)
	if err != nil {
		log.Fatal("❌ Error in creating table: ", err)
	}

	chunks, err := getChunksFromDB()

	if err != nil {
		log.Fatal("❌ Error in getting chunks from db: ", err)
	}

	// Initialize OpenAI client
	ai := openai.NewClient(os.Getenv("OPENAI_API_KEY"))

	log.Printf("✅ Queried from the DB: %d results ", len(chunks))

	// Set up concurrency
	numWorkers := 11 // Adjust this number based on your needs and API limits
	chunkChan := make(chan ChunkContent, len(chunks))
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go embedWorker(ai, db, chunkChan, &wg)
	}

	// Send chunks to workers
	for _, chunk := range chunks {
		chunkChan <- chunk
	}
	close(chunkChan)

	// Wait for all workers to finish
	wg.Wait()

	fmt.Println("✅ All chunks processed and embeddings stored.")
}

func embedWorker(ai *openai.Client, db *sql.DB, chunkChan <-chan ChunkContent, wg *sync.WaitGroup) {
	defer wg.Done()

	for chunk := range chunkChan {
		// Create embedding using OpenAI API
		resp, err := ai.CreateEmbeddings(
			context.Background(),
			openai.EmbeddingRequest{
				Input: []string{chunk.Chunk},
				Model: openai.AdaEmbeddingV2,
			},
		)
		if err != nil {
			log.Printf("Error creating embedding for chunk %d: %v", chunk.ID, err)
			continue
		}

		// Convert embedding to JSON string
		embeddingJSON, err := json.Marshal(resp.Data[0].Embedding)
		if err != nil {
			log.Printf("Error marshaling embedding for chunk %d: %v", chunk.ID, err)
			continue
		}

		// Store embedding in the database
		_, err = db.Exec("INSERT INTO embeddings (chunk_id, embedding) VALUES (?, ?)", chunk.ID, string(embeddingJSON))
		if err != nil {
			log.Printf("Error storing embedding for chunk %d: %v", chunk.ID, err)
			continue
		}

		fmt.Printf("> ✅ Processed chunk %d\n", chunk.ID)
	}
}
