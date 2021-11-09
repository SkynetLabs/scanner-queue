package api

import (
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

// Router exposed the internal httprouter struct.
func (api *API) Router() *httprouter.Router {
	return api.staticRouter
}
