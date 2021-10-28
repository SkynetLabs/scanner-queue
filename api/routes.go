package api

// buildHTTPRoutes registers all HTTP routes and their handlers.
func (api *API) buildHTTPRoutes() {
	api.router.POST("/scan/:skylink", api.ScanPOST)
	api.router.PUT("/scan/:skylink", api.ScanPUT)
}
