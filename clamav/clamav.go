package clamav

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/dutchcoders/go-clamd"
	"gitlab.com/NebulousLabs/errors"
)

type (
	// ClamAV is a client that allows scanning of content for malware.
	ClamAV struct {
		staticClam   StreamScanner
		staticPortal string
	}

	// Scanner describes the interface exposed by ClamAV and it's used for
	// testing and mocking.
	Scanner interface {
		Ping() error
		PreferredPortal() string
		Scan(r io.Reader, abort chan bool) (infected bool, description string, err error)
		ScanSkylink(skylink string, abort chan bool) (infected bool, description string, size, scannedSize uint64, err error)
	}

	// Scanner describes the interface exposed by Clamd (or at elast the parts
	// we use) and it's used for testing and mocking.
	StreamScanner interface {
		Ping() error
		ScanStream(r io.Reader, abort chan bool) (chan *clamd.ScanResult, error)
	}
)

// New creates a new ClamAV client that will try to connect to the ClamAV
// service listening on a TCP socket at the given address and port. Before
// returning the client, New verifies the connection to ClamAV.
func New(clamIP, clamPort, portal string) (*ClamAV, error) {
	var err error
	defer func() {
		if err1 := recover(); err1 != nil {
			err2 := errors.New(fmt.Sprintf("error while trying to connect to ClamAV: %v", err1))
			err = errors.Compose(err, err2)
		}
	}()
	clam := &ClamAV{
		staticClam:   clamd.NewClamd(fmt.Sprintf("tcp://%s:%s", clamIP, clamPort)),
		staticPortal: portal,
	}
	err = clam.Ping()
	if err != nil {
		return nil, err
	}
	return clam, nil
}

// Ping checks the ClamAV  daemon's state.
func (c *ClamAV) Ping() error {
	return c.staticClam.Ping()
}

// PreferredPortal returns the portal ClamAV uses to download content.
func (c *ClamAV) PreferredPortal() string {
	return c.staticPortal
}

// Scan streams the content of the reader to ClamAV for malware scanning.
// It returns an `infected` flag, a description of the detected malware and an
// error.
func (c *ClamAV) Scan(r io.Reader, abort chan bool) (infected bool, description string, err error) {
	result, err := c.staticClam.ScanStream(r, abort)
	if err != nil {
		return
	}
	for s := range result {
		if s.Status == clamd.RES_FOUND {
			return true, s.Description, nil
		}
		description = s.Description
	}
	return
}

// ScanSkylink downloads the content of the given skylink and streams it to
// ClamAV for scanning. It returns an `infected` flag, a description of the
// detected malware and an error.
func (c *ClamAV) ScanSkylink(skylink string, abort chan bool) (infected bool, description string, size, scannedSize uint64, err error) {
	resp, err := http.Get(fmt.Sprintf("%s/%s", c.staticPortal, skylink))
	if err != nil {
		return
	}
	defer func() {
		if err1 := resp.Body.Close(); err1 != nil {
			log.Println(errors.AddContext(err1, "error on closing response body"))
		}
	}()
	size, err = strconv.ParseUint(resp.Header.Get("content-length"), 10, 64)
	if err != nil {
		size = 0
		err = errors.AddContext(err, "failed parsing content-length")
		return
	}
	// Wrap the body's ReadCloser in a counting reader and check how may bytes
	// have been read from it. That's how we'll know how much of the content we
	// managed to scan.
	rc := NewReaderCounter(resp.Body)
	// Scan the content.
	infected, description, err = c.Scan(rc, abort)
	scannedSize = rc.ReadBytes()
	return
}
