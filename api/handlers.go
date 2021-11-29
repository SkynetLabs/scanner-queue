package api

import (
	"net/http"

	"github.com/SkynetLabs/malware-scanner/database"
	"github.com/julienschmidt/httprouter"
	"gitlab.com/NebulousLabs/errors"
	skyapi "gitlab.com/SkynetLabs/skyd/node/api"
)

// healthGET returns the status of the service
func (api *API) healthGET(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	status := struct {
		DBAlive     bool `json:"dbAlive"`
		ClamAVAlive bool `json:"clamAVAlive"`
	}{}
	err := api.staticClam.Ping()
	status.ClamAVAlive = err == nil
	err = api.staticDB.Ping(r.Context())
	status.DBAlive = err == nil
	skyapi.WriteJSON(w, status)
}

// scanPOST adds a new skylink to the scanning queue. If the skylink is already
// in the queue we respond with 200 OK but we don't add it again.
func (api *API) scanPOST(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	skylink, err := parseSkylink(ps.ByName("skylink"), api.staticClam.PreferredPortal())
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusBadRequest)
		return
	}
	err = api.staticDB.SkylinkCreate(r.Context(), skylink)
	if errors.Contains(err, database.ErrSkylinkExists) {
		skyapi.WriteJSON(w, "Skylink already exists in the database")
		return
	}
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusInternalServerError)
		return
	}
	api.staticLogger.Debugf("Added skylink %s", skylink.Skylink)
	skyapi.WriteSuccess(w)
}

// parseSkylink parses the given string into a skylink and validates it.
func parseSkylink(s, portal string) (*database.Skylink, error) {
	if s == "" {
		return nil, errors.New("empty skylink")
	}
	var sl database.Skylink
	err := sl.LoadString(s, portal)
	if err != nil {
		return nil, err
	}
	return &sl, nil
}
