package scanner

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
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
			Dev:      30 * time.Second,
			Testing:  100 * time.Millisecond,
			Standard: 10 * time.Second,
		},
	).(time.Duration)
	// sleepOnErrStep defines the base step for sleeping after encountering an
	// error. We'll increase the sleep by an order of magnitude on each
	// subsequent error until sleepOnErrSteps.
	sleepOnErrStep = 100 * time.Millisecond
	// sleepOnErrSteps is the maximum number of times we're going to increment
	// the sleep-on-error length.
	sleepOnErrSteps = 3
)

// Scanner provides a convenient interface for working with ClamAV
type Scanner struct {
	staticCtx    context.Context
	staticDB     *database.DB
	staticClam   *clamav.ClamAV
	staticLogger *logrus.Logger
}

// New returns a new Scanner with the given parameters.
func New(ctx context.Context, db *database.DB, clam *clamav.ClamAV, logger *logrus.Logger) (*Scanner, error) {
	if ctx == nil {
		return nil, errors.New("invalid context provided")
	}
	if db == nil {
		return nil, errors.New("invalid DB provided")
	}
	if clam == nil {
		return nil, errors.New("invalid ClamAV instance provided")
	}
	if logger == nil {
		return nil, errors.New("invalid logger provided")
	}
	return &Scanner{
		staticCtx:    ctx,
		staticDB:     db,
		staticClam:   clam,
		staticLogger: logger,
	}, nil
}

// SweepAndScan sweeps the DB for new skylinks, locks them, scans them,
// and updates their records in the DB.
func (s Scanner) SweepAndScan(abort chan bool) error {
	sl, err := s.staticDB.SweepAndLock(s.staticCtx)
	if err != nil {
		if !errors.Contains(err, database.ErrNoDocumentsFound) {
			s.staticLogger.Warnf("error while trying to lock a new record: %s", err)
		}
		return err
	}
	if sl.Skylink == "" {
		s.staticLogger.Warnf("SweepAndLock returned a record with an empty skylink. Record hash: %s", hex.EncodeToString(sl.Hash[:]))
		return errors.New("empty skylink")
	}
	inf, desc, size, scannedSize, err := s.staticClam.ScanSkylink(sl.Skylink, abort)
	if err != nil {
		// Scanning failed, log the error and unlock the record for another attempt.
		s.staticLogger.Debugln(errors.AddContext(err, "scanning failed"))
		sl.Status = database.SkylinkStatusNew
		sl.Timestamp = time.Now().UTC()
		err = s.staticDB.SkylinkSave(s.staticCtx, sl)
		if err != nil {
			s.staticLogger.Debugln(errors.AddContext(err, "unlocking a skylink failed"))
		}
		return err
	}
	// Sanity check: scannedSize vs size.
	if scannedSize > size {
		s.staticLogger.Warnf("Scanned size (%d bytes) is more than the content size (%d bytes) for skylink %s", scannedSize, size, sl.Skylink)
	}
	// Once the scanning is complete, we no longer need the skylink. We want to
	// remove it and only keep its hash. We don't want our database to be an
	// index of nasty files.
	sl.Skylink = ""
	sl.Status = database.SkylinkStatusComplete
	sl.Infected = inf
	sl.InfectionDescription = desc
	sl.Size = size
	sl.ScannedAllContent = scannedSize == size
	sl.ScannedAllOffsets = false
	sl.Timestamp = time.Now().UTC()
	err = s.staticDB.SkylinkSave(s.staticCtx, sl)
	if err != nil {
		s.staticLogger.Debugln(errors.AddContext(err, "updating a skylink's status failed"))
	}
	return err
}

// Start launches a background task that periodically scans the database for
// new skylink records and sends them for scanning.
func (s Scanner) Start() {
	go func() {
		// abort channel which interrupts the current scanning operation
		abort := make(chan bool)
		// sleepLength defines how long the thread will sleep before scanning
		// the next skylink. Its value is controlled by SweepAndScan - while we
		// keep finding files to scan, we'll keep this sleep at zero. Once we
		// run out of files to scan we'll reset it to its full duration of
		// sleepBetweenScans.
		sleepLength := sleepBetweenScans
		for {
			numSubsequentErrs := 0
			select {
			case <-s.staticCtx.Done():
				close(abort)
				return
			case <-time.After(sleepLength):
				err := s.SweepAndScan(abort)
				if errors.Contains(err, database.ErrNoDocumentsFound) {
					// This was a successful call, so the number of subsequent
					// errors is reset and we sleep for a pre-determined period
					// in waiting for new skylinks to be uploaded.
					sleepLength = sleepBetweenScans
					numSubsequentErrs = 0
				} else if err != nil {
					// On error, we sleep for an increasing amount of time -
					// from 100ms on the first error to 100s on the fourth and
					// subsequent errors.
					sleepLength = sleepOnErrStep * time.Duration(math.Pow10(numSubsequentErrs))
					numSubsequentErrs++
					if numSubsequentErrs > sleepOnErrSteps {
						numSubsequentErrs = sleepOnErrSteps
					}
				} else {
					// A successful scan. Reset the number of subsequent errors.
					numSubsequentErrs = 0
					// No need to sleep after a successful scan.
					sleepLength = 0
				}
			}
		}
	}()
}

// StartUnlocker launches a background thread that periodically scans the
// database and resets the state of potentially stuck scans. If a scan has been
// initiated too long ago it will put it back in "new" state, so it can be
// retried.
func (s Scanner) StartUnlocker() {
	go func() {
		ticker := time.NewTicker(database.ScanTimeout)
		for {
			select {
			case <-s.staticCtx.Done():
				return
			case <-ticker.C:
				n, err := s.staticDB.CancelStuckScans(s.staticCtx)
				if err != nil {
					s.staticLogger.Debugln(errors.AddContext(err, "error while trying to cancel stuck scans"))
				} else {
					s.staticLogger.Traceln(fmt.Sprintf("successfully cancelled %d stuck scans", n))
				}
			}
		}
	}()
}
