package clamav

import (
	"bytes"
	"io"
	"testing"

	"github.com/dutchcoders/go-clamd"
	"gitlab.com/NebulousLabs/errors"
	"gopkg.in/h2non/gock.v1"
)

// testPortal is a convenience const that makes it easy to mock calls.
const testPortal = "https://siasky.test"

type (
	// MockStreamScanner is a mock of Clamd implementing the clamav.StreamScanner
	// interface. The mock has a number of exposed fields which determine the
	// results of the mocked method calls. Use those to adjust the behaviour of
	// the mock to the test situation you wish to model.
	//
	// NOTE: All channels set as return values need to be buffered and closed.
	MockStreamScanner struct {
		PingValue       error
		ScanStreamValue map[io.Reader]*ScanStreamValue
	}

	// ScanStreamValue is a helper type for setting the response of the
	// ScanStream() method.
	//
	// NOTE: All channels set as return values need to be buffered and closed.
	ScanStreamValue struct {
		ScanResultCh chan *clamd.ScanResult
		Err          error
	}
)

// NewMockStreamScanner creates a new mock.
func NewMockStreamScanner() *MockStreamScanner {
	return &MockStreamScanner{
		PingValue:       nil,
		ScanStreamValue: map[io.Reader]*ScanStreamValue{},
	}
}

// Ping returns PingValue.
func (mss MockStreamScanner) Ping() error {
	return mss.PingValue
}

// ScanStream returns the ScanStreamValue defined for this reader or a default.
func (mss MockStreamScanner) ScanStream(r io.Reader, _ chan bool) (chan *clamd.ScanResult, error) {
	// Check if we have a pre-set value we want to return for this skylink.
	if v := mss.ScanStreamValue[r]; v != nil {
		return v.ScanResultCh, v.Err
	}
	// No pre-set, just return a default response.
	ch := make(chan *clamd.ScanResult, 1)
	ch <- &clamd.ScanResult{
		Raw:         "",
		Description: "",
		Path:        "",
		Hash:        "",
		Size:        1 << 20,
		Status:      clamd.RES_OK,
	}
	close(ch)
	return ch, nil
}

// TestClamAV_Scan ensures ClamAV.Scan works as expected.
func TestClamAV_Scan(t *testing.T) {
	// Mock a scanner service.
	mock := NewMockStreamScanner()
	c := ClamAV{
		staticClam:   mock,
		staticPortal: testPortal,
	}

	abort := make(chan bool)

	/*** Error scan ***/
	// Set the response.
	reader := bytes.NewBuffer([]byte("this reader causes an error"))
	resCh := make(chan *clamd.ScanResult, 1)
	close(resCh)
	res := &ScanStreamValue{
		ScanResultCh: resCh,
		Err:          errors.New("the error we expect"),
	}
	mock.ScanStreamValue[reader] = res
	// Run the scan.
	_, _, err := c.Scan(reader, abort)
	if err != res.Err {
		t.Fatalf("Expected error %s, got %s", res.Err, err)
	}

	/*** Virus found ***/
	// Set the response.
	reader = bytes.NewBuffer([]byte("this reader causes an error"))
	resCh = make(chan *clamd.ScanResult, 1)
	resCh <- &clamd.ScanResult{
		Description: "bad bad virus",
		Status:      clamd.RES_FOUND,
	}
	close(resCh)
	res = &ScanStreamValue{
		ScanResultCh: resCh,
		Err:          nil,
	}
	mock.ScanStreamValue[reader] = res
	// Run the scan.
	inf, desc, err := c.Scan(reader, abort)
	if err != nil {
		t.Fatal(err)
	}
	if !inf {
		t.Fatalf("Expected the file to be marked as infected.")
	}
	if desc != "bad bad virus" {
		t.Fatalf("Unexpected description '%s'", desc)
	}

	/*** No virus found ***/
	// Set the response.
	reader = bytes.NewBuffer([]byte("this reader causes an error"))
	resCh = make(chan *clamd.ScanResult, 1)
	resCh <- &clamd.ScanResult{
		Status: clamd.RES_OK,
	}
	close(resCh)
	res = &ScanStreamValue{
		ScanResultCh: resCh,
		Err:          nil,
	}
	mock.ScanStreamValue[reader] = res
	// Run the scan.
	inf, desc, err = c.Scan(reader, abort)
	if err != nil {
		t.Fatal(err)
	}
	if inf {
		t.Fatalf("Expected the file to be marked as clean.")
	}
	if desc != "" {
		t.Fatalf("Unexpected description '%s'", desc)
	}
}

func TestClamAV_ScanSkylink(t *testing.T) {
	defer gock.Off()

	/*
		TODO
		 - GET request returns error
		 - GET request returns nil body. can this happen?
		 - bad content length
		 - virus found (check scanned size)
		 - no virus found (check scanned size)
	*/

}