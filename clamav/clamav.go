package clamav

import (
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/dutchcoders/go-clamd"
	"gitlab.com/NebulousLabs/errors"
)

// ClamAV is a client that allows scanning of content for malware.
type ClamAV struct {
	staticClam   *clamd.Clamd
	staticPortal string
}

// New creates a new ClamAV client that will try to connect to the ClamAV
// service listening on a TCP socket at the given address and port. Before
// returning the client, New verifies the connection to ClamAV.
func New(clamIP, clamPort, portal string) (*ClamAV, error) {
	clam := &ClamAV{
		staticClam:   clamd.NewClamd(fmt.Sprintf("tcp://%s:%s", clamIP, clamPort)),
		staticPortal: portal,
	}
	err := clam.Ping()
	if err != nil {
		return nil, err
	}
	return clam, nil
}

// Ping checks the ClamAV  daemon's state.
func (c *ClamAV) Ping() error {
	return c.staticClam.Ping()
}

// Scan streams the content of the reader to ClamAV for malware scanning.
// It returns an `infected` flag, a description of the detected malware and an
// error.
func (c *ClamAV) Scan(r io.Reader, abort chan bool) (infected bool, description string, err error) {
	response, err := c.staticClam.ScanStream(r, abort)
	if err != nil {
		return
	}
	for s := range response {
		if s.Status == clamd.RES_FOUND {
			return true, s.Description, nil
		}
	}
	return
}

// ScanSkylink downloads the content of the given skylink and streams it to
// ClamAV for scanning. It returns an `infected` flag, a description of the
// detected malware and an error.
func (c *ClamAV) ScanSkylink(skylink string, abort chan bool) (infected bool, description string, err error) {
	resp, err := http.Get(fmt.Sprintf("%s/skynet/skylink/%s", c.staticPortal, skylink))
	if err != nil {
		return
	}
	defer func() {
		if err = resp.Body.Close(); err != nil {
			log.Println(errors.AddContext(err, "error on closing response body"))
		}
	}()
	return c.Scan(io.Reader(resp.Body), abort)
}