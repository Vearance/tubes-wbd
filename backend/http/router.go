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
	adminReadWrapper := &ServerInterfaceWrapper{
		Handler: server,
		HandlerMiddlewares: []MiddlewareFunc{
			middleware.RequireAdmin(config.AdminAuthorizer),
			middleware.Session(config.Auth, config.CookieOptions),
		},
		ErrorHandlerFunc: openAPIErrorHandler,
	}
	adminMutationWrapper := &ServerInterfaceWrapper{
		Handler: server,
		HandlerMiddlewares: []MiddlewareFunc{
			middleware.CSRF(config.Auth),
			middleware.RequireAdmin(config.AdminAuthorizer),
			middleware.Session(config.Auth, config.CookieOptions),
		},
		ErrorHandlerFunc: openAPIErrorHandler,
	}

	router.Get("/health", plainWrapper.GetHealth)
	router.Post("/auth/login", plainWrapper.PostAuthLogin)
	router.Post("/auth/logout", protectedWrapper.PostAuthLogout)
	router.Get("/admin/ping", adminReadWrapper.GetAdminPing)
	router.Get("/admin/users", adminReadWrapper.ListAdminUsers)
	router.Post("/admin/users", adminMutationWrapper.CreateAdminUser)
	router.Patch("/admin/users/{userId}", adminMutationWrapper.UpdateAdminUser)
	router.Get("/admin/groups", adminReadWrapper.ListAdminGroups)
	router.Post("/admin/groups", adminMutationWrapper.CreateAdminGroup)
	router.Get("/admin/groups/{groupId}/members", adminReadWrapper.ListAdminGroupMembers)
	router.Post("/admin/groups/{groupId}/members", adminMutationWrapper.AddAdminGroupMember)
	router.Delete("/admin/groups/{groupId}/members/{userId}", adminMutationWrapper.RemoveAdminGroupMember)
	router.Get("/admin/groups/{groupId}/databases", adminReadWrapper.ListAdminGroupDatabases)
	router.Put("/admin/groups/{groupId}/databases/{databaseId}", adminMutationWrapper.SetAdminGroupDatabase)
	router.Delete("/admin/groups/{groupId}/databases/{databaseId}", adminMutationWrapper.RemoveAdminGroupDatabase)

	router.Get("/admin/databases", adminReadWrapper.ListAdminDatabases)
	router.Post("/admin/databases", adminMutationWrapper.CreateAdminDatabase)
	router.Post("/admin/databases/test", adminMutationWrapper.TestAdminDatabaseConnection)
	router.Get("/admin/databases/{databaseId}", adminReadWrapper.GetAdminDatabase)
	router.Patch("/admin/databases/{databaseId}", adminMutationWrapper.UpdateAdminDatabase)
	router.Delete("/admin/databases/{databaseId}", adminMutationWrapper.DeleteAdminDatabase)
	router.Post("/admin/databases/{databaseId}/test", adminMutationWrapper.TestAdminStoredDatabase)

	router.Get("/databases", protectedWrapper.ListDatabases)
	router.Get("/databases/{databaseId}/schemas", protectedWrapper.ListDatabaseSchemas)
	router.Get("/databases/{databaseId}/tables", protectedWrapper.ListDatabaseTables)
	router.Get("/databases/{databaseId}/table-structure", protectedWrapper.GetDatabaseTableStructure)
	router.Get("/databases/{databaseId}/table-rows", protectedWrapper.GetDatabaseTableRows)
	router.Get("/databases/{databaseId}/migrations/history", protectedWrapper.ListMigrationHistory)
	router.Post("/databases/{databaseId}/migrations/explain", protectedWrapper.ExplainMigration)
	router.Get("/databases/{databaseId}/migrations/history/{version}", protectedWrapper.GetMigrationHistoryEntry)
	router.Get("/databases/{databaseId}/migrations/drafts", protectedWrapper.ListMigrationDrafts)
	router.Post("/databases/{databaseId}/migrations/drafts", protectedWrapper.CreateMigrationDraft)
	router.Get("/migrations/drafts/{draftId}", protectedWrapper.GetMigrationDraft)
	router.Patch("/migrations/drafts/{draftId}", protectedWrapper.UpdateMigrationDraft)
	router.Post("/migrations/drafts/{draftId}/execute", protectedWrapper.ExecuteMigrationDraft)
	router.Get("/migrations/drafts/{draftId}/runs", protectedWrapper.ListMigrationRuns)

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
