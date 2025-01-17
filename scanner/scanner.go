package scanner

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"time"

	blockapi "github.com/SkynetLabs/blocker/api"
	blockdb "github.com/SkynetLabs/blocker/database"
	"github.com/SkynetLabs/malware-scanner/clamav"
	"github.com/SkynetLabs/malware-scanner/database"
	"github.com/SkynetLabs/skynet-accounts/build"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	// malwareTag marks the skylink as blocked by malware-scanner, as opposed to
	// user-reported malware.
	malwareTag = "malware-scanner"
)

var (
	// BlockerIP is the IP of the blocker service.
	// Set according to the BLOCKER_IP env var.
	BlockerIP string
	// BlockerPort is the port of the blocker service.
	// Set according to the BLOCKER_PORT env var.
	BlockerPort string

	// sleepBetweenReports defines how long the scanner should sleep after
	// scanning the DB and not finding any skylinks to report to blocker.
	sleepBetweenReports = build.Select(
		build.Var{
			Dev:      30 * time.Second,
			Testing:  100 * time.Millisecond,
			Standard: 10 * time.Minute,
		},
	).(time.Duration)
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

// SweepAndBlock scans the database for malicious skylinks that haven't been
// reported to blocker yet and reports them. It doesn't lock the records because
// it isn't needed.
func (s Scanner) SweepAndBlock() (int, error) {
	var count int
	filter := bson.M{
		"status":  database.SkylinkStatusUnreported,
		"skylink": bson.M{"$ne": ""},
	}
	update := bson.M{
		"$set": bson.M{
			"skylink": "",
			"status":  database.SkylinkStatusComplete,
		},
	}
	var sl database.Skylink

	// Continue finding skylinks and reporting them while there are skylinks to
	// report.
	for {
		// Find a malicious skylink to report.
		sr := s.staticDB.FindOneSkylink(s.staticCtx, filter)
		if sr.Err() == mongo.ErrNoDocuments {
			// no more records to report
			break
		}
		if sr.Err() != nil {
			return count, errors.AddContext(sr.Err(), "failed to fetch malicious skylink from db")
		}
		err := sr.Decode(&sl)
		if err != nil {
			s.staticLogger.Errorf("Failed to deserialize skylink from DB into a var. Error: '%s'", err.Error())
			return count, err
		}
		// Report the skylink to blocker.
		s.staticLogger.Infof("Reporting skylink '%s' as malicious with description '%s'", sl.Skylink, sl.InfectionDescription)
		err = reportToBlocker(sl.Skylink)
		if err != nil {
			return count, errors.AddContext(err, "blocker error")
		}
		// Mark the skylink as reported and remove the skylink from the record.
		_, err = s.staticDB.UpdateOneSkylink(s.staticCtx, bson.M{"_id": sl.ID}, update)
		if err != nil {
			return count, errors.AddContext(err, "failed to update the skylink's status in db")
		}
		count++
	}
	return count, nil
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
	sl.Status = database.SkylinkStatusUnreported
	if !inf {
		// The skylink is not infected, so we can already clean up its skylink
		// and mark our work with it as done. If that wasn't the case, we would
		// have left the skylink present until it's reported to blocker.
		sl.Skylink = ""
		sl.Status = database.SkylinkStatusComplete
	}
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
	// abort channel which interrupts the current scanning operation
	abort := make(chan bool)

	// Start a thread that watches the context and immediately closes the abort
	// channel when the context is closed. This will immediately (or at least as
	// quickly as ClamAV allows) terminate the current scan and allow for a
	// quick and clean service shutdown.
	go func() {
		<-s.staticCtx.Done()
		close(abort)
	}()

	// Start the scanning loop.
	go func() {
		// sleepLength defines how long the thread will sleep before scanning
		// the next skylink. Its value is controlled by SweepAndScan - while we
		// keep finding files to scan, we'll keep this sleep at zero. Once we
		// run out of files to scan we'll reset it to its full duration of
		// sleepBetweenScans.
		sleepLength := sleepBetweenScans
		first := true
		for {
			numSubsequentErrs := 0
			if !first {
				select {
				case <-s.staticCtx.Done():
					return
				case <-time.After(sleepLength):
				}
			}
			first = false
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
	}()

	// Start the reporting loop.
	// This loop will look for skylinks that are detected as malicious and will
	// report them to the blocker service, so they can be immediately blocked on
	// all portals.
	go func() {
		first := true
		for {
			if !first {
				select {
				case <-s.staticCtx.Done():
					return
				case <-time.After(sleepBetweenReports):
				}
			}
			first = false
			n, err := s.SweepAndBlock()
			if err != nil {
				s.staticLogger.Infof("SweepAndBlock blocked %d malicious skylinks before it encountered an error: %s", n, err.Error())
			} else {
				s.staticLogger.Tracef("SweepAndBlock blocked %d malicious skylinks.", n)
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
			}
			n, err := s.staticDB.CancelStuckScans(s.staticCtx)
			if err != nil {
				s.staticLogger.Debugln(errors.AddContext(err, "error while trying to cancel stuck scans"))
			} else {
				s.staticLogger.Traceln(fmt.Sprintf("successfully cancelled %d stuck scans", n))
			}
		}
	}()
}

// reportToBlocker calls the blocker service and instructs it to block the given
// skylink as malware.
func reportToBlocker(skylink string) error {
	body := blockapi.BlockPOST{
		Skylink: skylink,
		Reporter: blockdb.Reporter{
			Name: "Malware Scanner",
		},
		Tags: []string{malwareTag},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return errors.AddContext(err, "failed to build request body")
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%s/block", BlockerIP, BlockerPort), bytes.NewBuffer(bodyBytes))
	if err != nil {
		return errors.AddContext(err, "failed to build blocker request")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.AddContext(err, "failed to call blocker")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := ioutil.ReadAll(res.Body)
		return errors.New(fmt.Sprintf("blocker failed. status code %d, body: '%s'", res.StatusCode, string(b)))
	}
	return nil
}
