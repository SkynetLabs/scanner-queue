package scanner

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/SkynetLabs/malware-scanner/clamav"
	"github.com/SkynetLabs/malware-scanner/database"
	"github.com/SkynetLabs/skynet-accounts/build"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
)

var (
	// sleepBetweenScans defines how long the scanner should sleep after
	// scanning the DB and not finding any skylinks to scan.
	sleepBetweenScans = build.Select(
		build.Var{
			Dev:      time.Second,
			Testing:  100 * time.Millisecond,
			Standard: 10 * time.Second,
		},
	).(time.Duration)
)

// Scanner provides a convenient interface for working with ClamAV
type Scanner struct {
	staticCtx    context.Context
	staticDB     *database.DB
	staticClamav *clamav.ClamAV
	staticLogger *logrus.Logger
}

// New returns a new Scanner with the given parameters.
func New(ctx context.Context, db *database.DB, clam *clamav.ClamAV, logger *logrus.Logger) *Scanner {
	return &Scanner{
		staticCtx:    ctx,
		staticDB:     db,
		staticClamav: clam,
		staticLogger: logger,
	}
}

// SweepAndScan sweeps the DB for new skylinks, locks them, scans them,
// and updates their records in the DB.
func (s Scanner) SweepAndScan(abort chan bool) {
	sl, err := s.staticDB.SweepAndLock(s.staticCtx)
	if err != nil && !errors.Contains(err, database.ErrNoDocumentsFound) {
		s.staticLogger.Warnf("error while trying to lock a new record: %s", err)
		return
	}
	if errors.Contains(err, database.ErrNoDocumentsFound) {
		// TODO This should trigger a sleep
		return
	}
	if sl.Skylink == "" {
		build.Critical(fmt.Sprintf("SweepAndLock returned a record with an empty skylink. Record hash: %s", hex.EncodeToString(sl.Hash[:])))
		return
	}
	inf, desc, err := s.staticClamav.ScanSkylink(sl.Skylink, abort)
	if err != nil {
		// Scanning failed, log the error and unlock the record for another attempt.
		s.staticLogger.Debugln(errors.AddContext(err, "scanning failed"))
		sl.Status = database.SkylinkStatusNew
		sl.Timestamp = time.Now().UTC()
		err = s.staticDB.SkylinkSave(s.staticCtx, sl)
		if err != nil {
			s.staticLogger.Debugln(errors.AddContext(err, "unlocking a skylink failed"))
		}
		return
	}
	// Once the scanning is complete, we no longer need the skylink. We want to
	// remove it and only keep its hash. We don't want our database to be an
	// index of nasty files.
	sl.Skylink = ""
	sl.Status = database.SkylinkStatusComplete
	sl.Infected = inf
	sl.InfectionDescription = desc
	sl.Timestamp = time.Now().UTC()
	err = s.staticDB.SkylinkSave(s.staticCtx, sl)
	if err != nil {
		s.staticLogger.Debugln(errors.AddContext(err, "updating a skylink's status failed"))
	}
}

// Start launches a background task that periodically scans the database for
// new skylink records and sends them for scanning.
func (s Scanner) Start() {
	// abort channel which interrupts the current scanning operation
	abort := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-s.staticCtx.Done():
				// interrupt the current scan and exit
				abort <- true
				return
			case <-time.After(sleepBetweenScans):
				s.SweepAndScan(abort)
			}
		}
	}()
}

// TODO Write an "unlock" daemon.
