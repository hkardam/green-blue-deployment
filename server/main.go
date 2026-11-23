package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

type Todo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

var (
	db      *sql.DB
	color   = env("COLOR", "unknown")  // "green-1" or "green-2", set by docker-compose
	version = env("VERSION", "dev")    // release label, set by deploy.sh
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe /health on localhost and exit 0/1 (used by the Docker HEALTHCHECK)")
	flag.Parse()
	if *healthcheck {
		probe()
	}

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
	// Both servers run this on start-up, so it must stay idempotent and
	// backward-compatible (see README: "Database changes").
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS todos (
		id BIGSERIAL PRIMARY KEY, title TEXT NOT NULL, done BOOLEAN NOT NULL DEFAULT false)`); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"color": color, "version": version})
	})
	mux.HandleFunc("GET /todos", list)
	mux.HandleFunc("POST /todos", create)
	mux.HandleFunc("GET /todos/{id}", get)
	mux.HandleFunc("PUT /todos/{id}", update)
	mux.HandleFunc("DELETE /todos/{id}", del)

	srv := &http.Server{Addr: ":8080", Handler: headers(mux)}
	go func() {
		log.Printf("%s %s listening on :8080", color, version)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// Graceful shutdown: when deploy.sh stops the old server, finish in-flight
	// requests instead of cutting them off.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	<-stop
	log.Println("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// probe is what `server -healthcheck` runs. The image is FROM scratch (no shell,
// no curl), so the binary has to check itself.
func probe() {
	c := http.Client{Timeout: 2 * time.Second}
	res, err := c.Get("http://127.0.0.1:8080/health")
	if err != nil || res.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	os.Exit(0)
}

// health is the readiness check: 200 only if this instance can reach Postgres.
func health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fail(w, http.StatusServiceUnavailable, err)
		return
	}
	w.Write([]byte("ok"))
}

// headers adds CORS plus X-Served-By / X-Version so the client can see which
// server answered each request.
func headers(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Expose-Headers", "X-Served-By, X-Version")
		w.Header().Set("X-Served-By", color)
		w.Header().Set("X-Version", version)
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
