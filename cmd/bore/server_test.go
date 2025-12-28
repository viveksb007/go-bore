package main

import (
	"testing"
)

func TestServerCommandFlagDefaults(t *testing.T) {
	// Reset flags to defaults
	serverCmd.Flags().Set("port", "7835")
	serverCmd.Flags().Set("secret", "")

	// Verify default port
	portFlag := serverCmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatal("Expected --port flag to exist")
	}
	if portFlag.DefValue != "7835" {
		t.Errorf("Expected default port 7835, got %s", portFlag.DefValue)
	}

	// Verify secret has no default
	secretFlag := serverCmd.Flags().Lookup("secret")
	if secretFlag == nil {
		t.Fatal("Expected --secret flag to exist")
	}
	if secretFlag.DefValue != "" {
		t.Errorf("Expected empty default secret, got %s", secretFlag.DefValue)
	}
}

func TestServerCommandPortFlag(t *testing.T) {
	tests := []struct {
		name     string
		portVal  string
		expected int
	}{
		{"default port", "7835", 7835},
		{"custom port", "9000", 9000},
		{"low port", "80", 80},
		{"high port", "65535", 65535},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverCmd.Flags().Set("port", tt.portVal)
			if serverPort != tt.expected {
				t.Errorf("Expected port %d, got %d", tt.expected, serverPort)
			}
		})
	}

	// Reset
	serverCmd.Flags().Set("port", "7835")
}

func TestServerCommandSecretFlag(t *testing.T) {
	tests := []struct {
		name      string
		secretVal string
		expected  string
	}{
		{"no secret", "", ""},
		{"simple secret", "mytoken", "mytoken"},
		{"complex secret", "my-secret-token-123!", "my-secret-token-123!"},
		{"long secret", "this-is-a-very-long-secret-token-for-testing", "this-is-a-very-long-secret-token-for-testing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverCmd.Flags().Set("secret", tt.secretVal)
			if serverSecret != tt.expected {
				t.Errorf("Expected secret '%s', got '%s'", tt.expected, serverSecret)
			}
		})
	}

	// Reset
	serverCmd.Flags().Set("secret", "")
}

func TestServerCommandShortFlags(t *testing.T) {
	// Test short flag -p for port
	portFlag := serverCmd.Flags().ShorthandLookup("p")
	if portFlag == nil {
		t.Error("Expected -p shorthand for --port flag")
	}

	// Test short flag -s for secret
	secretFlag := serverCmd.Flags().ShorthandLookup("s")
	if secretFlag == nil {
		t.Error("Expected -s shorthand for --secret flag")
	}
}

func TestServerCommandCombinedFlags(t *testing.T) {
	// Test setting both flags together
	serverCmd.Flags().Set("port", "8080")
	serverCmd.Flags().Set("secret", "testtoken")

	if serverPort != 8080 {
		t.Errorf("Expected port 8080, got %d", serverPort)
	}
	if serverSecret != "testtoken" {
		t.Errorf("Expected secret 'testtoken', got '%s'", serverSecret)
	}

	// Reset
	serverCmd.Flags().Set("port", "7835")
	serverCmd.Flags().Set("secret", "")
}

func TestServerCommandUsage(t *testing.T) {
	// Verify command usage string
	if serverCmd.Use != "server" {
		t.Errorf("Expected Use 'server', got '%s'", serverCmd.Use)
	}

	// Verify command has a short description
	if serverCmd.Short == "" {
		t.Error("Expected Short description to be set")
	}

	// Verify command has a long description
	if serverCmd.Long == "" {
		t.Error("Expected Long description to be set")
	}
}

func TestServerCommandIsSubcommand(t *testing.T) {
	// Verify server is a subcommand of root
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'server' to be a subcommand of root")
	}
}
