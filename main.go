package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Post struct {
	ID        int
	Content   string
	Username  string
	Likes     int
	CreatedAt string
	Tags      []string
}

type PageData struct {
	Username string
	Posts    []Post
}

var db *sql.DB
var templates = template.Must(template.ParseGlob("templates/*.html"))

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./posts.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create posts table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Create users table (needed for likes)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Create likes table
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

	// Create tags table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Create post_tags junction table
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

	// Create replies table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS replies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (post_id) REFERENCES posts(id)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/logout", logoutHandler)
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/post", postHandler)
	http.HandleFunc("/like", likePostHandler)
	http.HandleFunc("/reply", replyHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	log.Println("Starting server on :8081...")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

//index handler
func indexHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT
			posts.id,
			posts.content,
			posts.username,
			COUNT(likes.id) AS likes,
			posts.created_at
		FROM posts
		LEFT JOIN likes ON posts.id = likes.post_id
		GROUP BY posts.id
		ORDER BY posts.created_at DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var post Post
		if err := rows.Scan(&post.ID, &post.Content, &post.Username, &post.Likes, &post.CreatedAt); err != nil {
			log.Println("Scan error:", err)
			continue
		}

		// Fetch tags for the post
		tagRows, err := db.Query(`
			SELECT tags.name
			FROM post_tags
			INNER JOIN tags ON post_tags.tag_id = tags.id
			WHERE post_tags.post_id = ?`, post.ID)
		if err != nil {
			log.Println("Error fetching tags:", err)
			continue
		}

		var tags []string
		for tagRows.Next() {
			var tag string
			if err := tagRows.Scan(&tag); err != nil {
				log.Println("Error scanning tag:", err)
				continue
			}
			tags = append(tags, tag)
		}
		tagRows.Close()

		post.Tags = tags // Store tags in the struct instead of appending to content
		posts = append(posts, post)
	}

	data := PageData{
		Username: getUsername(r),
		Posts:    posts,
	}
	templates.ExecuteTemplate(w, "index.html", data)
}

//post Handler
func postHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	username := getUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	
	content := r.FormValue("content")
	if content == "" {
		http.Error(w, "Missing content", 400)
		return
	}

	// Insert the post
	result, err := db.Exec("INSERT INTO posts (username, content) VALUES (?, ?)", username, content)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	postID, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Failed to retrieve postID", 500)
		return
	}

	// Handle tags
	tagList := r.FormValue("tags")
	if tagList != "" {
		tags := parseTags(tagList)
		for _, tagName := range tags {
			tagName = strings.TrimSpace(tagName)
			if tagName == "" {
				continue
			}

			var tagID int
			// Try to find existing tag
			err = db.QueryRow("SELECT id FROM tags WHERE name = ?", tagName).Scan(&tagID)
			if err == sql.ErrNoRows {
				// Tag doesn't exist, create it
				result, err := db.Exec("INSERT INTO tags (name) VALUES (?)", tagName)
				if err != nil {
					log.Printf("Failed to insert tag '%s': %v", tagName, err)
					continue
				}
				tagID64, _ := result.LastInsertId()
				tagID = int(tagID64)
			} else if err != nil {
				log.Printf("Failed to query tag '%s': %v", tagName, err)
				continue
			}

			// Associate the tag with the post
			_, err = db.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", postID, tagID)
			if err != nil {
				log.Printf("Failed to associate tag '%s' with post: %v", tagName, err)
			}
		}
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

//tags helper func
func parseTags(tags string) []string {
	if tags == "" {
		return nil
	}
	var result []string
	for _, tag := range strings.Split(tags, ",") {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// "[...] [time] ago" system
func timeAgo(t time.Time) string {
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(diff.Hours()))
	default:
		return t.Format("Jan 2")
	}
}

//like handler
func likePostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	username := getUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	postID := r.FormValue("post_id")
	if postID == "" {
		http.Error(w, "Missing post ID", http.StatusBadRequest)
		return
	}

	var userID int
	err := db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err != nil {
		http.Error(w, "User not found", 500)
		return
	}

	// Check if the user has already liked the post
	var exists int
	err = db.QueryRow("SELECT 1 FROM likes WHERE user_id = ? AND post_id = ?", userID, postID).Scan(&exists)
	if err == nil {
		// Unlike the post
		_, err = db.Exec("DELETE FROM likes WHERE user_id = ? AND post_id = ?", userID, postID)
		if err != nil {
			http.Error(w, "Failed to unlike post", 500)
			return
		}
	} else if err == sql.ErrNoRows {
		// Like the post
		_, err = db.Exec("INSERT INTO likes (user_id, post_id) VALUES (?, ?)", userID, postID)
		if err != nil {
			http.Error(w, "Failed to like post", 500)
			return
		}
	} else {
		http.Error(w, "Database error", 500)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

//reply handler
func replyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	username := getUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	postID := r.FormValue("post_id")
	content := r.FormValue("content")

	if postID == "" || content == "" {
		http.Error(w, "Missing required fields", 400)
		return
	}

	_, err := db.Exec("INSERT INTO replies (post_id, username, content) VALUES (?, ?, ?)",
		postID, username, content)
	if err != nil {
		http.Error(w, "Failed to save reply", 500)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
