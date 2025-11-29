package api

func (s *APIServer) setupRoutes() {
	authWithHeaders := chain(s.bearerTokenAuthMiddleware, s.headersMiddleware)
	authWithStreamHeaders := chain(s.bearerTokenAuthMiddleware, s.streamHeadersMiddleware)

	s.router.Handle("GET /health", s.handleHealth())
	s.router.Handle("POST /v1/deploy", authWithHeaders(s.handleDeploy()))
	s.router.Handle("GET /v1/deploy/{deploymentID}/logs", authWithStreamHeaders(s.handleDeploymentLogs()))
	s.router.Handle("POST /v1/images/upload", authWithHeaders(s.handleImageUpload()))
	s.router.Handle("GET /v1/logs", authWithStreamHeaders(s.handleLogs()))
	s.router.Handle("GET /v1/rollback/{appName}", authWithHeaders(s.handleRollbackTargets()))
	s.router.Handle("POST /v1/rollback", authWithHeaders(s.handleRollback()))
	s.router.Handle("GET /v1/status/{appName}", authWithHeaders(s.handleAppStatus()))
	s.router.Handle("POST /v1/stop/{appName}", authWithHeaders(s.handleStopApp()))
	s.router.Handle("GET /v1/version", s.handleVersion())
}
