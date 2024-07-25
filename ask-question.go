package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/sashabaranov/go-openai"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
)

func main() {
	err := godotenv.Load()
	log.Printf("Hello world")
	var question = "What are the eligibility requirements to claim a deduction?"
	log.Print("Question: ", question)

	log.Printf("Attempt 1: With baseline RAG")
	log.Printf("> Embedding question from OpenAI")
	ai := openai.NewClient(os.Getenv("OPENAI_API_KEY"))

	embedResp, err := ai.CreateEmbeddings(
		context.Background(),
		openai.EmbeddingRequest{
			Input: []string{question},
			Model: openai.AdaEmbeddingV2,
		},
	)
	if err != nil {
		log.Fatal("❌ Error in calling OpenAI: ", err)
	}

	embeddingJSON, err := json.Marshal(embedResp.Data[0].Embedding)
	questionEmbeddings, err := parseEmbedding(string(embeddingJSON))

	db, err := loadDb()
	if err != nil {
		log.Fatal("❌ Error in loading sqlite db: ", err)
	}

	log.Printf("> Grabbing embeddings from DB and calculating their scores")
	rows, err := db.Query("SELECT embedding, chunk_id FROM embeddings")
	if err != nil {
		log.Fatal("❌ Error in querying embeddings: ", err)
	}

	var closestChunk struct {
		score float64
		id    int
	}
	closestChunk.score = 0
	closestChunk.id = 0

	for rows.Next() {
		var embedding string
		var chunkId int
		err := rows.Scan(&embedding, &chunkId)
		if err != nil {
			log.Fatal("❌ Error in getting embedding from rows.Scan: ", err)
		}
		chunkEmbeddings, err := parseEmbedding(embedding)
		if err != nil {
			log.Fatal("❌ Error in parsing chunk embeddings: ", err)
		}

		similarityScore := cosineSimilarity(questionEmbeddings, chunkEmbeddings)
		if err != nil {
			log.Fatal("❌ Error in calculating cosine score: ", err)
		}

		log.Printf(">> Chunk %d: [%f]", chunkId, similarityScore)

		if similarityScore > closestChunk.score {
			closestChunk.score = similarityScore
			closestChunk.id = chunkId
		}
	}

	log.Printf("> Closest chunk id: %d (%f) ", closestChunk.id, closestChunk.score)
	row := db.QueryRow("SELECT chunk FROM chunks WHERE id == (?)", closestChunk.id)
	var chunk string
	err = row.Scan(&chunk)
	if err != nil {
		log.Fatal("❌ Error in getting closest chunk: ", err)
	}
	log.Printf("> Asking OpenAI with best document pulled")

	var content = "Use the following content to answer the question:\n" + "Content:\n" + chunk + "Question: " + question

	baselineRagResp, err := ai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a helpful assistant",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: content,
				},
			},
			Model: openai.GPT4oMini,
		},
	)
	if err != nil {
		log.Fatal("❌ Error in calling OpenAI: ", err)
	}

	log.Printf(baselineRagResp.Choices[0].Message.Content)

	log.Printf("Attempt 2: With Graph RAG")
	log.Printf("> Get cypher query from OpenAI")

	gdb, err := loadGraphDB()
	if err != nil {
		log.Fatal("❌ Error with loading graph db: ", err)
	}
	session := gdb.NewSession(context.Background(), neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeRead,
	})
	defer session.Close(context.Background())
	schema, err := getSchemaVisualization(session)
	if err != nil {
		log.Fatal("❌ Error in getting schema: ", err)
	}
	prompt := fmt.Sprintf("Given a neo4j schema, create a cypher query that would give you as much information as you need to answer the following question\n Neo4j schema:\n%s\nQuestion: %s", schema, question)
	cypherTool := getCypherTool()
	cypherQueryResp, err := ai.CreateChatCompletion(
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

	log.Printf("> Using tool from OpenAI to call cypher query")
	var queryResult = ""
	// Check if the model wants to call the function
	toolCalls := cypherQueryResp.Choices[0].Message.ToolCalls
	if toolCalls != nil {
		log.Println("Function call requested: ", len(toolCalls))
		for _, tool := range toolCalls {
			log.Printf("Name: %s\n", tool.Function.Name)
			log.Printf("Arguments: %s\n", tool.Function.Arguments)
			var args struct {
				Queries []string `json:"queries"`
			}
			err := json.Unmarshal([]byte(tool.Function.Arguments), &args)
			if err != nil {
				log.Printf("Error parsing arguments: %v\n", err)
				return
			}

			log.Printf("Generated Cypher query: %s\n", args.Queries)

			for _, query := range args.Queries {
				result, err := getQueryResultAsString(session, query)
				if err != nil {
					log.Print("❌ Error in writing cypher query: ", err)
				}
				queryResult += result + "\n"
			}
		}
	}

	log.Printf("> Got cypher context and calling OpenAI for answers")
	content = "Use the following neo4j context to answer the question:\n" + "Context:\n" + queryResult + "Question: " + question

	graphRagResult, err := ai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a helpful assistant",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: content,
				},
			},
			Model: openai.GPT4oMini,
		},
	)
	if err != nil {
		log.Fatal("❌ Error in creating openai call: ", err)
	}
	log.Printf(graphRagResult.Choices[0].Message.Content)
}

func parseEmbedding(embeddingStr string) ([]float64, error) {
	trimmed := strings.Trim(embeddingStr, "[]")
	strValues := strings.Split(trimmed, ",")
	embedding := make([]float64, len(strValues))
	for i, str := range strValues {
		val, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return nil, err
		}
		embedding[i] = val
	}
	return embedding, nil
}

func cosineSimilarity(a, b []float64) float64 {
	dotProduct := 0.0
	magnitudeA := 0.0
	magnitudeB := 0.0

	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		magnitudeA += a[i] * a[i]
		magnitudeB += b[i] * b[i]
	}

	magnitudeA = math.Sqrt(magnitudeA)
	magnitudeB = math.Sqrt(magnitudeB)

	if magnitudeA == 0 || magnitudeB == 0 {
		return 0 // Handle division by zero
	}

	return dotProduct / (magnitudeA * magnitudeB)
}

func getQueryResultAsString(session neo4j.SessionWithContext, query string) (string, error) {
	result, err := session.Run(context.Background(), query, nil)
	if err != nil {
		return "", fmt.Errorf("error executing query: %w", err)
	}

	var records []map[string]interface{}

	for result.Next(context.Background()) {
		record := result.Record()
		recordMap := make(map[string]interface{})
		for _, key := range record.Keys {
			value, _ := record.Get(key)
			recordMap[key] = value
		}
		records = append(records, recordMap)
	}

	if err = result.Err(); err != nil {
		return "", fmt.Errorf("error iterating results: %w", err)
	}

	jsonData, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshaling to JSON: %w", err)
	}

	return string(jsonData), nil
}
