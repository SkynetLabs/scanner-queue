package database

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	accdb "github.com/SkynetLabs/skynet-accounts/database"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/skymodules"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.sia.tech/siad/crypto"
)

var (
	// CategorySafe means that we should not be blocking the file.
	CategorySafe = "safe"
	// CategoryMalicious means that this is a harmful file which we should block.
	CategoryMalicious = "malicious"
	// CategorySuspicious means that this file might be problematic. We should
	// block it in order to be safe, unless there's an exception for this type
	// of file.
	CategorySuspicious = "suspicious"
	// CategoryPUA means the file is a Potentially Unwanted Application. This is a
	// broad category of applications, some of which dangerous, others - not.
	CategoryPUA = "pua"

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
type Skylink struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Skylink   string             `bson:"skylink" json:"skylink"`
	Hash      crypto.Hash        `bson:"hash" json:"hash"`
	Timestamp time.Time          `bson:"timestamp" json:"timestamp"`
	Status    string             `bson:"status" json:"status"`
	Category  string             `bson:"category" json:"category"`
}

// LoadString parses a skylink from string and populates all required fields.
func (s *Skylink) LoadString(skylink string) error {
	if !accdb.ValidSkylinkHash(skylink) {
		return errors.New(ErrInvalidSkylink)
	}
	s.Skylink = skylink
	var sl skymodules.Skylink
	err := sl.LoadString(skylink)
	if err != nil {
		return errors.AddContext(err, ErrInvalidSkylink)
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

// Skylink fetches the DB record that corresponds to the given skylink from the
// database.
func (db *DB) Skylink(ctx context.Context, skylink string) (*Skylink, error) {
	sr := db.Skylinks.FindOne(ctx, bson.M{"skylink": skylink})
	if sr.Err() != nil {
		return nil, sr.Err()
	}
	var sl Skylink
	err := sr.Decode(&sl)
	if err != nil {
		return nil, err
	}
	// If the hash is empty, load the skylink from its string representation.
	if subtle.ConstantTimeCompare(sl.Hash[:], make([]byte, crypto.HashSize)) == 1 {
		err = sl.LoadString(skylink)
		if err != nil {
			return nil, err
		}
	}
	return &sl, nil
}

// SkylinkCreate creates a new skylink. If the skylink already exists it does
// nothing.
func (db *DB) SkylinkCreate(ctx context.Context, skylink *Skylink) error {
	_, err := db.Skylinks.InsertOne(ctx, skylink)
	if err != nil && strings.Contains(err.Error(), "E11000 duplicate key error collection: scanner.skylinks index: skylink_unique") {
		// This skylink already exists in the DB.
		return nil
	}
	return err
}

// SkylinkSave saves the given Skylink record to the database.
func (db *DB) SkylinkSave(ctx context.Context, skylink *Skylink) error {
	filter := bson.M{"_id": skylink.ID}
	opts := &options.ReplaceOptions{
		Upsert: &True,
	}
	_, err := db.Skylinks.ReplaceOne(ctx, filter, skylink, opts)
	if err != nil {
		return errors.AddContext(err, "failed to save")
	}
	return nil
}

// SkylinkUpdate updates a skylink. If the skylink doesn't exist, it creates it.
func (db *DB) SkylinkUpdate(ctx context.Context, skylink *Skylink) error {
	filter := bson.M{"skylink": skylink.Skylink}
	update := bson.M{
		"$set": bson.M{
			"timestamp": skylink.Timestamp,
			"status":    skylink.Status,
			"category":  skylink.Category,
		},
	}
	opts := &options.UpdateOptions{Upsert: &True}
	_, err := db.Skylinks.UpdateOne(ctx, filter, update, opts)
	return err
}
