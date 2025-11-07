package api

func (s *APIServer) setupRoutes() {
	authMiddleware := s.bearerTokenAuthMiddleware

	s.router.Handle("GET /health", s.handleHealth())
	s.router.Handle("POST /v1/deploy", authMiddleware(s.handleDeploy()))
	s.router.Handle("GET /v1/deploy/{deploymentID}/logs", authMiddleware(s.handleDeploymentLogs()))
	s.router.Handle("POST /v1/images/upload", authMiddleware(s.handleImageUpload()))
	s.router.Handle("GET /v1/logs", authMiddleware(s.handleLogs()))
	s.router.Handle("GET /v1/rollback/{appName}", authMiddleware(s.handleRollbackTargets()))
	s.router.Handle("POST /v1/rollback", authMiddleware(s.handleRollback()))
	s.router.Handle("GET /v1/status/{appName}", authMiddleware(s.handleAppStatus()))
	s.router.Handle("POST /v1/stop/{appName}", authMiddleware(s.handleStopApp()))
	s.router.Handle("GET /v1/version", s.handleVersion())
}
