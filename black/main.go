package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

type Todo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; ; i++ { // wait for postgres
		if err = db.Ping(); err == nil {
			break
		}
		if i == 30 {
			log.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS todos (
		id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, done BOOLEAN NOT NULL DEFAULT false)`); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /todos", list)
	mux.HandleFunc("POST /todos", create)
	mux.HandleFunc("GET /todos/{id}", get)
	mux.HandleFunc("PUT /todos/{id}", update)
	mux.HandleFunc("DELETE /todos/{id}", del)

	log.Println("black listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", cors(mux)))
}

func cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func idOf(r *http.Request) (int64, error) { return strconv.ParseInt(r.PathValue("id"), 10, 64) }

func list(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, title, done FROM todos ORDER BY id DESC LIMIT 100`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	todos := []Todo{}
	for rows.Next() {
		var t Todo
		rows.Scan(&t.ID, &t.Title, &t.Done)
		todos = append(todos, t)
	}
	writeJSON(w, 200, todos)
}

func get(w http.ResponseWriter, r *http.Request) {
	id, err := idOf(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var t Todo
	err = db.QueryRow(`SELECT id, title, done FROM todos WHERE id=$1`, id).Scan(&t.ID, &t.Title, &t.Done)
	if err == sql.ErrNoRows {
		fail(w, 404, err)
		return
	} else if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, t)
}

func create(w http.ResponseWriter, r *http.Request) {
	var t Todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		fail(w, 400, err)
		return
	}
	if err := db.QueryRow(`INSERT INTO todos (title, done) VALUES ($1, $2) RETURNING id`, t.Title, t.Done).Scan(&t.ID); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 201, t)
}

func update(w http.ResponseWriter, r *http.Request) {
	id, err := idOf(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var t Todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		fail(w, 400, err)
		return
	}
	res, err := db.Exec(`UPDATE todos SET title=$1, done=$2 WHERE id=$3`, t.Title, t.Done, id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(w, 404, sql.ErrNoRows)
		return
	}
	t.ID = id
	writeJSON(w, 200, t)
}

func del(w http.ResponseWriter, r *http.Request) {
	id, err := idOf(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	res, err := db.Exec(`DELETE FROM todos WHERE id=$1`, id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(w, 404, sql.ErrNoRows)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
