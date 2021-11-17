package database

import (
	"fmt"
	"net/http"
	"time"

	accdb "github.com/SkynetLabs/skynet-accounts/database"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/skymodules"
	"gitlab.com/SkynetLabs/skyd/skymodules/renter"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.sia.tech/siad/crypto"
)

var (
	// ErrInvalidSkylink is the error returned when the passed skylink is
	// invalid.
	ErrInvalidSkylink = "invalid skylink"

	// SkylinkStatusNew is the status of the skylink when it's created.
	SkylinkStatusNew = "new"
	// SkylinkStatusScanning is the status of the skylink while it's being
	// scanned.
	SkylinkStatusScanning = "scanning"
	// SkylinkStatusComplete is the status of the skylink after it's scanned.
	SkylinkStatusComplete = "complete"
)

// Skylink represents a skylink in the queue and holds its scanning status.
//
// ClamAV typically limits the amount of data it scans, e.g. it would only scan
// the first 16MiB of data in a given file. ScannedAllContent marks if we've
// managed to scan all the content or just its beginning.
//
// Multiple skylinks can point to the same merkle root but use different offsets
// on the data. Since we're blocking the entire merkle root, we should be
// scanning all possible (for the size of the data) offsets. ScannedAllOffsets
// marks if we have done that or not.
//
// Timestamp marks the last status change that happened to the record. It
// can be the time when it was created, locked for scanning, or scanned.
type Skylink struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Hash                 crypto.Hash        `bson:"hash" json:"hash"`
	Skylink              string             `bson:"skylink" json:"skylink"`
	Status               string             `bson:"status" json:"status"`
	Infected             bool               `bson:"infected" json:"infected"`
	InfectionDescription string             `bson:"infection_description" json:"infectionDescription"`
	ScannedAllContent    bool               `bson:"scanned_all_content" json:"scannedAllContent"`
	ScannedAllOffsets    bool               `bson:"scanned_all_offsets" json:"scannedAllOffsets"`
	Size                 uint64             `bson:"size" json:"size"`
	Timestamp            time.Time          `bson:"timestamp" json:"timestamp"`
}

// LoadString parses a skylink from string and populates all required fields.
func (s *Skylink) LoadString(skylink, portal string) error {
	if !accdb.ValidSkylinkHash(skylink) {
		return errors.New(ErrInvalidSkylink)
	}
	s.Skylink = skylink
	var sl skymodules.Skylink
	err := sl.LoadString(skylink)
	if err != nil {
		return errors.AddContext(err, ErrInvalidSkylink)
	}

	switch {
	case sl.IsSkylinkV1():
		s.Hash = crypto.HashObject(sl.MerkleRoot())
	case sl.IsSkylinkV2():
		slv1, err := resolveSkylinkV2(sl, portal)
		if err != nil {
			return errors.AddContext(err, "unable to resolve v2 skylink")
		}
		s.Hash = crypto.HashObject(slv1.MerkleRoot())
	default:
		return renter.ErrInvalidSkylinkVersion
	}

	s.Hash = crypto.HashObject(sl.MerkleRoot())
	if s.Timestamp.IsZero() {
		s.Timestamp = time.Now().UTC()
	}
	if s.Status == "" {
		s.Status = SkylinkStatusNew
	}
	return nil
}

// resolveSkylinkV2 returns the v1 skylink to which the given v2 skylink is
// currently pointing. Resolves up to three levels of nested v2 skylinks.
func resolveSkylinkV2(s skymodules.Skylink, portal string) (*skymodules.Skylink, error) {
	return recursivelyResolveSkylinkV2(s, portal, 3)
}

// recursivelyResolveSkylinkV2 resolves a v2 skylink to the v1 skylink it points
// to. If the skylink points to another skylink v2 it will recursively try
// again until it runs out of attempts.
func recursivelyResolveSkylinkV2(s skymodules.Skylink, portal string, attemptsLeft int) (*skymodules.Skylink, error) {
	if attemptsLeft < 1 {
		return nil, errors.New("v2 skylinks are nested too deeply")
	}
	if !s.IsSkylinkV2() {
		return nil, renter.ErrInvalidSkylinkVersion
	}
	resp, err := http.Head(fmt.Sprintf("%s/%s", portal, s.String()))
	if err != nil {
		return nil, errors.AddContext(err, fmt.Sprintf("failed to download metadata for skylink %s", s.String()))
	}
	skylinkHeader := resp.Header.Get("skynet-skylink")
	if skylinkHeader == "" {
		return nil, errors.New("empty skynet-skylink header")
	}
	var sl skymodules.Skylink
	err = sl.LoadString(skylinkHeader)
	if err != nil {
		return nil, err
	}
	// As it's possible for a v2 skylink to point to another v2 skylink, we will
	// do a  recursive call.
	if sl.IsSkylinkV2() {
		return recursivelyResolveSkylinkV2(sl, portal, attemptsLeft-1)
	}
	return &sl, nil
}
