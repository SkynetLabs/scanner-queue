package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/SkynetLabs/malware-scanner/api"
	"github.com/SkynetLabs/malware-scanner/clamav"
	"github.com/SkynetLabs/malware-scanner/database"
	"github.com/SkynetLabs/malware-scanner/scanner"
	accdb "github.com/SkynetLabs/skynet-accounts/database"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
)

// loadDBCredentials creates a new db connection based on credentials found in
// the environment variables.
func loadDBCredentials() (accdb.DBCredentials, error) {
	var cds accdb.DBCredentials
	var ok bool
	if cds.User, ok = os.LookupEnv("SKYNET_DB_USER"); !ok {
		return accdb.DBCredentials{}, errors.New("missing env var SKYNET_DB_USER")
	}
	if cds.Password, ok = os.LookupEnv("SKYNET_DB_PASS"); !ok {
		return accdb.DBCredentials{}, errors.New("missing env var SKYNET_DB_PASS")
	}
	if cds.Host, ok = os.LookupEnv("SKYNET_DB_HOST"); !ok {
		return accdb.DBCredentials{}, errors.New("missing env var SKYNET_DB_HOST")
	}
	if cds.Port, ok = os.LookupEnv("SKYNET_DB_PORT"); !ok {
		return accdb.DBCredentials{}, errors.New("missing env var SKYNET_DB_PORT")
	}
	return cds, nil
}

func main() {
	// Load the environment variables from the .env file.
	// Existing variables take precedence and won't be overwritten.
	_ = godotenv.Load()

	// Initialise the global context and logger. These will be used throughout
	// the service. Once the context is closed, all background threads will
	// wind themselves down.
	ctx := context.Background()
	logger := logrus.New()
	logLevel, err := logrus.ParseLevel(os.Getenv("MALWARE_SCANNER_LOG_LEVEL"))
	if err != nil {
		logLevel = logrus.InfoLevel
	}
	logger.SetLevel(logLevel)

	// portalAddr tells us which Skynet portal to use for downloading skylinks.
	portal := os.Getenv("PORTAL_DOMAIN")
	if portal == "" {
		log.Fatal("missing env var PORTAL_DOMAIN")
	}
	if !strings.HasPrefix(portal, "http") {
		portal = "https://" + portal
	}

	// Initialised the database connection.
	dbCreds, err := loadDBCredentials()
	if err != nil {
		log.Fatal(errors.AddContext(err, "failed to fetch db credentials"))
	}
	db, err := database.New(ctx, dbCreds, logger)
	if err != nil {
		log.Fatal(errors.AddContext(err, "failed to connect to the db"))
	}

	// Connect to ClamAV.
	clamIP := os.Getenv("CLAMAV_IP")
	if clamIP == "" {
		log.Fatal(errors.New("missing CLAMAV_IP environment variable - cannot connect to ClamAV"))
	}
	clamPort := os.Getenv("CLAMAV_PORT")
	if clamPort == "" {
		log.Fatal(errors.New("missing CLAMAV_PORT environment variable - cannot connect to ClamAV"))
	}
	clam, err := clamav.New(clamIP, clamPort, portal)
	if err != nil {
		log.Fatal(errors.AddContext(err, fmt.Sprintf("cannot connect to ClamAV on %s:%s", clamIP, clamPort)))
	}

	// Initialise and start the background scanner task.
	scan := scanner.New(ctx, db, clam, logger)
	scan.Start()
	// Start the background thread that resets the status of scans that take
	// too long and are considered stuck.
	scan.StartUnlocker()

	// Initialise the server.
	server, err := api.New(db, clam, logger)
	if err != nil {
		log.Fatal(errors.AddContext(err, "failed to build the api"))
	}

	// Get the port this service should listen on.
	port := os.Getenv("MALWARE_SCANNER_PORT")
	if port == "" {
		port = "4000"
	}

	logger.Info("Listening on port " + port)
	log.Fatal(http.ListenAndServe(":"+port, server.Router()))
}
