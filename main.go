package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Post struct {
	ID        int
	Content   string
	Username  string
	Likes     int
	CreatedAt string
}

type PageData struct {
	Username string
	Posts    []Post
}

var db *sql.DB
var templates = template.Must(template.ParseGlob("templates/*.html"))
//var tmpl = template.Must(template.ParseFiles("templates/*.html"))

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./posts.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

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
	rows, err := db.Query("SELECT id, content, username, likes, created_at FROM posts ORDER BY created_at DESC")
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
    posts = append(posts, post)
}

	data := PageData{
    Username: getUsername(r), // reads from cookie
    Posts:    posts,          // your slice of Post structs
}
templates.ExecuteTemplate(w, "index.html", data)
}

//Post Handler
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

	if username == "" || content == "" {
		http.Error(w, "Missing fields", 400)
		return
	}

	_, err := db.Exec("INSERT INTO posts (username, content) VALUES (?, ?)", username, content)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
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

//Like handler
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

	var exists int
	err = db.QueryRow("SELECT 1 FROM likes WHERE user_id = ? AND post_id = ?", userID, postID).Scan(&exists)
	if err == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	_, err = db.Exec("UPDATE posts SET likes = likes + 1 WHERE id = ?", postID)
	if err != nil {
		http.Error(w, "Failed to like post", 500)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

//reply handler
func replyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		db, _ := sql.Open("sqlite3", "./posts.db")

		postID := r.FormValue("post_id")
		username := r.FormValue("username")
		content := r.FormValue("content")
		timestamp := time.Now().Format("2006-01-02 15:04:05")

		_, err := db.Exec("INSERT INTO replies (post_id, username, content, created_at) VALUES (?, ?, ?, ?)",
			postID, username, content, timestamp)

		if err != nil {
			http.Error(w, "Failed to save reply", 500)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

//get username handler
//func getUsername(r *http.Request) string {
//    // session lookup logic
//    return ""
//}
