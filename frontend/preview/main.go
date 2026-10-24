// test for preview with mock datas
package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultPreviewPort = "4173"

type loginPageData struct {
	Title          string
	Username       string
	CSRFToken      string
	Error          string
	SessionExpired bool
	FieldErrors    map[string]string
}

func main() {
	loginTemplate, err := template.ParseFiles(
		"frontend/templates/layouts/public.html",
		"frontend/templates/partials/form_feedback.html",
		"frontend/templates/pages/login.html",
	)
	if err != nil {
		log.Fatalf("parse login template: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle(
		"GET /static/",
		http.StripPrefix("/static/", http.FileServer(http.Dir("frontend/static"))),
	)

	renderLogin := func(w http.ResponseWriter, data loginPageData) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := loginTemplate.ExecuteTemplate(w, "public-layout", data); err != nil {
			log.Printf("render login template: %v", err)
		}
	}

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		data := loginPageData{
			Title:       "Sign in · BScheme",
			CSRFToken:   "preview-csrf-token",
			FieldErrors: make(map[string]string),
		}

		switch r.URL.Query().Get("state") {
		case "invalid":
			data.Username = "alice"
			data.Error = "Invalid username or password."
		case "expired":
			data.SessionExpired = true
		case "server-error":
			data.Error = "Unable to sign in. Please try again later."
		case "fields":
			data.FieldErrors["username"] = "Enter your username."
			data.FieldErrors["password"] = "Enter your password."
		}

		renderLogin(w, data)
	})

	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		data := loginPageData{
			Title:       "Sign in · BScheme",
			Username:    username,
			CSRFToken:   "preview-csrf-token",
			FieldErrors: make(map[string]string),
		}

		if username == "" {
			data.FieldErrors["username"] = "Enter your username."
		}
		if password == "" {
			data.FieldErrors["password"] = "Enter your password."
		}

		if len(data.FieldErrors) == 0 {
			data.Error = "Preview mode does not authenticate users."
		}

		renderLogin(w, data)
	})

	port := os.Getenv("PREVIEW_PORT")
	if port == "" {
		port = defaultPreviewPort
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("frontend preview available at http://localhost:%s/login", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(fmt.Errorf("serve frontend preview: %w", err))
	}
}
