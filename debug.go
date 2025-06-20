package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "./posts.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Check if post_type column exists
	fmt.Println("=== Checking database schema ===")
	rows, err := db.Query("PRAGMA table_info(posts)")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("Posts table columns:")
	for rows.Next() {
		var cid int
		var name, type_, notNull, defaultValue, pk string
		rows.Scan(&cid, &name, &type_, &notNull, &defaultValue, &pk)
		fmt.Printf("  %s (%s)\n", name, type_)
	}

	// Check all posts
	fmt.Println("\n=== All posts in database ===")
	rows, err = db.Query("SELECT id, username, content, COALESCE(post_type, 'NULL') as post_type, created_at FROM posts ORDER BY created_at DESC")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var username, content, postType, createdAt string
		rows.Scan(&id, &username, &content, &postType, &createdAt)
		fmt.Printf("ID: %d, User: %s, Type: %s, Content: %.50s..., Created: %s\n", 
			id, username, postType, content, createdAt)
	}

	// Check specifically for journal posts
	fmt.Println("\n=== Journal posts only ===")
	rows, err = db.Query("SELECT id, username, content, created_at FROM posts WHERE post_type = 'journal' ORDER BY created_at DESC")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	journalCount := 0
	for rows.Next() {
		var id int
		var username, content, createdAt string
		rows.Scan(&id, &username, &content, &createdAt)
		fmt.Printf("ID: %d, User: %s, Content: %.50s..., Created: %s\n", 
			id, username, content, createdAt)
		journalCount++
	}
	
	if journalCount == 0 {
		fmt.Println("No journal posts found!")
	} else {
		fmt.Printf("Found %d journal posts\n", journalCount)
	}
}
