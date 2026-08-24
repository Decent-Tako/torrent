package storage

import (
	"errors"
	"fmt"
	"testing"
)

type expectedPeerRequestError struct{}

func (expectedPeerRequestError) Error() string { return "cold piece not ready" }

func (expectedPeerRequestError) ExpectedPeerRequestFailure() {}

// A storage backend marks expected peer-request failures on the error value.
// The torrent client classifies with errors.As and does not import the store.
func TestExpectedPeerRequestFailureIsOptionalErrorCapability(t *testing.T) {
	var marker ExpectedPeerRequestFailure
	if !errors.As(expectedPeerRequestError{}, &marker) {
		t.Fatal("marked error was not an ExpectedPeerRequestFailure")
	}
	if !errors.As(fmt.Errorf("refuse: %w", expectedPeerRequestError{}), &marker) {
		t.Fatal("wrapped marked error was not an ExpectedPeerRequestFailure")
	}
	if errors.As(errors.New("disk full"), &marker) {
		t.Fatal("unmarked storage error classified as expected")
	}
}
