package scanner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	blockapi "github.com/SkynetLabs/blocker/api"
	blockdb "github.com/SkynetLabs/blocker/database"
	"gitlab.com/NebulousLabs/errors"
	"gopkg.in/h2non/gock.v1"
)

// TestReportToBlocker ensures reportToBlocker works as expected.
func TestReportToBlocker(t *testing.T) {
	defer gock.Off()

	if BlockerIP == "" {
		BlockerIP = "10.10.10.110"
	}
	if BlockerPort == "" {
		BlockerPort = "4000"
	}

	skylink := "CAD07c3_6RCANw-IgdddeRhxgibS3hZdWxQvKh2gViKPVw"
	blockerURL := fmt.Sprintf("http://%s:%s", BlockerIP, BlockerPort)

	// Happy case.
	blockReqBody := blockapi.BlockPOST{
		Skylink: skylink,
		Reporter: blockdb.Reporter{
			Name: "Malware Scanner",
		},
		Tags: []string{"malware"},
	}
	blockReqBodyBytes, err := json.Marshal(blockReqBody)
	if err != nil {
		t.Fatalf("Failed to serialize request, Error: %s", err.Error())
	}

	gock.New(blockerURL).
		Post("/block").
		Body(bytes.NewBuffer(blockReqBodyBytes)).
		Reply(http.StatusOK)

	err = reportToBlocker(skylink)
	if err != nil {
		t.Fatal(err)
	}

	// Error when calling blocker.
	gock.New(blockerURL).
		Post("/block").
		Body(bytes.NewBuffer(blockReqBodyBytes)).
		ReplyError(errors.New("simulated error"))

	err = reportToBlocker(skylink)
	if err == nil || !strings.Contains(err.Error(), "simulated error") {
		t.Fatalf("Expected error 'simulated error', got '%s'", err)
	}

	// Blocker failed to block
	gock.New(blockerURL).
		Post("/block").
		Body(bytes.NewBuffer(blockReqBodyBytes)).
		Reply(http.StatusInternalServerError)

	err = reportToBlocker(skylink)
	if err == nil || !strings.Contains(err.Error(), "blocker failed. status code 500") {
		t.Fatalf("Expected error 'blocker failed. status code 500', got '%s'", err)
	}
}