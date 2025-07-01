package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"strings"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB
var templates = template.Must(template.ParseGlob("templates/*.html"))

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./posts.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()


	initDatabase()

	//routes
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/thread", postThreadHandler)
	http.HandleFunc("/post", postHandler)
	http.HandleFunc("/journal", journalHandler)
	http.HandleFunc("/journal/post", journalPostHandler)
	http.HandleFunc("/like", likePostHandler)
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/logout", logoutHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	log.Println("Starting server on :8081...")
	log.Fatal(http.ListenAndServe(":8081", nil))

	migrateDatabase()
}

func initDatabase() {
	// user table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// posts table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			content TEXT NOT NULL,
			parent_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (parent_id) REFERENCES posts(id)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Add post_type column if it doesn't exist (ignore error if it already exists)
	_, err = db.Exec(`ALTER TABLE posts ADD COLUMN post_type TEXT DEFAULT 'regular'`)
	if err != nil {
		// Log the error but don't fail - column might already exist
		log.Println("Note: post_type column might already exist:", err)
	}

	//create likes table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS likes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			post_id INTEGER NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (post_id) REFERENCES posts(id),
			UNIQUE (user_id, post_id)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// tags table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	//post_tags junction
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS post_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			FOREIGN KEY (post_id) REFERENCES posts(id),
			FOREIGN KEY (tag_id) REFERENCES tags(id),
			UNIQUE (post_id, tag_id)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
}

// this is mostly bc im lazy
func migrateDatabase(){
	var count int
	err := db.QueryRow("PRAGMA table_info(posts)").Scan(&count)

	_, err = db.Exec("ALTER TABLE posts ADD COLUMN image_url TEXT")
	if err != nil {
		if !strings.Contains(err.Error(), "duplicate column name: image_url") {
			log.Printf("Warning: Could not add image_url column: %v", err)
		}
	}
}
