package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"tubes-wbd/internal/auth"
	"tubes-wbd/internal/http/middleware"
)

type RouterConfig struct {
	Auth            *auth.AuthService
	AdminAuthorizer middleware.AdminAuthorizer
	CookieOptions   middleware.CookieOptions
	Middlewares     []func(http.Handler) http.Handler
}

func NewRouter(server ServerInterface, config RouterConfig) chi.Router {
	router := chi.NewRouter()
	for _, mw := range config.Middlewares {
		if mw != nil {
			router.Use(mw)
		}
	}

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
	router.Put("/admin/groups/{groupId}/leader", adminMutationWrapper.SetAdminGroupLeader)
	router.Delete("/admin/groups/{groupId}/leader", adminMutationWrapper.ClearAdminGroupLeader)
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
	router.Post("/databases/{databaseId}/query", protectedWrapper.ExecuteDatabaseQuery)
	router.Get("/databases/{databaseId}/migrations/history", protectedWrapper.ListMigrationHistory)
	router.Post("/databases/{databaseId}/migrations/rollback", protectedWrapper.SubmitMigrationRollback)
	router.Post("/databases/{databaseId}/migrations/forward", protectedWrapper.SubmitMigrationForward)
	router.Get("/databases/{databaseId}/migrations/executions", protectedWrapper.ListMigrationExecutions)
	router.Post("/databases/{databaseId}/migrations/explain", protectedWrapper.ExplainMigration)
	router.Get("/databases/{databaseId}/migrations/history/{version}", protectedWrapper.GetMigrationHistoryEntry)
	router.Get("/databases/{databaseId}/migrations/drafts", protectedWrapper.ListMigrationDrafts)
	router.Post("/databases/{databaseId}/migrations/drafts", protectedWrapper.CreateMigrationDraft)
	router.Get("/migrations/drafts/{draftId}", protectedWrapper.GetMigrationDraft)
	router.Patch("/migrations/drafts/{draftId}", protectedWrapper.UpdateMigrationDraft)
	router.Post("/migrations/drafts/{draftId}/execute", protectedWrapper.ExecuteMigrationDraft)
	router.Get("/migrations/executions/{executionId}", protectedWrapper.GetMigrationExecution)
	router.Post("/migrations/executions/{executionId}/compensate", protectedWrapper.SubmitMigrationCompensation)
	router.Get("/migrations/executions/{executionId}/approvals", protectedWrapper.GetMigrationExecutionApprovals)
	router.Post("/migrations/executions/{executionId}/approve", protectedWrapper.ApproveMigrationExecution)
	router.Post("/migrations/executions/{executionId}/reject", protectedWrapper.RejectMigrationExecution)
	router.Post("/migrations/executions/{executionId}/enqueue", protectedWrapper.EnqueueMigrationExecution)
	router.Get("/migrations/approvals/pending", protectedWrapper.ListPendingMigrationApprovals)

	return router
}

func openAPIErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if encodeErr := json.NewEncoder(w).Encode(ErrorResponse{Message: "invalid request"}); encodeErr != nil {
		slog.ErrorContext(ctx, "writing OpenAPI error response",
			"event", "http.response.write_error",
			"status", "error",
			"error", encodeErr,
		)
	}
	slog.WarnContext(ctx, "openapi request validation failed",
		"event", "http.request.validation_failed",
		"status", "rejected",
		"error", err,
	)
}
