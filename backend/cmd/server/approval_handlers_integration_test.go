//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/gorm"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/auth"
	"tubes-wbd/internal/database/platformdb"
	"tubes-wbd/internal/database/targetdb"
	httpapi "tubes-wbd/internal/http"
	"tubes-wbd/internal/http/middleware"
	"tubes-wbd/internal/manageddb"
	"tubes-wbd/internal/migration"
	userdomain "tubes-wbd/internal/users"
)

type unreachableTargets struct{}

func (unreachableTargets) Pool(context.Context, targetdb.DatabaseID) (*pgxpool.Pool, error) {
	return nil, errors.New("target databases are not used by approval tests")
}

type approvalAPI struct {
	t        *testing.T
	db       *gorm.DB
	router   http.Handler
	auth     *auth.AuthService
	sessions map[string]auth.LoginResult
	users    map[string]uint
	groups   map[string]uint
	dbID     uint
}

func newApprovalAPI(t *testing.T) *approvalAPI {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("platform"),
		tcpostgres.WithUsername("platform"),
		tcpostgres.WithPassword("platform_password"),
		tcpostgres.BasicWaitStrategies(),
	)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	require.NoError(t, err)
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := platformdb.Open(ctx, platformdb.Config{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = platformdb.Close(db) })

	require.NoError(t, userdomain.AutoMigrate(db))
	require.NoError(t, auth.AutoMigrate(db))
	require.NoError(t, manageddb.AutoMigrate(db))
	require.NoError(t, access.AutoMigrate(db))
	require.NoError(t, migration.AutoMigrate(db))

	userRepository := userdomain.NewGormRepository(db)
	sessionRepository := auth.NewGormSessionRepository(db)
	authService, err := auth.NewAuthService(userRepository, sessionRepository, handlerHasher{}, auth.ServiceConfig{
		IdleTimeout:         time.Hour,
		AbsoluteLifetime:    8 * time.Hour,
		RotationInterval:    time.Hour,
		RotationGracePeriod: time.Minute,
	})
	require.NoError(t, err)
	accessRepository := access.NewGormRepository(db)
	authorization, err := access.NewAuthorizationService(userRepository, accessRepository)
	require.NoError(t, err)
	groupService, err := access.NewGroupService(userRepository, accessRepository)
	require.NoError(t, err)
	migrations, err := migration.NewMigrationService(authorization, unreachableTargets{}, migration.NewGormRepository(db), migration.Options{})
	require.NoError(t, err)

	api := &approvalAPI{
		t:        t,
		db:       db,
		auth:     authService,
		sessions: map[string]auth.LoginResult{},
		users:    map[string]uint{},
		groups:   map[string]uint{},
	}
	api.router = httpapi.NewRouter(apiServer{
		authService:   authService,
		groupService:  groupService,
		authorization: authorization,
		migrations:    migrations,
	}, httpapi.RouterConfig{Auth: authService, AdminAuthorizer: authorization})

	for _, name := range []string{"admin", "rudolf", "airgroove", "goldship", "tachyon"} {
		user := userdomain.User{Username: name, PasswordHash: "hash:password", IsAdmin: name == "admin"}
		require.NoError(t, db.Create(&user).Error)
		api.users[name] = user.ID
	}
	target := manageddb.ManagedDatabase{
		Name: "targetdb", Host: "localhost", Port: 5432, Username: "target",
		EncryptedPassword: []byte("unused"), DBName: "targetdb", SSLMode: "disable",
	}
	require.NoError(t, db.Create(&target).Error)
	api.dbID = target.ID
	api.addGroup("Student Council", access.PermissionViewEdit, "rudolf", "airgroove")
	api.addGroup("Team Spica", access.PermissionViewOnly, "goldship")
	return api
}

func (a *approvalAPI) addGroup(name string, permission access.PermissionLevel, members ...string) {
	a.t.Helper()
	group := access.Group{Name: name}
	require.NoError(a.t, a.db.Create(&group).Error)
	a.groups[name] = group.ID
	for _, member := range members {
		require.NoError(a.t, a.db.Create(&access.UserGroup{UserID: a.users[member], GroupID: group.ID}).Error)
	}
	require.NoError(a.t, a.db.Create(&access.GroupDatabasePermission{
		GroupID: group.ID, ManagedDatabaseID: a.dbID, Permission: permission,
	}).Error)
}

func (a *approvalAPI) do(username, method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if username != "" {
		session, ok := a.sessions[username]
		if !ok {
			var err error
			session, err = a.auth.Login(context.Background(), username, "password")
			require.NoError(a.t, err)
			a.sessions[username] = session
		}
		request.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: session.Token})
		if method != http.MethodGet {
			request.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
	}
	response := httptest.NewRecorder()
	a.router.ServeHTTP(response, request)
	return response
}

func (a *approvalAPI) setLeader(group, leader string) {
	a.t.Helper()
	response := a.do("admin", http.MethodPut, fmt.Sprintf("/admin/groups/%d/leader", a.groups[group]),
		fmt.Sprintf(`{"user_id":%d}`, a.users[leader]))
	require.Equal(a.t, http.StatusOK, response.Code, response.Body.String())
}

func (a *approvalAPI) submitDraft(username string) httpapi.MigrationExecution {
	a.t.Helper()
	created := a.do(username, http.MethodPost, fmt.Sprintf("/databases/%d/migrations/drafts", a.dbID),
		`{"name":"add trainee index","sql":"CREATE INDEX trainees_name_idx ON trainees (name)","down_sql":"DROP INDEX trainees_name_idx"}`)
	require.Equal(a.t, http.StatusCreated, created.Code, created.Body.String())
	var draft httpapi.MigrationDraft
	decodeBody(a.t, created, &draft)

	submitted := a.do(username, http.MethodPost, fmt.Sprintf("/migrations/drafts/%d/execute", draft.Id), "")
	require.Equal(a.t, http.StatusAccepted, submitted.Code, submitted.Body.String())
	var execution httpapi.MigrationExecution
	decodeBody(a.t, submitted, &execution)
	return execution
}

func decodeBody(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), target), response.Body.String())
}

func executionPath(id int64, action string) string {
	if action == "" {
		return fmt.Sprintf("/migrations/executions/%d", id)
	}
	return fmt.Sprintf("/migrations/executions/%d/%s", id, action)
}

func TestIntegrationApprovalEndpointsLeaderManagement(t *testing.T) {
	api := newApprovalAPI(t)
	council := fmt.Sprintf("/admin/groups/%d", api.groups["Student Council"])

	response := api.do("admin", http.MethodPut, council+"/leader", fmt.Sprintf(`{"user_id":%d}`, api.users["goldship"]))
	require.Equal(t, http.StatusConflict, response.Code)

	api.setLeader("Student Council", "rudolf")
	list := api.do("admin", http.MethodGet, "/admin/groups", "")
	require.Equal(t, http.StatusOK, list.Code)
	var groups []httpapi.Group
	decodeBody(t, list, &groups)
	require.Equal(t, "rudolf", *groups[0].LeaderUsername)
	require.Nil(t, groups[1].LeaderUserId)

	response = api.do("admin", http.MethodDelete, fmt.Sprintf("%s/members/%d", council, api.users["rudolf"]), "")
	require.Equal(t, http.StatusConflict, response.Code)

	created := api.do("rudolf", http.MethodPost, fmt.Sprintf("/databases/%d/migrations/drafts", api.dbID),
		`{"name":"noop","sql":"SELECT 1"}`)
	require.Equal(t, http.StatusCreated, created.Code)
	var draft httpapi.MigrationDraft
	decodeBody(t, created, &draft)
	response = api.do("rudolf", http.MethodPost, fmt.Sprintf("/migrations/drafts/%d/execute", draft.Id), "")
	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "Team Spica")

	response = api.do("admin", http.MethodDelete, council+"/leader", "")
	require.Equal(t, http.StatusNoContent, response.Code)
	response = api.do("admin", http.MethodDelete, fmt.Sprintf("%s/members/%d", council, api.users["rudolf"]), "")
	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestIntegrationApprovalEndpointsApproveAndEnqueue(t *testing.T) {
	api := newApprovalAPI(t)
	api.setLeader("Student Council", "rudolf")
	api.setLeader("Team Spica", "goldship")
	execution := api.submitDraft("airgroove")
	require.Equal(t, httpapi.MigrationExecutionStatusPendingReview, execution.Status)

	response := api.do("goldship", http.MethodGet, executionPath(execution.Id, "approvals"), "")
	require.Equal(t, http.StatusOK, response.Code)
	var summary httpapi.MigrationApprovalSummary
	decodeBody(t, response, &summary)
	require.True(t, summary.CanDecide)
	require.Len(t, summary.Approvals, 2)
	require.Equal(t, "Student Council", summary.Approvals[0].GroupName)
	require.Equal(t, "rudolf", *summary.Approvals[0].LeaderUsername)

	response = api.do("tachyon", http.MethodPost, executionPath(execution.Id, "approve"), "")
	require.Equal(t, http.StatusForbidden, response.Code)
	response = api.do("airgroove", http.MethodPost, executionPath(execution.Id, "approve"), "")
	require.Equal(t, http.StatusForbidden, response.Code)

	response = api.do("rudolf", http.MethodPost, executionPath(execution.Id, "approve"), "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	decodeBody(t, response, &summary)
	require.Equal(t, httpapi.MigrationExecutionStatusPendingReview, summary.ExecutionStatus)
	require.False(t, summary.CanDecide)
	require.Equal(t, httpapi.MigrationApprovalDecisionApproved, summary.Approvals[0].Decision)
	require.Equal(t, "rudolf", *summary.Approvals[0].DecidedByUsername)

	response = api.do("goldship", http.MethodGet, "/migrations/approvals/pending", "")
	require.Equal(t, http.StatusOK, response.Code)
	var pending []httpapi.MigrationExecution
	decodeBody(t, response, &pending)
	require.Len(t, pending, 1)
	require.Equal(t, execution.Id, pending[0].Id)

	response = api.do("rudolf", http.MethodPost, executionPath(execution.Id, "enqueue"), "")
	require.Equal(t, http.StatusConflict, response.Code)

	response = api.do("goldship", http.MethodPost, executionPath(execution.Id, "approve"), `{"note":"spica app unaffected"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	decodeBody(t, response, &summary)
	require.Equal(t, httpapi.MigrationExecutionStatusApproved, summary.ExecutionStatus)
	require.Equal(t, "spica app unaffected", *summary.Approvals[1].Note)

	response = api.do("goldship", http.MethodPost, executionPath(execution.Id, "enqueue"), "")
	require.Equal(t, http.StatusForbidden, response.Code)

	response = api.do("airgroove", http.MethodPost, executionPath(execution.Id, "enqueue"), "")
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	var queued httpapi.MigrationExecution
	decodeBody(t, response, &queued)
	require.Equal(t, httpapi.MigrationExecutionStatusQueued, queued.Status)

	response = api.do("goldship", http.MethodPost, executionPath(execution.Id, "approve"), "")
	require.Equal(t, http.StatusConflict, response.Code)
	response = api.do("airgroove", http.MethodPost, executionPath(execution.Id, "enqueue"), "")
	require.Equal(t, http.StatusConflict, response.Code)
}

func TestIntegrationApprovalEndpointsReject(t *testing.T) {
	api := newApprovalAPI(t)
	api.setLeader("Student Council", "rudolf")
	api.setLeader("Team Spica", "goldship")
	execution := api.submitDraft("airgroove")

	response := api.do("goldship", http.MethodPost, executionPath(execution.Id, "reject"), `{"note":"  "}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = api.do("goldship", http.MethodPost, executionPath(execution.Id, "reject"), "")
	require.Equal(t, http.StatusBadRequest, response.Code)

	response = api.do("goldship", http.MethodPost, executionPath(execution.Id, "reject"), `{"note":"locks the trainees table"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var summary httpapi.MigrationApprovalSummary
	decodeBody(t, response, &summary)
	require.Equal(t, httpapi.MigrationExecutionStatusRejected, summary.ExecutionStatus)
	require.Equal(t, httpapi.MigrationApprovalDecisionRejected, summary.Approvals[1].Decision)

	response = api.do("airgroove", http.MethodGet, executionPath(execution.Id, ""), "")
	require.Equal(t, http.StatusOK, response.Code)
	var stored httpapi.MigrationExecution
	decodeBody(t, response, &stored)
	require.Equal(t, httpapi.MigrationExecutionStatusRejected, stored.Status)

	response = api.do("rudolf", http.MethodPost, executionPath(execution.Id, "approve"), "")
	require.Equal(t, http.StatusConflict, response.Code)
	response = api.do("airgroove", http.MethodPost, executionPath(execution.Id, "enqueue"), "")
	require.Equal(t, http.StatusConflict, response.Code)

	response = api.do("airgroove", http.MethodPost, fmt.Sprintf("/migrations/drafts/%d/execute", execution.DraftId), "")
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
}

func TestIntegrationApprovalEndpointsRejectBadRequests(t *testing.T) {
	api := newApprovalAPI(t)
	api.setLeader("Student Council", "rudolf")
	api.setLeader("Team Spica", "goldship")
	execution := api.submitDraft("airgroove")

	response := api.do("", http.MethodGet, executionPath(execution.Id, "approvals"), "")
	require.Equal(t, http.StatusUnauthorized, response.Code)
	response = api.do("", http.MethodGet, "/migrations/approvals/pending", "")
	require.Equal(t, http.StatusUnauthorized, response.Code)

	response = api.do("tachyon", http.MethodGet, executionPath(execution.Id, "approvals"), "")
	require.Equal(t, http.StatusForbidden, response.Code)
	response = api.do("rudolf", http.MethodGet, executionPath(execution.Id+100, "approvals"), "")
	require.Equal(t, http.StatusNotFound, response.Code)
	response = api.do("rudolf", http.MethodPost, executionPath(0, "approve"), "")
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = api.do("rudolf", http.MethodPost, executionPath(execution.Id, "approve"), `{"note":"ok","extra":1}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = api.do("rudolf", http.MethodPost, executionPath(execution.Id, "approve"), `{"note":"`+strings.Repeat("x", 2001)+`"}`)
	require.Equal(t, http.StatusBadRequest, response.Code)

	session, err := api.auth.Login(context.Background(), "rudolf", "password")
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, executionPath(execution.Id, "approve"), nil)
	request.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: session.Token})
	request.Header.Set("X-CSRF-Token", "not-the-token")
	recorder := httptest.NewRecorder()
	api.router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code)

	response = api.do("rudolf", http.MethodGet, executionPath(execution.Id, "approvals"), "")
	var summary httpapi.MigrationApprovalSummary
	decodeBody(t, response, &summary)
	require.Equal(t, httpapi.MigrationApprovalDecisionPending, summary.Approvals[0].Decision)
}
