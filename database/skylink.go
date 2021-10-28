package database

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Skylink struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Skylink   string             `bson:"skylink" json:"skylink"`
	Timestamp time.Time          `bson:"timestamp" json:"timestamp"`
	Scanned   bool               `bson:"scanned" json:"scanned"`
}

// SkylinkCreate creates a new skylink. If the skylink already exists it does
// nothing.
func (db *DB) SkylinkCreate(ctx context.Context, skylink Skylink) error {
	_, err := db.Skylinks.InsertOne(ctx, skylink)
	if err != nil && strings.Contains(err.Error(), "E11000 duplicate key error collection: scanner.skylinks index: skylink_unique") {
		// This skylink already exists in the DB.
		return nil
	}
	return err
}

// SkylinkUpdate updates a skylink. If the skylink doesn't exist, it creates it.
func (db *DB) SkylinkUpdate(ctx context.Context, skylink Skylink) error {
	filter := bson.M{"skylink": skylink.Skylink}
	update := bson.M{
		"$set": bson.M{
			"scanned":   skylink.Scanned,
			"timestamp": skylink.Timestamp,
		},
	}
	opts := &options.UpdateOptions{Upsert: &True}
	_, err := db.Skylinks.UpdateOne(ctx, filter, update, opts)
	return err
}
