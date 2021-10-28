package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/SkynetLabs/scanner-queue/database"
	accdb "github.com/SkynetLabs/skynet-accounts/database"
	"github.com/julienschmidt/httprouter"
	skyapi "gitlab.com/SkynetLabs/skyd/node/api"
)

func (api *API) ScanPOST(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	skylink := ps.ByName("skylink")
	if skylink == "" {
		skyapi.WriteError(w, skyapi.Error{"missing required parameter 'skylink'"}, http.StatusBadRequest)
		return
	}
	if !accdb.ValidSkylinkHash(skylink) {
		skyapi.WriteError(w, skyapi.Error{"invalid skylink"}, http.StatusBadRequest)
		return
	}
	sl := database.Skylink{
		Skylink:   skylink,
		Timestamp: time.Now().UTC(),
		Scanned:   false,
	}
	err := api.db.SkylinkCreate(r.Context(), sl)
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusInternalServerError)
		return
	}
	api.logger.Debugf("Added skylink %s", sl.Skylink)
	skyapi.WriteSuccess(w)
}

func (api *API) ScanPUT(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	skylink := ps.ByName("skylink")
	if skylink == "" {
		skyapi.WriteError(w, skyapi.Error{"missing required parameter 'skylink'"}, http.StatusBadRequest)
		return
	}
	if !accdb.ValidSkylinkHash(skylink) {
		skyapi.WriteError(w, skyapi.Error{"invalid skylink"}, http.StatusBadRequest)
		return
	}
	scanned, err := strconv.ParseBool(r.PostFormValue("scanned"))
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusBadRequest)
		return
	}
	sl := database.Skylink{
		Skylink:   skylink,
		Timestamp: time.Now().UTC(),
		Scanned:   scanned,
	}
	err = api.db.SkylinkUpdate(r.Context(), sl)
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusInternalServerError)
		return
	}
	api.logger.Debugf("Updated skylink %s", sl.Skylink)
	skyapi.WriteSuccess(w)
}
