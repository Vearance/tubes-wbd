package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/http/middleware"
)

type RouterConfig struct {
	Auth            *auth.AuthService
	AdminAuthorizer middleware.AdminAuthorizer
	CookieOptions   middleware.CookieOptions
}

func NewRouter(server ServerInterface, config RouterConfig) chi.Router {
	router := chi.NewRouter()

	plainWrapper := &ServerInterfaceWrapper{
		Handler:          server,
		ErrorHandlerFunc: openAPIErrorHandler,
	}
	protectedWrapper := &ServerInterfaceWrapper{
		Handler: server,
		HandlerMiddlewares: []MiddlewareFunc{
			middleware.CSRF(config.Auth),
			middleware.Session(config.Auth, config.CookieOptions),
		},
		ErrorHandlerFunc: openAPIErrorHandler,
	}
	adminWrapper := &ServerInterfaceWrapper{
		Handler: server,
		HandlerMiddlewares: []MiddlewareFunc{
			middleware.RequireAdmin(config.AdminAuthorizer),
			middleware.Session(config.Auth, config.CookieOptions),
		},
		ErrorHandlerFunc: openAPIErrorHandler,
	}

	router.Get("/health", plainWrapper.GetHealth)
	router.Post("/auth/login", plainWrapper.PostAuthLogin)
	router.Post("/auth/logout", protectedWrapper.PostAuthLogout)
	router.Get("/admin/ping", adminWrapper.GetAdminPing)

	return router
}

func openAPIErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if encodeErr := json.NewEncoder(w).Encode(ErrorResponse{Message: "invalid request"}); encodeErr != nil {
		log.Printf("writing OpenAPI error response: %v", encodeErr)
	}
	log.Printf("openapi request validation failed: %v", err)
}
