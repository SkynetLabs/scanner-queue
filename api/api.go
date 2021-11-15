package api

import (
	"fmt"
	"net/http"

	"github.com/SkynetLabs/malware-scanner/clamav"
	"github.com/SkynetLabs/malware-scanner/database"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
)

// API is our central entry point to all subsystems relevant to serving requests.
type API struct {
	staticDB     *database.DB
	staticClamav *clamav.ClamAV
	staticRouter *httprouter.Router
	staticLogger *logrus.Logger
}

// New creates a new API instance.
func New(db *database.DB, clam *clamav.ClamAV, logger *logrus.Logger) (*API, error) {
	if db == nil {
		return nil, errors.New("no DB provided")
	}
	if logger == nil {
		logger = logrus.New()
	}
	router := httprouter.New()
	router.RedirectTrailingSlash = true

	api := &API{
		staticDB:     db,
		staticClamav: clam,
		staticRouter: router,
		staticLogger: logger,
	}

	api.buildHTTPRoutes()
	return api, nil
}

// ListenAndServe starts the API server on the given port.
func (api *API) ListenAndServe(port int) error {
	api.staticLogger.Info(fmt.Sprintf("Listening on port %d", port))
	return http.ListenAndServe(fmt.Sprintf(":%d", port), api.staticRouter)
}
