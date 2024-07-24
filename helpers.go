package main

import (
	"database/sql"
	_ "github.com/tursodatabase/go-libsql"
	"log"
	"os"
	"path/filepath"
	"regexp"
)

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
	db, err := sql.Open("libsql", "file:chunks.db?cache=shared&&mode=rwc")

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
