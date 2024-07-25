package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// #cgo LDFLAGS: -L -Wl,-undefined,dynamic_lookup
import "C"

// FileInfo struct to hold file information
type FileInfo struct {
	Name    string
	Content string
}

// folderIterator returns a channel that yields FileInfo for each file in the folder
func folderIterator(folderPath string) <-chan FileInfo {
	ch := make(chan FileInfo)

	go func() {
		defer close(ch)

		files, err := os.ReadDir(folderPath)
		if err != nil {
			log.Printf("Error reading directory: %v\n", err)
			return
		}

		for _, file := range files {
			if !file.IsDir() {
				filePath := filepath.Join(folderPath, file.Name())
				content, err := os.ReadFile(filePath)
				if err != nil {
					log.Printf("Error reading file %s: %v\n", file.Name(), err)
					continue
				}
				ch <- FileInfo{Name: file.Name(), Content: string(content)}
			}
		}
	}()

	return ch
}

type ChunkInfo struct {
	Index int
	Chunk string
}

func chunkIterator(content, pattern string) <-chan ChunkInfo {
	ch := make(chan ChunkInfo)

	go func() {
		defer close(ch)

		re, err := regexp.Compile(pattern)
		if err != nil {
			log.Printf("Error compiling regex: %v\n", err)
			return
		}

		indexes := re.FindAllStringIndex(content, -1)
		start := 0

		for i, idx := range indexes {
			ch <- ChunkInfo{
				Index: i,
				Chunk: content[start:idx[0]],
			}
			start = idx[1]
		}
		ch <- ChunkInfo{
			Index: len(indexes),
			Chunk: content[start:],
		}
	}()

	return ch
}

func loadDb() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:chunks.db?cache=shared&&mode=rwc")

	if err != nil {
		log.Printf("❌ Error opening database: %v", err)
		return nil, err
	}

	db.SetMaxOpenConns(1)

	err = db.Ping()
	if err != nil {
		log.Printf("❌ Error connecting to database: %v", err)
		return nil, err
	}

	log.Println("✅ Database connection established")

	return db, nil
}

func loadGraphDB() (neo4j.DriverWithContext, error) {
	err := godotenv.Load()
	dbUri := os.Getenv("NEO4J_URI")
	dbUser := os.Getenv("NEO4J_USER")
	dbPassword := os.Getenv("NEO4J_PW")
	driver, err := neo4j.NewDriverWithContext(
		dbUri,
		neo4j.BasicAuth(dbUser, dbPassword, ""))

	err = driver.VerifyConnectivity(context.Background())
	if err != nil {
		return nil, err
	}
	log.Println("✅ Connection established.")

	return driver, nil
}

type ChunkContent struct {
	ID    int
	Chunk string
}

func getChunksFromDB() ([]ChunkContent, error) {
	db, err := loadDb()
	if err != nil {
		return nil, err
	}

	// Retrieve chunks from the database
	rows, err := db.Query("SELECT id, chunk FROM chunks")
	if err != nil {
		return nil, err
	}

	// Store chunks in memory
	var chunks []ChunkContent
	defer rows.Close()
	for rows.Next() {
		var chunk ChunkContent
		err := rows.Scan(&chunk.ID, &chunk.Chunk)
		if err != nil {
			log.Fatal(err)
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

func getSchemaVisualization(session neo4j.SessionWithContext) (string, error) {
	result, err := session.Run(context.Background(),
		"CALL db.schema.visualization()",
		nil)
	if err != nil {
		return "", fmt.Errorf("error running query: %w", err)
	}

	records, err := result.Collect(context.Background())
	if err != nil {
		return "", fmt.Errorf("error collecting results: %w", err)
	}

	var schemaStr strings.Builder

	for _, record := range records {
		nodes, _ := record.Get("nodes")
		relationships, _ := record.Get("relationships")

		schemaStr.WriteString("Nodes:\n")
		for _, node := range nodes.([]interface{}) {
			schemaStr.WriteString(fmt.Sprintf("%+v\n", node))
		}

		schemaStr.WriteString("\nRelationships:\n")
		for _, rel := range relationships.([]interface{}) {
			schemaStr.WriteString(fmt.Sprintf("%+v\n", rel))
		}
	}

	return schemaStr.String(), nil
}

func prettyPrintJSON(v interface{}) {
	jsonData, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("Error marshalling JSON: %v\n", err)
		return
	}
	fmt.Println(string(jsonData))
}
func getCypherTool() openai.Tool {

	// Define the function for creating Cypher queries
	cypherFunction := openai.FunctionDefinition{
		Name:        "create_cypher_query",
		Description: "Generate a Cypher query for Neo4j",
		Parameters: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"queries": {
					Type:        jsonschema.Array,
					Description: "List of cypher queries to execute",
					Items: &jsonschema.Definition{
						Type: jsonschema.String,
					},
				},
			},
			Required: []string{"queries"},
		},
	}

	// Create a Tool from the FunctionDefinition
	cypherTool := openai.Tool{
		Type:     openai.ToolTypeFunction,
		Function: &cypherFunction,
	}
	return cypherTool
}
