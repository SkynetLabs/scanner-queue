package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/SkynetLabs/malware-scanner/api"
	"github.com/SkynetLabs/malware-scanner/database"
	"github.com/SkynetLabs/malware-scanner/scanner"
	"github.com/SkynetLabs/skynet-accounts/test"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"go.sia.tech/siad/build"
)

var (
	testServiceAddr = "http://127.0.0.1"
	testServicePort = "6000"
)

type (
	// MalwareScannerTester is a simple testing kit for malware-scanner.
	// It starts a testing instance of the service and provides simplified ways
	// to call the handlers.
	MalwareScannerTester struct {
		Ctx    context.Context
		DB     *database.DB
		Logger *logrus.Logger

		Portal string

		cancel context.CancelFunc
	}
)

// NewMalwareScannerTester creates and starts a new MalwareScannerTester
// service. Use the Close method for a graceful shutdown.
func NewMalwareScannerTester(dbName string) (*MalwareScannerTester, error) {
	ctx := context.Background()
	logger := logrus.New()

	// Connect to the test database.
	db, err := database.NewCustomDB(ctx, dbName, test.DBTestCredentials(), logger)
	if err != nil {
		return nil, errors.AddContext(err, "failed to connect to the DB")
	}

	// TODO Should this be localhost?
	portal := testServiceAddr // "https://siasky.test"

	clam := NewMockClam(portal)

	ctxWithCancel, cancel := context.WithCancel(ctx)

	// Initialise and start the background scanner task.
	scan := scanner.New(ctx, db, clam, logger)
	scan.Start()
	// Start the background thread that resets the status of scans that take
	// too long and are considered stuck.
	scan.StartUnlocker()

	// Initialise the server.
	server, err := api.New(db, clam, logger)
	if err != nil {
		cancel()
		log.Fatal(errors.AddContext(err, "failed to build the api"))
	}

	// Start the HTTP server in a goroutine and gracefully stop it once the
	// cancel function is called and the context is closed.
	srv := &http.Server{
		Addr:    ":" + testServicePort,
		Handler: server,
	}
	go func() {
		_ = srv.ListenAndServe()
	}()
	go func() {
		select {
		case <-ctxWithCancel.Done():
			_ = srv.Shutdown(context.TODO())
		}
	}()

	mst := &MalwareScannerTester{
		Ctx:    ctxWithCancel,
		DB:     db,
		Logger: logger,
		Portal: portal,

		cancel: cancel,
	}
	// Wait for the accounts tester to be fully ready.
	err = build.Retry(50, time.Millisecond, func() error {
		_, _, err = mst.Get("/health", nil)
		return err
	})
	if err != nil {
		return nil, errors.AddContext(err, "failed to start malware-scanner tester in the given time")
	}
	return mst, nil
}

// Get executes a GET request against the test service.
func (mst *MalwareScannerTester) Get(endpoint string, params url.Values) (r *http.Response, body []byte, err error) {
	return mst.request(http.MethodGet, endpoint, params, nil)
}

// Post executes a POST request against the test service.
func (mst *MalwareScannerTester) Post(endpoint string, queryParams url.Values, bodyParams map[string]string) (r *http.Response, body []byte, err error) {
	if queryParams == nil {
		queryParams = url.Values{}
	}
	bodyBytes, err := json.Marshal(bodyParams)
	if err != nil {
		return
	}
	serviceURL := testServiceAddr + ":" + testServicePort + endpoint + "?" + queryParams.Encode()
	req, err := http.NewRequest(http.MethodPost, serviceURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c := http.Client{}
	r, err = c.Do(req)
	if err != nil {
		return
	}
	return processResponse(r)
}

// Put executes a PUT request against the test service.
func (mst *MalwareScannerTester) Put(endpoint string, params url.Values, putParams map[string]string) (r *http.Response, body []byte, err error) {
	return mst.request(http.MethodPut, endpoint, params, putParams)
}

// Close performs a graceful shutdown of the MalwareScannerTester service.
func (mst *MalwareScannerTester) Close() error {
	mst.cancel()
	return nil
}

// request is a helper method that puts together and executes an HTTP
// request. It attaches the current cookie, if one exists.
func (mst *MalwareScannerTester) request(method string, endpoint string, queryParams url.Values, bodyParams map[string]string) (*http.Response, []byte, error) {
	if queryParams == nil {
		queryParams = url.Values{}
	}
	serviceURL := testServiceAddr + ":" + testServicePort + endpoint + "?" + queryParams.Encode()
	b, err := json.Marshal(bodyParams)
	if err != nil {
		return nil, nil, errors.AddContext(err, "failed to marshal the body JSON")
	}
	req, err := http.NewRequest(method, serviceURL, bytes.NewBuffer(b))
	if err != nil {
		return nil, nil, err
	}
	client := http.Client{}
	r, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	return processResponse(r)
}

// processResponse is a helper method which extracts the body from the response
// and handles non-OK status codes.
func processResponse(r *http.Response) (*http.Response, []byte, error) {
	body, err := ioutil.ReadAll(r.Body)
	_ = r.Body.Close()
	// For convenience, whenever we have a non-OK status we'll wrap it in an
	// error.
	if r.StatusCode < 200 || r.StatusCode > 299 {
		err = errors.Compose(err, errors.New(r.Status))
	}
	return r, body, err
}