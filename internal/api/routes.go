package api

func (s *APIServer) setupRoutes() {
	headers := chain(s.headersMiddleware, s.rateLimiter.Middleware)
	headersWithAuth := chain(s.headersMiddleware, s.rateLimiter.Middleware, s.bearerTokenAuthMiddleware)
	streamHeadersWithAuth := chain(s.streamHeadersMiddleware, s.rateLimiter.Middleware, s.bearerTokenAuthMiddleware)

	s.router.Handle("GET /health", headers(s.handleHealth()))
	s.router.Handle("POST /v1/deploy", headersWithAuth(s.handleDeploy()))
	s.router.Handle("GET /v1/deploy/{deploymentID}/logs", streamHeadersWithAuth(s.handleDeploymentLogs()))
	s.router.Handle("POST /v1/images/upload", headersWithAuth(s.handleImageUpload()))
	s.router.Handle("GET /v1/logs", streamHeadersWithAuth(s.handleLogs()))
	s.router.Handle("GET /v1/rollback/{appName}", headersWithAuth(s.handleRollbackTargets()))
	s.router.Handle("POST /v1/rollback", headersWithAuth(s.handleRollback()))
	s.router.Handle("GET /v1/status/{appName}", headersWithAuth(s.handleAppStatus()))
	s.router.Handle("POST /v1/stop/{appName}", headersWithAuth(s.handleStopApp()))
	s.router.Handle("POST /v1/exec/{appName}", headersWithAuth(s.handleExec()))
	s.router.Handle("GET /v1/version", headersWithAuth(s.handleVersion()))
}
