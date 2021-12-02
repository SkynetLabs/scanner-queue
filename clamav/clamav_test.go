package clamav

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
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
		ScanStreamValue map[string]*ScanStreamValue
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
		ScanStreamValue: map[string]*ScanStreamValue{},
	}
}

// Ping returns PingValue.
func (mss *MockStreamScanner) Ping() error {
	return mss.PingValue
}

// ScanStream returns the ScanStreamValue defined for this reader or a default.
func (mss *MockStreamScanner) ScanStream(r io.Reader, _ chan bool) (chan *clamd.ScanResult, error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	// Check if we have a pre-set value we want to return for this skylink.
	if v := mss.ScanStreamValue[string(content)]; v != nil {
		return v.ScanResultCh, v.Err
	}
	// No pre-set, just return a default response.
	ch := make(chan *clamd.ScanResult, 1)
	ch <- &clamd.ScanResult{
		Raw:         "",
		Description: "",
		Path:        "",
		Hash:        "",
		Size:        len(content),
		Status:      clamd.RES_OK,
	}
	close(ch)
	return ch, nil
}

// SetScanResult is a helper method that sets the desired response in the mock
// for the given reader.
func (mss *MockStreamScanner) SetScanResult(content string, infected bool, description string, err error) {
	// Create a response channel.
	resCh := make(chan *clamd.ScanResult, 1)
	// Set the desired status for the result - infected or not.
	status := clamd.RES_OK
	if infected {
		status = clamd.RES_FOUND
	}
	// Send the desired result on the channel.
	resCh <- &clamd.ScanResult{
		Description: description,
		Status:      status,
	}
	close(resCh)
	// Set the desired response in the mock.
	mss.ScanStreamValue[content] = &ScanStreamValue{
		ScanResultCh: resCh,
		Err:          err,
	}
}

// TestClamAV_Scan ensures ClamAV.Scan works as expected.
func TestClamAV_Scan(t *testing.T) {
	// Mock a scanner service.
	mock := NewMockStreamScanner()
	scanner := ClamAV{
		staticClam:   mock,
		staticPortal: testPortal,
	}

	abort := make(chan bool)

	/*** Error scan ***/
	// Set the response.
	content := "this reader causes an error"
	expectedErr := errors.New("the error we expect")
	mock.SetScanResult(content, false, "", expectedErr)
	// Run the scan.
	_, _, err := scanner.Scan(bytes.NewBuffer([]byte(content)), abort)
	if err != expectedErr {
		t.Fatalf("Expected error %s, got %s", expectedErr, err)
	}

	/*** Virus found ***/
	// Set the response.
	content = "this reader causes an error"
	mock.SetScanResult(content, true, "bad bad virus", nil)
	// Run the scan.
	inf, desc, err := scanner.Scan(bytes.NewBuffer([]byte(content)), abort)
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
	content = "this reader causes an error"
	mock.SetScanResult(content, false, "", nil)
	// Run the scan.
	inf, desc, err = scanner.Scan(bytes.NewBuffer([]byte(content)), abort)
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

	// Mock a scanner service.
	mock := NewMockStreamScanner()
	scanner := ClamAV{
		staticClam:   mock,
		staticPortal: testPortal,
	}

	abort := make(chan bool)
	skylink := "AQAh2vxStoSJ_M9tWcTgqebUWerCAbpMfn9xxa9E29UOuw"
	safeContent := "this is some safe content"
	maliciousContent := "this is some malicious content"

	// Test: portal returns an error.
	gock.New(testPortal).
		Get(skylink).
		Reply(http.StatusInternalServerError).
		SetError(errors.New("expected error"))
	_, _, _, _, err := scanner.ScanSkylink(skylink, abort)
	if err == nil || !strings.Contains(err.Error(), "expected error") {
		t.Fatalf("Unexpected error %s", err)
	}

	// Test: portal responds with an empty body. We expect to not error on this.
	gock.New(testPortal).
		Get(skylink).
		Reply(http.StatusOK)
	_, _, _, _, err = scanner.ScanSkylink(skylink, abort)
	if err == nil || !strings.Contains(err.Error(), "failed parsing content-length") {
		t.Fatalf("Expected error '%s', got '%s'", "failed parsing content-length", err)
	}

	// Test: invalid content-length header.
	gock.New(testPortal).
		Get(skylink).
		Reply(http.StatusOK).
		BodyString(safeContent).
		SetHeader("content-length", "bad-value")
	_, _, _, _, err = scanner.ScanSkylink(skylink, abort)
	if err == nil || !strings.Contains(err.Error(), "failed parsing content-length") {
		t.Fatalf("Expected error '%s', got '%s'", "failed parsing content-length", err)
	}

	// Test safe content.
	gock.New(testPortal).
		Get(skylink).
		Reply(http.StatusOK).
		BodyString(safeContent).
		SetHeader("content-length", strconv.Itoa(len(safeContent)))
	inf, desc, size, scannedSize, err := scanner.ScanSkylink(skylink, abort)
	if err != nil {
		t.Fatal(err)
	}
	if inf || desc != "" || size != uint64(len(safeContent)) || scannedSize != uint64(len(safeContent)) {
		t.Fatal("Unexpected return values:", inf, desc, size, scannedSize)
	}

	// Test malicious content.
	mock.SetScanResult(maliciousContent, true, "virus description", nil)
	gock.New(testPortal).
		Get(skylink).
		Reply(http.StatusOK).
		BodyString(maliciousContent).
		SetHeader("content-length", strconv.Itoa(len(maliciousContent)))
	inf, desc, size, scannedSize, err = scanner.ScanSkylink(skylink, abort)
	if err != nil {
		t.Fatal(err)
	}
	if !inf || desc != "virus description" || size != uint64(len(maliciousContent)) || scannedSize != uint64(len(maliciousContent)) {
		t.Fatal("Unexpected return values:", inf, desc, size, scannedSize)
	}
}