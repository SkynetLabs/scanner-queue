package database

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/SkynetLabs/skynet-accounts/database"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.sia.tech/siad/crypto"
)

var (
	// CancelScanAfter defines how long we want to wait for a scan to finish
	// before giving up on it and returning the skylink back into the "new"
	// bucket, so the scan can be retried. This prevents scans from hanging
	// forever in case the scanning server crashed or otherwise failed to
	// either finish the scan or report its findings.
	CancelScanAfter = time.Hour

	// ErrNoDocumentsFound is returned when a database operation completes
	// successfully but it doesn't find or affect any documents.
	ErrNoDocumentsFound = errors.New("no documents found")

	// True is a helper value, so we can pass a *bool to MongoDB's methods.
	True = true

	// dbName defines the name of the database this service uses
	dbName = "scanner"
	// dbSkylinks defines the name of the skylinks collection
	dbSkylinks = "skylinks"
	// mongoCompressors defines the compressors we are going to use for the
	// connection to MongoDB
	mongoCompressors = "zstd,zlib,snappy"
	// mongoReadPreference defines the DB's read preference. The options are:
	// primary, primaryPreferred, secondary, secondaryPreferred, nearest.
	// See https://docs.mongodb.com/manual/core/read-preference/
	mongoReadPreference = "nearest"
	// mongoWriteConcern describes the level of acknowledgment requested from
	// MongoDB.
	mongoWriteConcern = "majority"
	// mongoWriteConcernTimeout specifies a time limit, in milliseconds, for
	// the write concern to be satisfied.
	mongoWriteConcernTimeout = "1000"
)

// DB holds a connection to the database, as well as helpful shortcuts to
// collections and utilities.
type DB struct {
	DB       *mongo.Database
	Skylinks *mongo.Collection
	Logger   *logrus.Logger
}

// New creates a new database connection.
func New(ctx context.Context, creds database.DBCredentials, logger *logrus.Logger) (*DB, error) {
	if logger == nil {
		logger = &logrus.Logger{}
	}
	connStr := fmt.Sprintf(
		"mongodb://%s:%s@%s:%s/?compressors=%s&readPreference=%s&w=%s&wtimeoutMS=%s",
		url.QueryEscape(creds.User),
		url.QueryEscape(creds.Password),
		creds.Host,
		creds.Port,
		mongoCompressors,
		mongoReadPreference,
		mongoWriteConcern,
		mongoWriteConcernTimeout,
	)
	c, err := mongo.NewClient(options.Client().ApplyURI(connStr))
	if err != nil {
		return nil, errors.AddContext(err, "failed to create a new db client")
	}
	err = c.Connect(ctx)
	if err != nil {
		return nil, errors.AddContext(err, "failed to connect to db")
	}
	db := c.Database(dbName)
	err = ensureDBSchema(ctx, db, logger)
	if err != nil {
		return nil, err
	}
	return &DB{
		DB:       db,
		Skylinks: db.Collection(dbSkylinks),
		Logger:   logger,
	}, nil
}

// Ping sends a ping command to verify that the client can connect to the DB and
// specifically to the primary.
func (db *DB) Ping(ctx context.Context) error {
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.DB.Client().Ping(ctx2, readpref.Primary())
}

// Skylink fetches the DB record that corresponds to the given skylink from the
// database.
func (db *DB) Skylink(ctx context.Context, hash crypto.Hash) (*Skylink, error) {
	sr := db.Skylinks.FindOne(ctx, bson.M{"hash": hash})
	if sr.Err() != nil {
		return nil, sr.Err()
	}
	var sl Skylink
	err := sr.Decode(&sl)
	if err != nil {
		return nil, err
	}
	return &sl, nil
}

// SkylinkByID fetches the DB record that corresponds to the given skylink by
// its DB ID.
func (db *DB) SkylinkByID(ctx context.Context, id primitive.ObjectID) (*Skylink, error) {
	sr := db.Skylinks.FindOne(ctx, bson.M{"_id": id})
	if sr.Err() != nil {
		return nil, sr.Err()
	}
	var sl Skylink
	err := sr.Decode(&sl)
	if err != nil {
		return nil, err
	}
	return &sl, nil
}

// SkylinkCreate creates a new skylink. If the skylink already exists it does
// nothing.
func (db *DB) SkylinkCreate(ctx context.Context, skylink *Skylink) error {
	// Check if a skylink with this hash already exists in the DB.
	sr := db.Skylinks.FindOne(ctx, bson.M{"hash": skylink.Hash})
	if sr.Err() != mongo.ErrNoDocuments {
		return errors.New("skylink already exists")
	}
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

// CancelStuckScans resets the status of scans that have been going on for more
// than scanner.CancelScanAfter. We assume that these scans have terminated
// unexpectedly without reporting their results (e.g. server crash).
func (db *DB) CancelStuckScans(ctx context.Context) (int64, error) {
	filter := bson.M{
		"status":    SkylinkStatusScanning,
		"timestamp": bson.M{"$gt": time.Now().Add(-CancelScanAfter)},
	}
	update := bson.M{
		"$set": bson.M{
			"timestamp": time.Now().UTC(),
			"status":    SkylinkStatusNew,
		},
	}
	ur, err := db.Skylinks.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, err
	}
	return ur.ModifiedCount, nil
}

// SweepAndLock sweeps the database for new skylinks. It "locks" and returns the
// first one it encounters. The "locking" is done by updating the skylink's
// status from "new" to "scanning".
func (db *DB) SweepAndLock(ctx context.Context) (*Skylink, error) {
	filter := bson.M{"status": SkylinkStatusNew}
	update := bson.M{
		"$set": bson.M{
			"timestamp": time.Now().UTC(),
			"status":    SkylinkStatusScanning,
		},
	}
	// Look for a single new record and change its status to "scanning".
	ur, err := db.Skylinks.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, err
	}
	if ur.UpsertedCount == 0 {
		return nil, ErrNoDocumentsFound
	}
	// Return the full record, so we can scan the skylink.
	skylinkID := ur.UpsertedID.(primitive.ObjectID)
	return db.SkylinkByID(ctx, skylinkID)
}

// ensureDBSchema checks that we have all collections and indexes we need and
// creates them if needed.
// See https://docs.mongodb.com/manual/indexes/
// See https://docs.mongodb.com/manual/core/index-unique/
func ensureDBSchema(ctx context.Context, db *mongo.Database, log *logrus.Logger) error {
	// schema defines a mapping between a collection name and the indexes that
	// must exist for that collection.
	schema := map[string][]mongo.IndexModel{
		dbSkylinks: {
			{
				Keys:    bson.D{{"skylink", 1}},
				Options: options.Index().SetName("skylink"),
			},
			{
				Keys:    bson.D{{"hash", 1}},
				Options: options.Index().SetName("hash_unique").SetUnique(true),
			},
			{
				Keys:    bson.D{{"timestamp", 1}},
				Options: options.Index().SetName("timestamp"),
			},
			{
				Keys:    bson.D{{"scanned", 1}},
				Options: options.Index().SetName("scanned"),
			},
		},
	}

	for collName, models := range schema {
		coll, err := ensureCollection(ctx, db, collName)
		if err != nil {
			return err
		}
		iv := coll.Indexes()
		var names []string
		names, err = iv.CreateMany(ctx, models)
		if err != nil {
			return errors.AddContext(err, "failed to create indexes")
		}
		log.Debugf("Ensured index exists: %v", names)
	}
	return nil
}

// ensureCollection gets the given collection from the
// database and creates it if it doesn't exist.
func ensureCollection(ctx context.Context, db *mongo.Database, collName string) (*mongo.Collection, error) {
	coll := db.Collection(collName)
	if coll == nil {
		err := db.CreateCollection(ctx, collName)
		if err != nil {
			return nil, err
		}
		coll = db.Collection(collName)
		if coll == nil {
			return nil, errors.New("failed to create collection " + collName)
		}
	}
	return coll, nil
}
