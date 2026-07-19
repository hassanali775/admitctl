package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI is a small test helper that invokes run() with fresh output
// buffers and returns (exitCode, stdout, stderr) for assertions.
func runCLI(t *testing.T, storePath string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, storePath, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRegisterThenList(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")

	code, out, errOut := runCLI(t, storePath, "register",
		"--id", "acme-corp", "--name", "Acme Corp", "--auth", "api_key", "--rps", "50")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "acme-corp") || !strings.Contains(out, "registered") {
		t.Fatalf("expected confirmation message, got: %q", out)
	}

	// A separate "invocation" reading from the same store path should
	// see the tenant that was just registered.
	code, out, errOut = runCLI(t, storePath, "list")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "acme-corp") {
		t.Fatalf("expected list to show previously registered tenant, got: %q", out)
	}
	if !strings.Contains(out, "active") {
		t.Fatalf("expected newly registered tenant to be listed as active, got: %q", out)
	}
}

func TestRegister_InvalidConfigReportsFieldErrors(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")

	code, _, errOut := runCLI(t, storePath, "register",
		"--id", "Not_Valid", "--name", "X", "--auth", "bogus", "--rps", "0")
	if code != 1 {
		t.Fatalf("expected exit 1 for invalid config, got %d", code)
	}
	if !strings.Contains(errOut, "invalid tenant configuration") {
		t.Fatalf("expected field-level validation errors in stderr, got: %q", errOut)
	}
}

func TestRegister_DuplicateIDFails(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")

	if code, _, errOut := runCLI(t, storePath, "register",
		"--id", "acme", "--name", "Acme", "--auth", "api_key", "--rps", "10"); code != 0 {
		t.Fatalf("expected first registration to succeed, got exit %d (stderr: %s)", code, errOut)
	}

	code, _, errOut := runCLI(t, storePath, "register",
		"--id", "acme", "--name", "Acme Again", "--auth", "api_key", "--rps", "10")
	if code != 1 {
		t.Fatalf("expected exit 1 for duplicate registration, got %d", code)
	}
	if !strings.Contains(errOut, "already exists") {
		t.Fatalf("expected duplicate-tenant error in stderr, got: %q", errOut)
	}
}

func TestGet_NotFoundReportsError(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")

	code, _, errOut := runCLI(t, storePath, "get", "ghost")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "not found") {
		t.Fatalf("expected not-found error, got: %q", errOut)
	}
}

func TestDeactivate_PersistsAcrossInvocations(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")

	runCLI(t, storePath, "register", "--id", "acme", "--name", "Acme", "--auth", "api_key", "--rps", "10")

	code, out, errOut := runCLI(t, storePath, "deactivate", "acme")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "deactivated") {
		t.Fatalf("expected deactivation confirmation, got: %q", out)
	}

	// Fresh "invocation" against the same store must see the
	// deactivation, proving persistence actually round-trips status.
	code, out, _ = runCLI(t, storePath, "get", "acme")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "inactive") {
		t.Fatalf("expected persisted status to be inactive, got: %q", out)
	}
}

func TestUnknownCommandReturnsExitCode2(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")
	code, _, errOut := runCLI(t, storePath, "bogus-command")
	if code != 2 {
		t.Fatalf("expected exit 2 for unknown command, got %d", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("expected usage/error message, got: %q", errOut)
	}
}

func TestNoArgsPrintsUsageAndExits2(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tenants.json")
	code, _, errOut := runCLI(t, storePath)
	if code != 2 {
		t.Fatalf("expected exit 2 for no arguments, got %d", code)
	}
	if !strings.Contains(errOut, "Usage") {
		t.Fatalf("expected usage text in stderr, got: %q", errOut)
	}
}