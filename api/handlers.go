package api

import (
	"net/http"
	"time"

	"github.com/SkynetLabs/scanner-queue/database"
	"github.com/julienschmidt/httprouter"
	"gitlab.com/NebulousLabs/errors"
	skyapi "gitlab.com/SkynetLabs/skyd/node/api"
	"go.mongodb.org/mongo-driver/mongo"
)

// ScanPOST adds a new skylink to the scanning queue. If the skylink is already
// in the queue we respond with 200 OK but we don't add it again.
func (api *API) ScanPOST(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	skylink, err := parseSkylink(ps.ByName("skylink"))
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusBadRequest)
		return
	}
	err = api.db.SkylinkCreate(r.Context(), skylink)
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusInternalServerError)
		return
	}
	api.logger.Debugf("Added skylink %s", skylink.Skylink)
	skyapi.WriteSuccess(w)
}

// ScanPUT updates a skylink in the queue. If the skylink is not yet in the
// queue it gets added to it.
func (api *API) ScanPUT(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	skylink, err := parseSkylink(ps.ByName("skylink"))
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusBadRequest)
		return
	}
	status := r.PostFormValue("status")
	if !StatusIsValid(status) {
		skyapi.WriteError(w, skyapi.Error{"invalid status"}, http.StatusBadRequest)
		return
	}
	category := r.PostFormValue("category")
	if !CategoryIsValid(category) {
		skyapi.WriteError(w, skyapi.Error{"invalid category"}, http.StatusBadRequest)
		return
	}
	// Fetch the skylink record from the database.
	sl, err := api.db.Skylink(r.Context(), skylink.Skylink)
	if errors.Contains(err, mongo.ErrNoDocuments) {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusNotFound)
		return
	}
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusInternalServerError)
		return
	}
	// Check for invalid status transitions.
	// The flow should always be new->scanning->complete.
	if (sl.Status != database.SkylinkStatusNew && status == database.SkylinkStatusNew) ||
		(sl.Status == database.SkylinkStatusComplete && status == database.SkylinkStatusScanning) {
		skyapi.WriteError(w, skyapi.Error{"invalid status transition"}, http.StatusBadRequest)
		return
	}
	// If the status is "complete" then we don't need to store the skylink's
	// string representation anymore, the hash is enough.
	if status == database.SkylinkStatusComplete {
		sl.Skylink = ""
	}
	sl.Status = status
	sl.Category = category
	sl.Timestamp = time.Now().UTC()
	// Save the updated record in the database.
	err = api.db.SkylinkSave(r.Context(), sl)
	if err != nil {
		skyapi.WriteError(w, skyapi.Error{err.Error()}, http.StatusInternalServerError)
		return
	}
	api.logger.Debugf("Updated skylink %s", skylink)
	skyapi.WriteSuccess(w)
}

// CategoryIsValid checks whether the category has one of the predefined valid
// values.
func CategoryIsValid(s string) bool {
	switch s {
	case database.CategorySafe:
		return true
	case database.CategoryMalicious:
		return true
	case database.CategorySuspicious:
		return true
	case database.CategoryPUA:
		return true
	}
	return false
}

// StatusIsValid checks whether the status has one of the predefined valid
// values.
func StatusIsValid(s string) bool {
	switch s {
	case database.SkylinkStatusNew:
		return true
	case database.SkylinkStatusScanning:
		return true
	case database.SkylinkStatusComplete:
		return true
	}
	return false
}

// parseSkylink parses the given string into a skylink and validates it.
func parseSkylink(s string) (*database.Skylink, error) {
	if s == "" {
		return nil, errors.New("empty skylink")
	}
	var sl database.Skylink
	err := sl.LoadString(s)
	if err != nil {
		return nil, err
	}
	return &sl, nil
}
