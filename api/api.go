package api

import (
	"github.com/SkynetLabs/scanner-queue/database"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
)

type API struct {
	db     *database.DB
	router *httprouter.Router
	logger *logrus.Logger
}

func New(db *database.DB, logger *logrus.Logger) (*API, error) {
	if db == nil {
		return nil, errors.New("no DB provided")
	}
	if logger == nil {
		logger = logrus.New()
	}
	router := httprouter.New()
	router.RedirectTrailingSlash = true

	api := &API{
		db:     db,
		router: router,
		logger: logger,
	}

	api.buildHTTPRoutes()
	return api, nil
}

// Router exposed the internal httprouter struct.
func (api *API) Router() *httprouter.Router {
	return api.router
}
