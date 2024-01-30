package main

import (
	"context"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mu         sync.Mutex
	shortURLs  = make(map[string]string)
	client     *mongo.Client
	collection *mongo.Collection
)

var tpl = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>URL Shortener</title>
</head>
<body>
    <h1>URL Shortener</h1>
    <form method="post" action="/shorten">
        <label for="url">URL to Shorten:</label>
        <input type="url" name="url" required>
        <button type="submit">Shorten</button>
    </form>
    <br>
    <h2>Shortened URLs:</h2>
    <ul>
        {{range $code, $url := .ShortURLs}}
            <li><a href="/{{$code}}" target="_blank">{{$url}}</a></li>
        {{end}}
    </ul>
</body>
</html>
`))

type PageVariables struct {
	ShortURLs map[string]string
}

type URLMapping struct {
	Code string `bson:"code"`
	URL  string `bson:"url"`
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Connect to MongoDB
	clientOptions := options.Client().ApplyURI("")
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(context.Background())

	// Select the database and collection
	database := client.Database("urlshortener")
	collection = database.Collection("urls")

	// Initialize HTTP server
	r := http.NewServeMux()
	r.HandleFunc("/", homeHandler)
	r.HandleFunc("/shorten", shortenHandler)
	r.HandleFunc("/{code}", redirectHandler)

	log.Fatal(http.ListenAndServe(":4001", r))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	pageVariables := PageVariables{
		ShortURLs: shortURLs,
	}

	err := tpl.Execute(w, pageVariables)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func shortenHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	url := r.FormValue("url")
	if url == "" {
		http.Error(w, "URL cannot be empty", http.StatusBadRequest)
		return
	}

	shortCode := generateShortCode()
	shortURLs[shortCode] = url

	// Save to MongoDB
	if err := saveToMongoDB(shortCode, url); err != nil {
		log.Printf("Failed to save to database: %v", err)
		http.Error(w, "Failed to save to database", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	shortCode := r.URL.Path[1:]
	if originalURL, ok := shortURLs[shortCode]; ok {
		http.Redirect(w, r, originalURL, http.StatusSeeOther)
		return
	}

	// If not found in the map, try to find in MongoDB
	url, err := findInMongoDB(shortCode)
	if err == nil && url != "" {
		http.Redirect(w, r, url, http.StatusSeeOther)
		return
	}

	http.NotFound(w, r)
}

func generateShortCode() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength := 6

	b := make([]byte, codeLength)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}

	return string(b)
}

func saveToMongoDB(code, url string) error {
	_, err := collection.InsertOne(context.Background(), URLMapping{Code: code, URL: url})
	if err != nil {
		log.Printf("Error saving to MongoDB: %v", err)
	}
	return err
}

func findInMongoDB(code string) (string, error) {
	var result URLMapping
	err := collection.FindOne(context.Background(), bson.M{"code": code}).Decode(&result)
	if err != nil {
		log.Printf("Error finding URL in MongoDB: %v", err)
		return "", err
	}
	return result.URL, nil
}
