package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Post struct {
	ID         int
	Content    string
	Username   string
	ImageURL   string
	Likes      int
	ReplyCount int
	CreatedAt  string
	Tags       []string
	ParentID   *int
	Replies    []Post
	PostType   string
}

type PageData struct {
	Username string
	Posts    []Post
	Post     *Post // individual post view
}

type DayGroup struct {
	DayNumber int
	Date      string
	Posts     []Post
}

type JournalPageData struct {
	Username  string
	DayGroups []DayGroup
}

const NEIGHBORHOOD_START_DATE = "2025-06-01"

// Helper functions for image handling
func isValidImage(contentType string) bool {
	validTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}
	return validTypes[contentType]
}

func generateUniqueFilename(originalFilename string) string {
	ext := filepath.Ext(originalFilename)
	randBytes := make([]byte, 16)
	rand.Read(randBytes)
	return hex.EncodeToString(randBytes) + ext
}

// index handler - timeline (exclude journal posts)
func indexHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT
			posts.id,
			posts.content,
			posts.username,
			posts.image_url,
			COUNT(DISTINCT likes.id) AS likes,
			COUNT(DISTINCT replies.id) AS reply_count,
			posts.created_at
		FROM posts
		LEFT JOIN likes ON posts.id = likes.post_id
		LEFT JOIN posts AS replies ON posts.id = replies.parent_id
		WHERE posts.parent_id IS NULL
		  AND (posts.post_type IS NULL OR posts.post_type != 'journal')
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
		if err := rows.Scan(&post.ID, &post.Content, &post.Username, &post.ImageURL, &post.Likes, &post.ReplyCount, &post.CreatedAt); err != nil {
			log.Println("Scan error:", err)
			continue
		}

		// fetch tags for the post
		post.Tags = getPostTags(post.ID)
		posts = append(posts, post)
	}

	data := PageData{
		Username: getUsername(r),
		Posts:    posts,
	}
	templates.ExecuteTemplate(w, "index.html", data)
}

// post thread handler
func postThreadHandler(w http.ResponseWriter, r *http.Request) {
	postIDStr := r.URL.Query().Get("id")
	if postIDStr == "" {
		http.Error(w, "Missing post ID", 400)
		return
	}

	postID, err := strconv.Atoi(postIDStr)
	if err != nil {
		http.Error(w, "Invalid post ID", 400)
		return
	}

	// gets main post
	var post Post
	err = db.QueryRow(`
		SELECT
			posts.id,
			posts.content,
			posts.username,
			posts.image_url,
			COUNT(DISTINCT likes.id) AS likes,
			COUNT(DISTINCT replies.id) AS reply_count,
			posts.created_at
		FROM posts
		LEFT JOIN likes ON posts.id = likes.post_id
		LEFT JOIN posts AS replies ON posts.id = replies.parent_id
		WHERE posts.id = ?
		GROUP BY posts.id
	`, postID).Scan(&post.ID, &post.Content, &post.Username, &post.ImageURL, &post.Likes, &post.ReplyCount, &post.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Post not found", 404)
		} else {
			http.Error(w, err.Error(), 500)
		}
		return
	}

	// get tags for the main post
	post.Tags = getPostTags(post.ID)

	// gets replies
	post.Replies = getPostReplies(postID)

	data := PageData{
		Username: getUsername(r),
		Post:     &post,
	}
	templates.ExecuteTemplate(w, "thread.html", data)
}

// post handler
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

	// image upload
	var imageURL string
	file, handler, fileErr := r.FormFile("image")
	if fileErr == nil {
		defer file.Close()

		if isValidImage(handler.Header.Get("Content-Type")) {
			filename := generateUniqueFilename(handler.Filename)

			// Create uploads directory if it doesn't exist
			err := os.MkdirAll("./uploads", 0755)
			if err != nil {
				log.Printf("Error creating uploads directory: %v", err)
			} else {
				dst, err := os.Create("./uploads/" + filename)
				if err == nil {
					defer dst.Close()
					io.Copy(dst, file)
					imageURL = "/uploads/" + filename // set image URL for post
				}
			}
		}
	}

	// JOURNAL: determine post type
	postType := r.FormValue("post_type")
	if postType == "" {
		postType = "regular" //default
	}

	// check if this is a reply
	var parentID *int
	if parentIDStr := r.FormValue("parent_id"); parentIDStr != "" {
		if pid, err := strconv.Atoi(parentIDStr); err == nil {
			parentID = &pid
		}
	}

	// insert the post (now w/ image_url)
	var result sql.Result
	var err error
	if parentID != nil {
		result, err = db.Exec("INSERT INTO posts (username, content, image_url, parent_id, post_type) VALUES (?, ?, ?, ?, ?)", username, content, imageURL, *parentID, postType)
	} else {
		result, err = db.Exec("INSERT INTO posts (username, content, image_url, post_type) VALUES (?, ?, ?, ?)", username, content, imageURL, postType)
	}

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	postID, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Failed to retrieve postID", 500)
		return
	}

	// handle tags (main posts only)
	if parentID == nil {
		tagList := r.FormValue("tags")
		if tagList != "" {
			insertPostTags(int(postID), tagList)
		}
	}

	// redirect (based on post type)
	if parentID != nil {
		// reply -> thread
		http.Redirect(w, r, "/thread?id="+strconv.Itoa(*parentID), http.StatusSeeOther)
	} else if postType == "journal" {
		// journal -> journal
		http.Redirect(w, r, "/journal", http.StatusSeeOther)
	} else {
		// regular -> home
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// like handler
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

	// check if the user has already liked the post
	var exists int
	err = db.QueryRow("SELECT 1 FROM likes WHERE user_id = ? AND post_id = ?", userID, postID).Scan(&exists)
	if err == nil {
		// unlike the post
		_, err = db.Exec("DELETE FROM likes WHERE user_id = ? AND post_id = ?", userID, postID)
		if err != nil {
			http.Error(w, "Failed to unlike post", 500)
			return
		}
	} else if err == sql.ErrNoRows {
		// like the post
		_, err = db.Exec("INSERT INTO likes (user_id, post_id) VALUES (?, ?)", userID, postID)
		if err != nil {
			http.Error(w, "Failed to like post", 500)
			return
		}
	} else {
		http.Error(w, "Database error", 500)
		return
	}

	// redirect back to appropriate page
	redirectURL := r.FormValue("redirect")
	if redirectURL == "" {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// helper functions

func getPostTags(postID int) []string {
	rows, err := db.Query(`
		SELECT tags.name
		FROM post_tags
		INNER JOIN tags ON post_tags.tag_id = tags.id
		WHERE post_tags.post_id = ?`, postID)
	if err != nil {
		log.Printf("Error fetching tags for post %d: %v", postID, err)
		return nil
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			log.Printf("Error scanning tag: %v", err)
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}

func getPostReplies(postID int) []Post {
	rows, err := db.Query(`
		SELECT
			posts.id,
			posts.content,
			posts.username,
			posts.image_url,
			COUNT(DISTINCT likes.id) AS likes,
			posts.created_at
		FROM posts
		LEFT JOIN likes ON posts.id = likes.post_id
		WHERE posts.parent_id = ?
		GROUP BY posts.id
		ORDER BY posts.created_at ASC
	`, postID)
	if err != nil {
		log.Printf("Error fetching replies for post %d: %v", postID, err)
		return nil
	}
	defer rows.Close()

	var replies []Post
	for rows.Next() {
		var reply Post
		if err := rows.Scan(&reply.ID, &reply.Content, &reply.Username, &reply.ImageURL, &reply.Likes, &reply.CreatedAt); err != nil {
			log.Printf("Error scanning reply: %v", err)
			continue
		}
		replies = append(replies, reply)
	}
	return replies
}

func insertPostTags(postID int, tagList string) {
	if tagList == "" {
		return
	}

	tags := parseTags(tagList)
	for _, tagName := range tags {
		tagName = strings.TrimSpace(tagName)
		if tagName == "" {
			continue
		}

		var tagID int
		// try to find existing tag
		err := db.QueryRow("SELECT id FROM tags WHERE name = ?", tagName).Scan(&tagID)
		if err == sql.ErrNoRows {
			// if tag doesn't exist, create it
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

		// associate the tag with the post
		_, err = db.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", postID, tagID)
		if err != nil {
			log.Printf("Failed to associate tag '%s' with post: %v", tagName, err)
		}
	}
}

// helper for parsing comma separated tags
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

// Journal handler
// Fixed journalHandler with complete data and proper day grouping
func journalHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT
			posts.id,
			posts.content,
			posts.username,
			posts.image_url,
			COUNT(DISTINCT likes.id) AS likes,
			COUNT(DISTINCT replies.id) AS reply_count,
			posts.created_at
		FROM posts
		LEFT JOIN likes ON posts.id = likes.post_id
		LEFT JOIN posts AS replies ON posts.id = replies.parent_id
		WHERE posts.parent_id IS NULL
		  AND posts.post_type = 'journal'
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
		if err := rows.Scan(
			&post.ID, &post.Content, &post.Username, &post.ImageURL,
			&post.Likes, &post.ReplyCount, &post.CreatedAt); err != nil {
			log.Println("Scan error:", err)
			continue
		}
		post.Tags = getPostTags(post.ID)
		posts = append(posts, post)
	}

	// Fixed: group posts by day with proper error handling
	dayGroups := groupPostsByDayFixed(posts)

	data := JournalPageData{
		Username:  getUsername(r),
		DayGroups: dayGroups,
	}
	templates.ExecuteTemplate(w, "journal.html", data)
}

// Fixed version of groupPostsByDay function
func groupPostsByDayFixed(posts []Post) []DayGroup {
	if len(posts) == 0 {
		return nil
	}

	dayMap := make(map[int][]Post)

	for _, post := range posts {
		dayNumber := getDayNumberFixed(post.CreatedAt)
		log.Printf("DEBUG: Post ID %d, Created: %s, Day: %d", post.ID, post.CreatedAt, dayNumber)
		if dayNumber > 0 {
			dayMap[dayNumber] = append(dayMap[dayNumber], post)
		}
	}

	// Create day groups in descending order (newest first)
	var dayGroups []DayGroup
	maxDay := getMaxDay(dayMap)
	for day := maxDay; day >= 1; day-- {
		if posts, exists := dayMap[day]; exists {
			dayGroups = append(dayGroups, DayGroup{
				DayNumber: day,
				Date:      formatDayDate(day),
				Posts:     posts,
			})
		}
	}

	return dayGroups
}

// Fixed getDayNumber function with better error handling
func getDayNumberFixed(createdAt string) int {
	// Parse neighborhood start date
	startDate, err := time.Parse("2006-01-02", NEIGHBORHOOD_START_DATE)
	if err != nil {
		log.Printf("Error parsing neighborhood start date: %v", err)
		return 0
	}

	// Try multiple date formats for parsing
	var postDate time.Time

	// Try SQLite default format first
	postDate, err = time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		// Try RFC3339 format
		postDate, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			// Try just the date part
			postDate, err = time.Parse("2006-01-02", createdAt[:10])
			if err != nil {
				log.Printf("Error parsing post created_at date '%s': %v", createdAt, err)
				return 0
			}
		}
	}

	// Calculate difference in days
	diff := postDate.Sub(startDate)
	dayNum := int(diff.Hours()/24) + 1

	// Ensure day number is positive
	if dayNum < 1 {
		dayNum = 1
	}

	return dayNum
}

// journal post handler
func journalPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Redirect(w, r, "/journal", http.StatusSeeOther)
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

	// Handle image upload for journal posts
	var imageURL string
	file, handler, fileErr := r.FormFile("image")
	if fileErr == nil {
		defer file.Close()

		if isValidImage(handler.Header.Get("Content-Type")) {
			filename := generateUniqueFilename(handler.Filename)

			// Create uploads directory if it doesn't exist
			err := os.MkdirAll("./uploads", 0755)
			if err != nil {
				log.Printf("Error creating uploads directory: %v", err)
			} else {
				dst, err := os.Create("./uploads/" + filename)
				if err == nil {
					defer dst.Close()
					io.Copy(dst, file)
					imageURL = "/uploads/" + filename
				}
			}
		}
	}

	// Insert journal post
	result, err := db.Exec("INSERT INTO posts (username, content, image_url, post_type) VALUES (?, ?, ?, 'journal')", username, content, imageURL)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	postID, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Failed to retrieve postID", 500)
		return
	}

	//Handler for journal post tags
	tagList := r.FormValue("tags")
	if tagList != "" {
		insertPostTags(int(postID), tagList)
	}

	http.Redirect(w, r, "/journal", http.StatusSeeOther)
}

func groupPostsByDay(posts []Post) []DayGroup {
	if len(posts) == 0 {
		return nil
	}

	dayMap := make(map[int][]Post)

	for _, post := range posts {
		dayNumber := getDayNumber(post.CreatedAt)
		if dayNumber > 0 {
			dayMap[dayNumber] = append(dayMap[dayNumber], post)
		}
	}

	var dayGroups []DayGroup
	for day := 1; day <= getMaxDay(dayMap); day++ {
		if posts, exists := dayMap[day]; exists {
			dayGroups = append(dayGroups, DayGroup{
				DayNumber: day,
				Date:      formatDayDate(day),
				Posts:     posts,
			})
		}
	}

	return dayGroups
}

func getDayNumber(createdAt string) int {
	// parse neighborhood start date
	startDate, err := time.Parse("2006-01-02", NEIGHBORHOOD_START_DATE)
	if err != nil {
		log.Println("Error parsing neighborhood start date: %v", err)
		return 0
	}

	// parse post created_at date
	postDate, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		log.Println("Error parsing post created_at date: %v", err)
		return 0
	}

	//calculate difference in days
	diff := postDate.Sub(startDate)
	dayNum := int(diff.Hours() / 24) + 1

	return dayNum
}

func getMaxDay(dayMap map[int][]Post) int {
	max := 0
	for day := range dayMap {
		if day > max {
			max = day
		}
	}
	return max
}

func formatDayDate(dayNum int) string {
	startDate, _ := time.Parse("2006-01-02", NEIGHBORHOOD_START_DATE)
	targetDate := startDate.AddDate(0, 0, dayNum-1)
	return targetDate.Format("January 2, 2006")
}

// image handler - you can remove this if you don't need a separate endpoint
func UploadImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the multipart form (32MB limit)
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// check file type
	if !isValidImage(handler.Header.Get("Content-Type")) {
		http.Error(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	// generate a unique filename
	filename := generateUniqueFilename(handler.Filename)

	// Create uploads directory if it doesn't exist
	err = os.MkdirAll("./uploads", 0755)
	if err != nil {
		http.Error(w, "Cannot create uploads directory", http.StatusInternalServerError)
		return
	}

	// save the file to the server
	dst, err := os.Create("./uploads/" + filename)
	if err != nil {
		http.Error(w, "Cannot create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}

	// return filename for frontend use
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"filename": filename})
}
