package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/sashabaranov/go-openai"
	"log"
	"os"
	"sync"
)

func main() {

	gdb, err := loadGraphDB()
	if err != nil {
		log.Fatal("❌ Error with loading graph db: ", err)
	}

	session := gdb.NewSession(context.Background(), neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(context.Background())

	ai := openai.NewClient(os.Getenv("OPENAI_API_KEY"))

	// Retrieve chunks from the database
	chunks, err := getChunksFromDB()

	// Set up concurrency
	numWorkers := 1 // Adjust this number based on your needs and API limits
	chunkChan := make(chan ChunkContent, len(chunks))
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go graphWorker(ai, session, chunkChan, &wg)
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

func graphWorker(ai *openai.Client, session neo4j.SessionWithContext,
	chunkChan <-chan ChunkContent, wg *sync.WaitGroup) {
	defer wg.Done()

	for chunk := range chunkChan {
		schema, err := getSchemaVisualization(session)
		if err != nil {
			log.Fatal("❌ Error in getting schema: ", err)
		}

		prompt := fmt.Sprintf("Given a markdown document, extract entities and relationships and create a syntacially correct Cipher query to create them. Make as many tool calls as you can\nHere is the schema information\n %s\n Here is the markdown document\n%s", schema, chunk.Chunk)

		cypherTool := getCypherTool()

		resp, err := ai.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Messages: []openai.ChatCompletionMessage{
					{
						Role:    openai.ChatMessageRoleSystem,
						Content: "You are a Cypher script expert for Neo4j databases.",
					},
					{
						Role:    openai.ChatMessageRoleUser,
						Content: prompt,
					},
				},
				Model: openai.GPT4oMini,
				Tools: []openai.Tool{cypherTool},
			},
		)

		if err != nil {
			log.Fatal("❌ Error in creating openai call: ", err)
		}

		// Check if the model wants to call the function
		toolCalls := resp.Choices[0].Message.ToolCalls

		if toolCalls != nil {
			fmt.Println("Function call requested: ", len(toolCalls))
			for _, tool := range toolCalls {
				fmt.Printf("Name: %s\n", tool.Function.Name)
				fmt.Printf("Arguments: %s\n", tool.Function.Arguments)
				var args struct {
					Queries []string `json:"queries"`
				}
				err := json.Unmarshal([]byte(tool.Function.Arguments), &args)
				if err != nil {
					fmt.Printf("Error parsing arguments: %v\n", err)
					return
				}

				fmt.Printf("Generated Cypher query: %s\n", args.Queries)

				for _, query := range args.Queries {
					_, err := session.Run(context.Background(), query, nil)
					if err != nil {
						log.Print("❌ Error in writing cypher query: ", err)
					}
				}
			}
		}

		fmt.Printf("> ✅ Processed chunk %d\n", chunk.ID)
	}
}
