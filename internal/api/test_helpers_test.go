package api

// registerUnprotectedTestRoutes registers all core HTTP routes without RBAC protection.
// This is a test-only helper for internal package tests that exercise handlers directly
// without going through authentication middleware.
func (s *Server) registerUnprotectedTestRoutes() {
	s.mux.HandleFunc("/openapi.yaml", s.handleOpenAPISpec)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/api/v1/auth/setup", s.handleSetup)
	s.mux.HandleFunc("/readyz", s.handleReady)
	if s.metrics != nil {
		s.mux.Handle("/metrics", s.metrics.Handler())
	}
	s.mux.HandleFunc("/api/v1/test-sentry", s.handleTestSentry)
	s.mux.HandleFunc("/api/v1/pools", s.handlePools)
	s.mux.HandleFunc("/api/v1/pools/", s.handlePoolsSubroutes)
	s.mux.HandleFunc("/api/v1/accounts", s.handleAccounts)
	s.mux.HandleFunc("/api/v1/accounts/", s.handleAccountsSubroutes)
	s.mux.HandleFunc("/api/v1/blocks", s.handleBlocksList)
	s.mux.HandleFunc("/api/v1/export", s.handleExport)
	s.mux.HandleFunc("/api/v1/import/accounts", s.handleImportAccounts)
	s.mux.HandleFunc("/api/v1/import/pools", s.handleImportPools)
	s.mux.HandleFunc("/api/v1/audit", s.handleAuditList)
	s.mux.HandleFunc("/api/v1/schema/check", s.handleSchemaCheck)
	s.mux.HandleFunc("/api/v1/schema/apply", s.handleSchemaApply)
	s.mux.HandleFunc("/api/v1/search", s.handleSearch)
	s.mux.Handle("/", s.handleSPA())
}

// registerUnprotectedAuthTestRoutes registers auth API endpoints without RBAC protection.
// This is a test-only helper for internal package tests.
func (as *AuthServer) registerUnprotectedAuthTestRoutes() {
	as.mux.HandleFunc("/api/v1/auth/keys", as.handleAPIKeys)
	as.mux.HandleFunc("/api/v1/auth/keys/", as.handleAPIKeyByID)
}
