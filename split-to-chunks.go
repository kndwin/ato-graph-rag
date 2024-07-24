package main

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"log"
)

func main() {
	log.Printf("🟨 Starting\n")

	db, err := loadDb()
	if err != nil {
		log.Fatal("❌ Error in initialising db: ", err)
	}

	err = replaceTable(db)
	if err != nil {
		log.Fatal("❌ Error in replacing table: ", err)
	}

	for fileInfo := range folderIterator("./docs/cleaned") {
		log.Printf("📂 File: '%s'", fileInfo.Name)
		for chunkInfo := range chunkIterator(fileInfo.Content, `QC\d{5}`) {
			log.Print("> 🔄 [Chunk: ", chunkInfo.Index, "]")
			query := `INSERT INTO chunks (title, chunk, chunk_index) VALUES (?, ?, ?)`
			_, err := db.Exec(query, fileInfo.Name, chunkInfo.Chunk, chunkInfo.Index)

			if err != nil {
				log.Fatal("❌ Error inserting chunk: ", err)
			}
		}
	}
	log.Printf("✅ Finished\n")
}

func replaceTable(db *sql.DB) error {
	// Drop the existing table
	_, err := db.Exec(`DROP TABLE IF EXISTS chunks`)
	if err != nil {
		return fmt.Errorf("error dropping table 'chunks': %v", err)
	}

	// Create the new table
	_, err = db.Exec(`CREATE TABLE chunks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        title TEXT,
        chunk TEXT,
        chunk_index INTEGER
    )`)
	if err != nil {
		return fmt.Errorf("error creating table 'chunks': %v", err)
	}

	log.Println("✅ Table 'chunks' replaced successfully")
	return nil
}
