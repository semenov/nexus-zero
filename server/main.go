package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
	srv := NewServer(store, hub)

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
	})

	// WebSocket endpoint (auth via query param).
	r.Get("/v1/ws", srv.ServeWS)

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// corsMiddleware adds permissive CORS headers to every response. This allows
// a future web client served from any origin to interact with the API.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
