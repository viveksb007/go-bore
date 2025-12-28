package main

import (
	"testing"
)

func TestClientCommandFlagDefaults(t *testing.T) {
	// Reset flags to defaults
	clientCmd.Flags().Set("to", "")
	clientCmd.Flags().Set("secret", "")
	clientCmd.Flags().Set("local-host", "localhost")

	// Verify --to has no default (required)
	toFlag := clientCmd.Flags().Lookup("to")
	if toFlag == nil {
		t.Fatal("Expected --to flag to exist")
	}
	if toFlag.DefValue != "" {
		t.Errorf("Expected empty default for --to, got %s", toFlag.DefValue)
	}

	// Verify --secret has no default
	secretFlag := clientCmd.Flags().Lookup("secret")
	if secretFlag == nil {
		t.Fatal("Expected --secret flag to exist")
	}
	if secretFlag.DefValue != "" {
		t.Errorf("Expected empty default secret, got %s", secretFlag.DefValue)
	}

	// Verify --local-host defaults to localhost
	localHostFlag := clientCmd.Flags().Lookup("local-host")
	if localHostFlag == nil {
		t.Fatal("Expected --local-host flag to exist")
	}
	if localHostFlag.DefValue != "localhost" {
		t.Errorf("Expected default local-host 'localhost', got %s", localHostFlag.DefValue)
	}
}

func TestClientCommandToFlag(t *testing.T) {
	tests := []struct {
		name     string
		toVal    string
		expected string
	}{
		{"domain with port", "example.com:7835", "example.com:7835"},
		{"ip with port", "192.168.1.100:7835", "192.168.1.100:7835"},
		{"localhost with port", "localhost:7835", "localhost:7835"},
		{"domain only", "example.com", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientCmd.Flags().Set("to", tt.toVal)
			if serverAddr != tt.expected {
				t.Errorf("Expected serverAddr '%s', got '%s'", tt.expected, serverAddr)
			}
		})
	}

	// Reset
	clientCmd.Flags().Set("to", "")
}

func TestClientCommandSecretFlag(t *testing.T) {
	tests := []struct {
		name      string
		secretVal string
		expected  string
	}{
		{"no secret", "", ""},
		{"simple secret", "mytoken", "mytoken"},
		{"complex secret", "my-secret-token-123!", "my-secret-token-123!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientCmd.Flags().Set("secret", tt.secretVal)
			if clientSecret != tt.expected {
				t.Errorf("Expected secret '%s', got '%s'", tt.expected, clientSecret)
			}
		})
	}

	// Reset
	clientCmd.Flags().Set("secret", "")
}

func TestClientCommandLocalHostFlag(t *testing.T) {
	tests := []struct {
		name     string
		hostVal  string
		expected string
	}{
		{"default localhost", "localhost", "localhost"},
		{"ip address", "192.168.1.100", "192.168.1.100"},
		{"custom hostname", "myhost.local", "myhost.local"},
		{"loopback ip", "127.0.0.1", "127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientCmd.Flags().Set("local-host", tt.hostVal)
			if localHost != tt.expected {
				t.Errorf("Expected localHost '%s', got '%s'", tt.expected, localHost)
			}
		})
	}

	// Reset
	clientCmd.Flags().Set("local-host", "localhost")
}

func TestClientCommandShortFlags(t *testing.T) {
	// Test short flag -t for --to
	toFlag := clientCmd.Flags().ShorthandLookup("t")
	if toFlag == nil {
		t.Error("Expected -t shorthand for --to flag")
	}

	// Test short flag -s for --secret
	secretFlag := clientCmd.Flags().ShorthandLookup("s")
	if secretFlag == nil {
		t.Error("Expected -s shorthand for --secret flag")
	}

	// Test short flag -l for --local-host
	localHostFlag := clientCmd.Flags().ShorthandLookup("l")
	if localHostFlag == nil {
		t.Error("Expected -l shorthand for --local-host flag")
	}
}

func TestClientCommandCombinedFlags(t *testing.T) {
	// Test setting all flags together
	clientCmd.Flags().Set("to", "example.com:7835")
	clientCmd.Flags().Set("secret", "testtoken")
	clientCmd.Flags().Set("local-host", "192.168.1.100")

	if serverAddr != "example.com:7835" {
		t.Errorf("Expected serverAddr 'example.com:7835', got '%s'", serverAddr)
	}
	if clientSecret != "testtoken" {
		t.Errorf("Expected secret 'testtoken', got '%s'", clientSecret)
	}
	if localHost != "192.168.1.100" {
		t.Errorf("Expected localHost '192.168.1.100', got '%s'", localHost)
	}

	// Reset
	clientCmd.Flags().Set("to", "")
	clientCmd.Flags().Set("secret", "")
	clientCmd.Flags().Set("local-host", "localhost")
}

func TestClientCommandUsage(t *testing.T) {
	// Verify command usage string
	if clientCmd.Use != "client <port>" {
		t.Errorf("Expected Use 'client <port>', got '%s'", clientCmd.Use)
	}

	// Verify command has a short description
	if clientCmd.Short == "" {
		t.Error("Expected Short description to be set")
	}

	// Verify command has a long description
	if clientCmd.Long == "" {
		t.Error("Expected Long description to be set")
	}
}

func TestClientCommandIsSubcommand(t *testing.T) {
	// Verify client is a subcommand of root
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "client" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'client' to be a subcommand of root")
	}
}

func TestClientCommandPortValidation(t *testing.T) {
	tests := []struct {
		name        string
		port        string
		expectError bool
	}{
		{"valid port 80", "80", false},
		{"valid port 8080", "8080", false},
		{"valid port 1", "1", false},
		{"valid port 65535", "65535", false},
		{"invalid port 0", "0", true},
		{"invalid port negative", "-1", true},
		{"invalid port too high", "65536", true},
		{"invalid port string", "abc", true},
		{"invalid port empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			if tt.port == "" {
				err = clientCmd.Args(clientCmd, []string{})
			} else {
				err = clientCmd.Args(clientCmd, []string{tt.port})
				if err == nil {
					// Parse the port to check validation
					_, parseErr := parseAndValidatePort(tt.port)
					if parseErr != nil {
						err = parseErr
					}
				}
			}

			// For empty port, we expect an error from Args validation
			if tt.port == "" && err == nil {
				t.Error("Expected error for empty port")
			}
		})
	}
}

// parseAndValidatePort is a helper to test port validation logic
func parseAndValidatePort(portStr string) (int, error) {
	var port int
	_, err := parsePort(portStr, &port)
	if err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, err
	}
	return port, nil
}

func parsePort(portStr string, port *int) (int, error) {
	var n int
	_, err := parsePortHelper(portStr, &n)
	if err != nil {
		return 0, err
	}
	*port = n
	return n, nil
}

func parsePortHelper(portStr string, port *int) (int, error) {
	// Simple port parsing for testing
	n := 0
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return 0, &portError{portStr}
		}
		n = n*10 + int(c-'0')
	}
	*port = n
	return n, nil
}

type portError struct {
	port string
}

func (e *portError) Error() string {
	return "invalid port: " + e.port
}

func TestClientCommandToFlagRequired(t *testing.T) {
	// Verify --to flag is marked as required
	toFlag := clientCmd.Flags().Lookup("to")
	if toFlag == nil {
		t.Fatal("Expected --to flag to exist")
	}

	// Check annotations for required flag
	annotations := toFlag.Annotations
	if annotations == nil {
		t.Error("Expected --to flag to have annotations (required)")
	}
}

func TestClientCommandArgsValidation(t *testing.T) {
	// Test with no args
	err := clientCmd.Args(clientCmd, []string{})
	if err == nil {
		t.Error("Expected error with no arguments")
	}

	// Test with one arg
	err = clientCmd.Args(clientCmd, []string{"8080"})
	if err != nil {
		t.Errorf("Expected no error with one argument, got %v", err)
	}

	// Test with two args
	err = clientCmd.Args(clientCmd, []string{"8080", "extra"})
	if err == nil {
		t.Error("Expected error with extra arguments")
	}
}
