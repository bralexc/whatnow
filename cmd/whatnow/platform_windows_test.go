//go:build windows && !e2e

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// This package's normal build uses the real provider; the e2e build uses a mock.
// A unique service isolates this test from every real whatnow credential.
func TestWindowsCredentialManager(t *testing.T) {
	service := fmt.Sprintf("whatnow-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	for _, account := range []string{"anthropic", "integration/gcal/test/url"} {
		t.Run(account, func(t *testing.T) {
			t.Cleanup(func() { keyring.Delete(service, account) })
			for _, value := range []string{"test-secret-é-日", "updated-test-secret"} {
				if err := keyring.Set(service, account, value); err != nil {
					t.Fatalf("Credential Manager write: %v", err)
				}
				got, err := keyring.Get(service, account)
				if err != nil || got != value {
					t.Fatalf("Credential Manager round trip failed: %v", err)
				}
			}
			if err := keyring.Delete(service, account); err != nil {
				t.Fatalf("Credential Manager delete: %v", err)
			}
			if _, err := keyring.Get(service, account); !errors.Is(err, keyring.ErrNotFound) {
				t.Fatalf("deleted credential: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestWindowsStandaloneTimeZones(t *testing.T) {
	// Simulate a user with neither Go nor a downloaded timezone database installed.
	t.Setenv("GOROOT", filepath.Join(t.TempDir(), "no-go"))
	t.Setenv("ZONEINFO", filepath.Join(t.TempDir(), "no-zoneinfo"))
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for month, want := range map[time.Month]int{time.January: -5 * 3600, time.July: -4 * 3600} {
		_, offset := time.Date(2026, month, 15, 12, 0, 0, 0, loc).Zone()
		if offset != want {
			t.Errorf("%s offset = %d, want %d", month, offset, want)
		}
	}
}
