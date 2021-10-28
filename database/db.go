package database

import (
	"context"
	"fmt"
	"net/url"

	"github.com/SkynetLabs/skynet-accounts/database"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	// True is a helper value, so we can pass a *bool to MongoDB's methods.
	True = true

	// dbName defines the name of the database this service uses
	dbName = "scanner"
	// dbSkylinks deifnes the name of the skylinks collection
	dbSkylinks = "skylinks"
	// mongoCompressors defines the compressors we are going to use for the
	// connection to MongoDB
	mongoCompressors = "zstd,zlib,snappy"
	// mongoReadPreference defines the DB's read preference. The options are:
	// primary, primaryPreferred, secondary, secondaryPreferred, nearest.
	// See https://docs.mongodb.com/manual/core/read-preference/
	mongoReadPreference = "primary"
	// mongoWriteConcern describes the level of acknowledgment requested from
	// MongoDB.
	mongoWriteConcern = "majority"
	// mongoWriteConcernTimeout specifies a time limit, in milliseconds, for
	// the write concern to be satisfied.
	mongoWriteConcernTimeout = "1000"
)

type DB struct {
	DB       *mongo.Database
	Skylinks *mongo.Collection
	Logger   *logrus.Logger
}

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
				Options: options.Index().SetName("skylink_unique").SetUnique(true),
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
