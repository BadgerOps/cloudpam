package api

// registerUnprotectedTestRoutes registers all core HTTP routes without RBAC protection.
// This is a test-only helper for internal package tests that exercise handlers directly
// without going through authentication middleware.
func (s *Server) registerUnprotectedTestRoutes() {
	s.handleOpenAPIRouteFunc("/openapi", s.handleOpenAPIPage)
	s.handleOpenAPIRouteFunc("/openapi.yaml", s.handleOpenAPISpec)
	s.handleOpenAPIRouteFunc("/healthz", s.handleHealth)
	s.handleOpenAPIRouteFunc("/api/v1/auth/setup", s.handleSetup)
	s.handleOpenAPIRouteFunc("/readyz", s.handleReady)
	if s.metrics != nil {
		s.handleOpenAPIRoute("/metrics", s.metrics.Handler())
	}
	s.handleOpenAPIRouteFunc("/api/v1/test-sentry", s.handleTestSentry)
	s.handleOpenAPIRouteFunc("/api/v1/pools", s.handlePools)
	s.handleOpenAPIRouteFunc("/api/v1/pools/", s.handlePoolsSubroutes)
	s.handleOpenAPIRouteFunc("/api/v1/accounts", s.handleAccounts)
	s.handleOpenAPIRouteFunc("/api/v1/accounts/", s.handleAccountsSubroutes)
	s.handleOpenAPIRouteFunc("/api/v1/blocks", s.handleBlocksList)
	s.handleOpenAPIRouteFunc("/api/v1/export", s.handleExport)
	s.handleOpenAPIRouteFunc("/api/v1/import/accounts", s.handleImportAccounts)
	s.handleOpenAPIRouteFunc("/api/v1/import/pools", s.handleImportPools)
	s.handleOpenAPIRouteFunc("/api/v1/audit", s.handleAuditList)
	s.handleOpenAPIRouteFunc("/api/v1/schema/check", s.handleSchemaCheck)
	s.handleOpenAPIRouteFunc("/api/v1/schema/apply", s.handleSchemaApply)
	s.handleOpenAPIRouteFunc("/api/v1/search", s.handleSearch)
	s.handleOpenAPIRoute("/", s.handleSPA())
}

// registerUnprotectedAuthTestRoutes registers auth API endpoints without RBAC protection.
// This is a test-only helper for internal package tests.
func (as *AuthServer) registerUnprotectedAuthTestRoutes() {
	as.handleOpenAPIRouteFunc("/api/v1/auth/keys", as.handleAPIKeys)
	as.handleOpenAPIRouteFunc("/api/v1/auth/keys/", as.handleAPIKeyByID)
}
