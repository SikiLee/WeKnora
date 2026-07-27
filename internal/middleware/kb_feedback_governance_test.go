package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestRequireKBFeedbackGovernanceMatrix(t *testing.T) {
	enabled := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
	tests := []struct {
		name       string
		role       types.TenantRole
		userID     string
		access     *KBAccess
		apiKey     bool
		verified   bool
		wantStatus int
	}{
		{
			name: "own KB creator", role: types.TenantRoleContributor, userID: "creator",
			access: ownFeedbackKBAccess("creator"), wantStatus: http.StatusNoContent,
		},
		{
			name: "own KB admin", role: types.TenantRoleAdmin, userID: "admin",
			access: ownFeedbackKBAccess("creator"), verified: true, wantStatus: http.StatusNoContent,
		},
		{
			name: "own KB non-creator contributor", role: types.TenantRoleContributor, userID: "other",
			verified: true, access: ownFeedbackKBAccess("creator"), wantStatus: http.StatusNoContent,
		},
		{
			name: "own KB creator demoted to viewer", role: types.TenantRoleViewer, userID: "creator",
			access: ownFeedbackKBAccess("creator"), wantStatus: http.StatusForbidden,
		},
		{
			name: "shared KB editor", role: types.TenantRoleContributor, userID: "editor",
			verified: true, access: sharedFeedbackKBAccess(types.OrgRoleEditor), wantStatus: http.StatusNoContent,
		},
		{
			name: "shared KB viewer", role: types.TenantRoleAdmin, userID: "viewer",
			access: sharedFeedbackKBAccess(types.OrgRoleViewer), wantStatus: http.StatusNotFound,
		},
		{
			name: "authorized API key", role: types.TenantRoleViewer, userID: "system-1",
			access: ownFeedbackKBAccess("creator"), apiKey: true, wantStatus: http.StatusForbidden,
		},
		{
			name: "missing KB access resolution", role: types.TenantRoleAdmin, userID: "admin",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(ErrorHandler())
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
				ctx = context.WithValue(ctx, types.UserIDContextKey, tc.userID)
				ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
				ctx = context.WithValue(ctx, types.TenantRoleVerifiedContextKey, tc.verified)
				if tc.apiKey {
					ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 7, FullAccess: true})
				}
				c.Request = c.Request.WithContext(ctx)
				if tc.access != nil {
					c.Set(KBAccessContextKey, tc.access)
				}
				c.Next()
			})
			r.Use(RequireKBFeedbackGovernance(cfg))
			r.GET("/governance", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/governance", nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestRequireKBFeedbackResetMatrix(t *testing.T) {
	enabled := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
	tests := []struct {
		name       string
		role       types.TenantRole
		userID     string
		access     *KBAccess
		apiKey     bool
		verified   bool
		wantStatus int
	}{
		{
			name: "creator", role: types.TenantRoleContributor, userID: "creator",
			access: ownFeedbackKBAccess("creator"), verified: true, wantStatus: http.StatusNoContent,
		},
		{
			name: "verified admin", role: types.TenantRoleAdmin, userID: "admin",
			access: ownFeedbackKBAccess("creator"), verified: true, wantStatus: http.StatusNoContent,
		},
		{
			name: "non-creator contributor", role: types.TenantRoleContributor, userID: "other",
			access: ownFeedbackKBAccess("creator"), verified: true, wantStatus: http.StatusForbidden,
		},
		{
			name: "shared editor", role: types.TenantRoleContributor, userID: "editor",
			access: sharedFeedbackKBAccess(types.OrgRoleEditor), verified: true, wantStatus: http.StatusForbidden,
		},
		{
			name: "synthetic admin", role: types.TenantRoleAdmin, userID: "admin",
			access: ownFeedbackKBAccess("creator"), wantStatus: http.StatusForbidden,
		},
		{
			name: "api key", role: types.TenantRoleAdmin, userID: "system",
			access: ownFeedbackKBAccess("creator"), apiKey: true, verified: true, wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
				ctx = context.WithValue(ctx, types.UserIDContextKey, tc.userID)
				ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
				ctx = context.WithValue(ctx, types.TenantRoleVerifiedContextKey, tc.verified)
				if tc.apiKey {
					ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 7, FullAccess: true})
				}
				c.Request = c.Request.WithContext(ctx)
				c.Set(KBAccessContextKey, tc.access)
				c.Next()
			})
			r.POST("/reset", RequireKBFeedbackReset(cfg), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/reset", nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestRequireKBFeedbackGovernanceAllowsSharedEditorThroughAccessChain(t *testing.T) {
	enabled := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
	lookup := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{
		"kb-shared": {ID: "kb-shared", TenantID: 2, CreatorID: "source-owner"},
	}}
	share := &stubKBShareForGuard{
		permission: map[string]types.OrgMemberRole{"kb-shared": types.OrgRoleEditor},
		shared:     map[string]bool{"kb-shared": true},
		source:     map[string]uint64{"kb-shared": 2},
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.UserIDContextKey, "editor")
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleContributor)
		ctx = context.WithValue(ctx, types.TenantRoleVerifiedContextKey, true)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET(
		"/knowledge-bases/:id/chunk-feedback",
		RequireKBFeedbackAccess(KBIDFromParam("id"), types.OrgRoleEditor, lookup, share, nil, cfg),
		RequireKBFeedbackGovernance(cfg),
		func(c *gin.Context) {
			tenantID, _ := types.TenantIDFromContext(c.Request.Context())
			if tenantID != 2 {
				t.Fatalf("effective tenant=%d want=2", tenantID)
			}
			c.Status(http.StatusNoContent)
		},
	)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(
		http.MethodGet, "/knowledge-bases/kb-shared/chunk-feedback", nil,
	))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d want=%d body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
}

func TestRequireKBFeedbackGovernanceAlwaysEnforcesSensitiveBoundary(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		access   *KBAccess
		role     types.TenantRole
		user     *types.User
		verified bool
		want     int
	}{
		{
			name:   "RBAC disabled does not admit non-creator",
			cfg:    &config.Config{Tenant: &config.TenantConfig{EnableRBAC: boolPointer(false)}},
			access: ownFeedbackKBAccess("creator"), role: types.TenantRoleContributor,
			user: &types.User{ID: "other"}, want: http.StatusForbidden,
		},
		{
			name: "cross-tenant superuser stays hidden",
			cfg: &config.Config{Tenant: &config.TenantConfig{
				EnableRBAC: boolPointer(true), EnableCrossTenantAccess: true,
			}},
			access: sharedFeedbackKBAccess(types.OrgRoleEditor), role: types.TenantRoleViewer,
			user: &types.User{ID: "super", CanAccessAllTenants: true}, want: http.StatusNotFound,
		},
		{
			name: "same-tenant superuser is not an ownership bypass",
			cfg: &config.Config{Tenant: &config.TenantConfig{
				EnableRBAC: boolPointer(true), EnableCrossTenantAccess: true,
			}},
			access: ownFeedbackKBAccess("creator"), role: types.TenantRoleViewer,
			user: &types.User{ID: "super", CanAccessAllTenants: true}, want: http.StatusForbidden,
		},
		{
			name:   "synthetic fail-open Admin is not a persisted tenant Admin",
			cfg:    &config.Config{Tenant: &config.TenantConfig{EnableRBAC: boolPointer(false)}},
			access: ownFeedbackKBAccess("creator"), role: types.TenantRoleAdmin,
			user: &types.User{ID: "other"}, verified: false, want: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(ErrorHandler())
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
				ctx = context.WithValue(ctx, types.UserIDContextKey, tc.user.ID)
				ctx = context.WithValue(ctx, types.UserContextKey, tc.user)
				ctx = context.WithValue(ctx, types.TenantRoleContextKey, tc.role)
				ctx = context.WithValue(ctx, types.TenantRoleVerifiedContextKey, tc.verified)
				c.Request = c.Request.WithContext(ctx)
				c.Set(KBAccessContextKey, tc.access)
				c.Next()
			})
			r.GET("/governance", RequireKBFeedbackGovernance(tc.cfg), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/governance", nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func ownFeedbackKBAccess(creatorID string) *KBAccess {
	return &KBAccess{
		KnowledgeBase:     &types.KnowledgeBase{ID: "kb-own", TenantID: 1, CreatorID: creatorID},
		CallerTenantID:    1,
		EffectiveTenantID: 1,
		Permission:        types.OrgRoleAdmin,
	}
}

func sharedFeedbackKBAccess(permission types.OrgMemberRole) *KBAccess {
	return &KBAccess{
		KnowledgeBase:     &types.KnowledgeBase{ID: "kb-shared", TenantID: 2, CreatorID: "source-owner"},
		CallerTenantID:    1,
		EffectiveTenantID: 2,
		Permission:        permission,
	}
}
