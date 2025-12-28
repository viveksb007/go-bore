package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/viveksb007/go-bore/internal/logging"
)

func TestRootCommand(t *testing.T) {
	// Reset flags for testing
	verbose = false

	// Test help output
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gobore") {
		t.Error("Expected help output to contain 'gobore'")
	}
	if !strings.Contains(output, "server") {
		t.Error("Expected help output to contain 'server' subcommand")
	}
	if !strings.Contains(output, "client") {
		t.Error("Expected help output to contain 'client' subcommand")
	}
}

func TestRootCommandVersion(t *testing.T) {
	// Verify version is set correctly
	if rootCmd.Version != version {
		t.Errorf("Expected version '%s', got '%s'", version, rootCmd.Version)
	}
}

func TestVerboseFlag(t *testing.T) {
	// Reset flags
	verbose = false

	rootCmd.SetArgs([]string{"--verbose", "--help"})
	rootCmd.Execute()

	if !verbose {
		t.Error("Expected verbose flag to be true")
	}

	// Reset for other tests
	verbose = false
}

func TestServerCommandHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"server", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "--port") {
		t.Error("Expected server help to contain '--port' flag")
	}
	if !strings.Contains(output, "--secret") {
		t.Error("Expected server help to contain '--secret' flag")
	}
	if !strings.Contains(output, "7835") {
		t.Error("Expected server help to show default port 7835")
	}
}

func TestServerCommandDefaultPort(t *testing.T) {
	// Reset flags
	serverPort = 0
	serverSecret = ""

	// We can't actually run the server in tests, but we can verify flag parsing
	// by checking the flag values after parsing
	serverCmd.Flags().Set("port", "7835")

	if serverPort != 7835 {
		t.Errorf("Expected default port 7835, got %d", serverPort)
	}
}

func TestServerCommandCustomPort(t *testing.T) {
	serverCmd.Flags().Set("port", "9000")

	if serverPort != 9000 {
		t.Errorf("Expected port 9000, got %d", serverPort)
	}

	// Reset
	serverCmd.Flags().Set("port", "7835")
}

func TestServerCommandSecret(t *testing.T) {
	serverCmd.Flags().Set("secret", "testtoken")

	if serverSecret != "testtoken" {
		t.Errorf("Expected secret 'testtoken', got '%s'", serverSecret)
	}

	// Reset
	serverCmd.Flags().Set("secret", "")
}

func TestClientCommandHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"client", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "--to") {
		t.Error("Expected client help to contain '--to' flag")
	}
	if !strings.Contains(output, "--secret") {
		t.Error("Expected client help to contain '--secret' flag")
	}
	if !strings.Contains(output, "--local-host") {
		t.Error("Expected client help to contain '--local-host' flag")
	}
	if !strings.Contains(output, "localhost") {
		t.Error("Expected client help to show default local-host 'localhost'")
	}
}

func TestClientCommandRequiresToFlag(t *testing.T) {
	// Verify --to flag is marked as required
	toFlag := clientCmd.Flags().Lookup("to")
	if toFlag == nil {
		t.Fatal("Expected --to flag to exist")
	}

	// Check that the flag is marked as required via annotations
	annotations := clientCmd.Flags().Lookup("to").Annotations
	if annotations == nil {
		t.Error("Expected --to flag to have annotations (required)")
	}
}

func TestClientCommandRequiresPortArg(t *testing.T) {
	// Verify the command expects exactly 1 argument
	if clientCmd.Args == nil {
		t.Error("Expected Args validator to be set")
	}

	// Test with no args - should fail
	err := clientCmd.Args(clientCmd, []string{})
	if err == nil {
		t.Error("Expected error when no port argument provided")
	}

	// Test with one arg - should pass
	err = clientCmd.Args(clientCmd, []string{"8080"})
	if err != nil {
		t.Errorf("Expected no error with one argument, got %v", err)
	}

	// Test with two args - should fail
	err = clientCmd.Args(clientCmd, []string{"8080", "extra"})
	if err == nil {
		t.Error("Expected error when extra arguments provided")
	}
}

func TestClientCommandFlags(t *testing.T) {
	// Reset flags
	serverAddr = ""
	clientSecret = ""
	localHost = "localhost"

	clientCmd.Flags().Set("to", "example.com:7835")
	clientCmd.Flags().Set("secret", "mytoken")
	clientCmd.Flags().Set("local-host", "192.168.1.100")

	if serverAddr != "example.com:7835" {
		t.Errorf("Expected serverAddr 'example.com:7835', got '%s'", serverAddr)
	}
	if clientSecret != "mytoken" {
		t.Errorf("Expected clientSecret 'mytoken', got '%s'", clientSecret)
	}
	if localHost != "192.168.1.100" {
		t.Errorf("Expected localHost '192.168.1.100', got '%s'", localHost)
	}

	// Reset
	clientCmd.Flags().Set("to", "")
	clientCmd.Flags().Set("secret", "")
	clientCmd.Flags().Set("local-host", "localhost")
}

func TestVerboseFlagEnablesDebugLogging(t *testing.T) {
	// Reset flags
	verbose = false

	// Set verbose flag and trigger PersistentPreRun
	verbose = true
	rootCmd.PersistentPreRun(rootCmd, []string{})

	// Verify logging level is DEBUG
	if logging.GetLevel() != slog.LevelDebug {
		t.Errorf("Expected DEBUG level when verbose=true, got %v", logging.GetLevel())
	}

	// Reset
	verbose = false
	rootCmd.PersistentPreRun(rootCmd, []string{})
}

func TestNonVerboseFlagUsesInfoLogging(t *testing.T) {
	// Reset flags
	verbose = false

	// Trigger PersistentPreRun without verbose
	rootCmd.PersistentPreRun(rootCmd, []string{})

	// Verify logging level is INFO
	if logging.GetLevel() != slog.LevelInfo {
		t.Errorf("Expected INFO level when verbose=false, got %v", logging.GetLevel())
	}
}
