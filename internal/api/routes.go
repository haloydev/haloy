package api

func (s *APIServer) setupRoutes() {
	withAuth := chain(s.rateLimiter.Middleware, s.bearerTokenAuthMiddleware)
	httpWithRateLimit := chain(s.headersMiddleware, s.rateLimiter.Middleware)
	httpWithAuth := chain(s.headersMiddleware, s.rateLimiter.Middleware, s.bearerTokenAuthMiddleware)
	httpWithAuthLayers := chain(s.headersMiddleware, s.layerRateLimiter.Middleware, s.bearerTokenAuthMiddleware)
	streamWithAuth := chain(s.streamHeadersMiddleware, s.rateLimiter.Middleware, s.bearerTokenAuthMiddleware)

	s.router.Handle("GET /health", httpWithRateLimit(s.handleHealth()))
	s.router.Handle("POST /v1/deploy", httpWithAuth(s.handleDeploy()))
	s.router.Handle("GET /v1/deploy/{deploymentID}/logs", streamWithAuth(s.handleDeploymentLogs()))
	s.router.Handle("POST /v1/images/upload", httpWithAuth(s.handleImageUpload()))
	s.router.Handle("POST /v1/images/layers/check", httpWithAuthLayers(s.handleLayerCheck()))
	s.router.Handle("POST /v1/images/layers", httpWithAuthLayers(s.handleLayerUpload()))
	s.router.Handle("POST /v1/images/layers/assemble", httpWithAuthLayers(s.handleImageAssemble()))
	s.router.Handle("GET /v1/logs/{appName}", streamWithAuth(s.handleAppLogs()))
	s.router.Handle("GET /v1/server-logs", streamWithAuth(s.handleServerLogs()))
	s.router.Handle("GET /v1/rollback/{appName}", httpWithAuth(s.handleRollbackTargets()))
	s.router.Handle("POST /v1/rollback", httpWithAuth(s.handleRollback()))
	s.router.Handle("GET /v1/status/{appName}", httpWithAuth(s.handleAppStatus()))
	s.router.Handle("POST /v1/stop/{appName}", httpWithAuth(s.handleStopApp()))
	s.router.Handle("POST /v1/exec/{appName}", httpWithAuth(s.handleExec()))
	s.router.Handle("POST /v1/tunnel/{appName}", withAuth(s.handleTunnel()))
	s.router.Handle("GET /v1/version", httpWithAuth(s.handleVersion()))
	s.router.Handle("POST /v1/upgrade", httpWithAuth(s.handleUpgrade()))
	s.router.Handle("POST /v1/upgrade/restart", httpWithAuth(s.handleUpgradeRestart()))
}
