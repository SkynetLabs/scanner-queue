package database

import (
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/skymodules"
	"gitlab.com/SkynetLabs/skyd/skymodules/renter"
	"gopkg.in/h2non/gock.v1"
)

const testPortal = "http://siasky.test"

// TestSkylink_LoadString ensures that LoadString works as expected.
func TestSkylink_LoadString(t *testing.T) {
	defer gock.Off()

	v1 := "CAD07c3_6RCANw-IgdddeRhxgibS3hZdWxQvKh2gViKPVw"
	v2 := "AQAh2vxStoSJ_M9tWcTgqebUWerCAbpMfn9xxa9E29UOuw"
	v1HashStr := "82a925be13a9d970a4bda34ed67c8e5be179a499e39895b15ff081d62a317ec8"

	var sl Skylink

	// Invalid
	err := sl.LoadString("not a skylink", testPortal)
	if err == nil || !errors.Contains(err, ErrInvalidSkylink) {
		t.Fatalf("Expected error '%s', got '%s'", ErrInvalidSkylink, err)
	}

	// V1 skylink
	err = sl.LoadString(v1, testPortal)
	if err != nil {
		t.Fatal(err)
	}
	if hexHash := hex.EncodeToString(sl.Hash[:]); hexHash != v1HashStr {
		t.Fatalf("Expected hash %s, got %s", v1HashStr, hexHash)
	}
	// Ensure the timestamp was just set (within the last 5ms).
	if sl.Timestamp.After(time.Now().UTC()) || sl.Timestamp.Before(time.Now().Add(-5*time.Millisecond).UTC()) {
		t.Fatalf("Expected a timestamp within 5ms of %s, got %s", time.Now().UTC().String(), sl.Timestamp.String())
	}
	// Store the timestamp, so we can later ensure that it's not changed during
	// subsequent loads.
	ts := sl.Timestamp
	// Ensure the status of the skylink is set to "new".
	if sl.Status != SkylinkStatusNew {
		t.Fatalf("Expected status 'new', got '%s'", sl.Status)
	}
	// Change the status to "scanning", so we can later check that it's not
	// changed during subsequent loads.
	sl.Status = SkylinkStatusScanning

	// V2 skylink. We'll point it to the previously used v1 skylink.
	gock.New(testPortal).
		Head(v2).
		Reply(http.StatusNoContent).
		SetHeader("skynet-skylink", v1)
	err = sl.LoadString(v2, testPortal)
	if err != nil {
		t.Fatal(err)
	}
	if hexHash := hex.EncodeToString(sl.Hash[:]); hexHash != v1HashStr {
		t.Fatalf("Expected hash %s, got %s", v1HashStr, hexHash)
	}
	// Ensure the timestamp has not been changed.
	if sl.Timestamp != ts {
		t.Fatal("Timestamp has been changed.")
	}
	// Ensure the status has not been changed.
	if sl.Status != SkylinkStatusScanning {
		t.Fatalf("Expected status %s, got %s", SkylinkStatusScanning, sl.Status)
	}
}

// TestRecursivelyResolveSkylinkV2 ensures recursivelyResolveSkylinkV2 works as
// expected.
func TestRecursivelyResolveSkylinkV2(t *testing.T) {
	defer gock.Off()

	v1 := "CAD07c3_6RCANw-IgdddeRhxgibS3hZdWxQvKh2gViKPVw"
	v2 := "AQAh2vxStoSJ_M9tWcTgqebUWerCAbpMfn9xxa9E29UOuw"
	anotherV2 := "AQBh2vxStoSJ_M9tWcTgqebUWerCAbpMfn9xxa9E29UOuw"
	var sl skymodules.Skylink

	// Expect and error when we run out of attempts.
	_, err := recursivelyResolveSkylinkV2(sl, testPortal, 0)
	if err == nil || !strings.Contains(err.Error(), "v2 skylinks are nested too deeply") {
		t.Fatalf("Expected error '%s', got '%s'", "v2 skylinks are nested too deeply", err)
	}

	// Expect an error if you pass a V1 skylink.
	err = sl.LoadString(v1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = recursivelyResolveSkylinkV2(sl, testPortal, 3)
	if err == nil || !errors.Contains(err, renter.ErrInvalidSkylinkVersion) {
		t.Fatalf("Expected error '%s', got '%s'", renter.ErrInvalidSkylinkVersion, err)
	}

	// Expect to properly resolve a V2 skylink, provided the portal responds
	// with the right headers.
	gock.New(testPortal).
		Head(v2).
		Reply(http.StatusNoContent).
		SetHeader("skynet-skylink", v1)
	err = sl.LoadString(v2)
	if err != nil {
		t.Fatal(err)
	}
	sl2, err := recursivelyResolveSkylinkV2(sl, testPortal, 3)
	if err != nil {
		t.Fatal(err)
	}
	if sl2.String() != v1 {
		t.Fatalf("Expected to get v1 skylink %s, got %s", v1, sl2.String())
	}

	// Resolve a recursive skylink: v2 -> anotherV2 -> v1
	gock.New(testPortal).
		Head(v2).
		Reply(http.StatusNoContent).
		SetHeader("skynet-skylink", anotherV2)
	gock.New(testPortal).
		Head(anotherV2).
		Reply(http.StatusNoContent).
		SetHeader("skynet-skylink", v1)
	err = sl.LoadString(v2)
	if err != nil {
		t.Fatal(err)
	}
	sl2, err = recursivelyResolveSkylinkV2(sl, testPortal, 3)
	if err != nil {
		t.Fatal(err)
	}
	if sl2.String() != v1 {
		t.Fatalf("Expected to get v1 skylink %s, got %s", v1, sl2.String())
	}

	// Expect to fail to resolve an infinitely recursive skylink.
	gock.New(testPortal).
		Head(v2).
		Reply(http.StatusNoContent).
		SetHeader("skynet-skylink", v2)
	gock.New(testPortal).
		Head(v2).
		Reply(http.StatusNoContent).
		SetHeader("skynet-skylink", v2)
	gock.New(testPortal).
		Head(v2).
		Reply(http.StatusNoContent).
		SetHeader("skynet-skylink", v2)
	err = sl.LoadString(v2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = recursivelyResolveSkylinkV2(sl, testPortal, 3)
	if err == nil || !strings.Contains(err.Error(), "v2 skylinks are nested too deeply") {
		t.Fatalf("Expected error '%s', got '%s'", "v2 skylinks are nested too deeply", err)
	}
}
