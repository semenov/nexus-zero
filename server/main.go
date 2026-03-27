package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := Load()

	// Connect to PostgreSQL.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to create pgx pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	log.Println("connected to database")

	store := NewStore(pool)
	hub := NewHub()
	pusher := NewPusher()
	srv := NewServer(store, hub, pusher)

	r := chi.NewRouter()

	// Standard middleware.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Public routes.
	r.Post("/v1/users", srv.HandleRegister)
	r.Get("/v1/users/{identity_key}", srv.HandleGetUser)

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Post("/v1/messages", srv.HandleSendMessage)
		r.Get("/v1/messages/pending", srv.HandleGetPending)
		r.Get("/v1/messages/history", srv.HandleGetHistory)
		r.Get("/v1/contacts", srv.HandleGetContacts)
		r.Put("/v1/contacts/{contact_key}", srv.HandleUpsertContact)
		r.Delete("/v1/contacts/{contact_key}", srv.HandleDeleteContact)
		r.Put("/v1/device-token", srv.HandleUpsertDeviceToken)
	})

	// WebSocket endpoint (auth via query param).
	r.Get("/v1/ws", srv.ServeWS)

	// Serve web app static files if the dist directory exists.
	if _, err := os.Stat("web/dist"); err == nil {
		fs := http.FileServer(http.Dir("web/dist"))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			// Serve index.html for any path that doesn't match a real file
			// (SPA client-side routing).
			if _, err := os.Stat("web/dist" + r.URL.Path); os.IsNotExist(err) {
				http.ServeFile(w, r, "web/dist/index.html")
				return
			}
			fs.ServeHTTP(w, r)
		})
		log.Println("serving web app from web/dist")
	}

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// corsMiddleware adds CORS headers for the web client origin.
func corsMiddleware(next http.Handler) http.Handler {
	allowed := map[string]bool{
		"https://nexus.semenov.ai": true,
		"http://localhost:5173":    true, // dev
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
