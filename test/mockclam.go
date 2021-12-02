package test

import "io"

type (
	// MockClam is a mock of ClamAV implementing the clamav.Scanner interface.
	// The mock has a number of exposed fields which determine the result of the
	// method calls. Use those to adjust the behaviour of the mock to the test
	// situation you wish to model.
	MockClam struct {
		PingValue            error
		PreferredPortalValue string
		ScanValue            map[io.Reader]*ScanValue
		ScanSkylinkValue     map[string]*ScanSkylinkValue
	}

	// ScanValue is a helper type for setting the response of MockClam.Scan()
	ScanValue struct {
		Infected    bool
		Description string
		Err         error
	}

	// ScanSkylinkValue is a helper type for setting the response of the
	// ScanSkylink() method.
	ScanSkylinkValue struct {
		Infected    bool
		Description string
		Size        uint64
		ScannedSize uint64
		Err         error
	}
)

// NewMockClam creates a new mock.
func NewMockClam(portal string) *MockClam {
	return &MockClam{
		PingValue:            nil,
		PreferredPortalValue: portal,
		ScanValue:            map[io.Reader]*ScanValue{},
		ScanSkylinkValue:     map[string]*ScanSkylinkValue{},
	}
}

// Ping returns PingValue.
func (mc MockClam) Ping() error {
	return mc.PingValue
}

// PreferredPortal returns PreferredPortalValue.
func (mc MockClam) PreferredPortal() string {
	return mc.PreferredPortalValue
}

// Scan returns the ScanValue defined for this reader or a default.
func (mc MockClam) Scan(r io.Reader, _ chan bool) (infected bool, description string, err error) {
	// Check if we have a pre-set value we want to return for this reader.
	if v := mc.ScanValue[r]; v != nil {
		return v.Infected, v.Description, v.Err
	}
	// No pre-set, just return a default response.
	return false, "", nil
}

// ScanSkylink returns the ScanSkylinkValue defined for this skylink or a default.
func (mc MockClam) ScanSkylink(skylink string, _ chan bool) (infected bool, description string, size, scannedSize uint64, err error) {
	// Check if we have a pre-set value we want to return for this skylink.
	if v := mc.ScanSkylinkValue[skylink]; v != nil {
		return v.Infected, v.Description, v.Size, v.ScannedSize, v.Err
	}
	// No pre-set, just return a default response.
	return false, "", 1 << 20, 1 << 20, nil
}