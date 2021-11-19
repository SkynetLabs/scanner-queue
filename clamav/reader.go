package clamav

import "io"

// ReaderCounter is a wrapper of io.Reader that counts how many bytes are read
// from it.
type ReaderCounter struct {
	readBytes uint64
	r         io.Reader
}

// NewReaderCounter creates a new ReaderCounter instance.
func NewReaderCounter(r io.Reader) *ReaderCounter {
	return &ReaderCounter{
		readBytes: 0,
		r:         r,
	}
}

// Read reads up to len(p) bytes into p. It returns the number of bytes
// read (0 <= n <= len(p)) and any error encountered. Even if Read
// returns n < len(p), it may use all of p as scratch space during the call.
// If some data is available but not len(p) bytes, Read conventionally
// returns what is available instead of waiting for more.
func (rc *ReaderCounter) Read(p []byte) (n int, err error) {
	n, err = rc.r.Read(p)
	rc.readBytes += uint64(n)
	return
}

// ReadBytes returns the number of bytes read from the reader so far.
func (rc *ReaderCounter) ReadBytes() uint64 {
	return rc.readBytes
}