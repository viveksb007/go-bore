package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		verbose bool
		want    slog.Level
	}{
		{
			name:    "verbose mode enables debug level",
			verbose: true,
			want:    slog.LevelDebug,
		},
		{
			name:    "non-verbose mode uses info level",
			verbose: false,
			want:    slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			InitWithWriter(&buf, tt.verbose)

			got := GetLevel()
			if got != tt.want {
				t.Errorf("GetLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInitWithWriter(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter(&buf, false)

	Info("test message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected output to contain 'test message', got: %s", output)
	}
	if !strings.Contains(output, "INFO") {
		t.Errorf("expected output to contain 'INFO', got: %s", output)
	}
}

func TestLogLevels(t *testing.T) {
	t.Run("info level logs info and above", func(t *testing.T) {
		var buf bytes.Buffer
		InitWithWriter(&buf, false) // INFO level

		Debug("debug message")
		Info("info message")
		Error("error message")

		output := buf.String()
		if strings.Contains(output, "debug message") {
			t.Error("debug message should not be logged at INFO level")
		}
		if !strings.Contains(output, "info message") {
			t.Error("info message should be logged at INFO level")
		}
		if !strings.Contains(output, "error message") {
			t.Error("error message should be logged at INFO level")
		}
	})

	t.Run("debug level logs all messages", func(t *testing.T) {
		var buf bytes.Buffer
		InitWithWriter(&buf, true) // DEBUG level

		Debug("debug message")
		Info("info message")
		Error("error message")

		output := buf.String()
		if !strings.Contains(output, "debug message") {
			t.Error("debug message should be logged at DEBUG level")
		}
		if !strings.Contains(output, "info message") {
			t.Error("info message should be logged at DEBUG level")
		}
		if !strings.Contains(output, "error message") {
			t.Error("error message should be logged at DEBUG level")
		}
	})
}

func TestSetLevel(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter(&buf, false)

	// Initially at INFO level
	if GetLevel() != slog.LevelInfo {
		t.Errorf("expected INFO level, got %v", GetLevel())
	}

	// Set to DEBUG
	SetLevel(slog.LevelDebug)
	if GetLevel() != slog.LevelDebug {
		t.Errorf("expected DEBUG level after SetLevel, got %v", GetLevel())
	}

	// Set to ERROR
	SetLevel(slog.LevelError)
	if GetLevel() != slog.LevelError {
		t.Errorf("expected ERROR level after SetLevel, got %v", GetLevel())
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{
			name:   "empty secret",
			secret: "",
			want:   "[none]",
		},
		{
			name:   "short secret (1 char)",
			secret: "a",
			want:   "[***]",
		},
		{
			name:   "short secret (4 chars)",
			secret: "abcd",
			want:   "[***]",
		},
		{
			name:   "normal secret (5 chars)",
			secret: "abcde",
			want:   "ab***",
		},
		{
			name:   "long secret",
			secret: "mysupersecrettoken123",
			want:   "my***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskSecret(tt.secret)
			if got != tt.want {
				t.Errorf("MaskSecret(%q) = %q, want %q", tt.secret, got, tt.want)
			}
		})
	}
}

func TestSecretAttr(t *testing.T) {
	attr := SecretAttr("password", "mysecretpassword")

	if attr.Key != "password" {
		t.Errorf("expected key 'password', got %q", attr.Key)
	}
	if attr.Value.String() != "my***" {
		t.Errorf("expected masked value 'my***', got %q", attr.Value.String())
	}
}

func TestLoggerWith(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter(&buf, false)

	logger := With("component", "test")
	logger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "component=test") {
		t.Errorf("expected output to contain 'component=test', got: %s", output)
	}
}

func TestWarn(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter(&buf, false)

	Warn("warning message")

	output := buf.String()
	if !strings.Contains(output, "warning message") {
		t.Errorf("expected output to contain 'warning message', got: %s", output)
	}
	if !strings.Contains(output, "WARN") {
		t.Errorf("expected output to contain 'WARN', got: %s", output)
	}
}

func TestSecretsNotLoggedInPlainText(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter(&buf, true) // Enable debug to capture all logs

	secret := "mysupersecrettoken123"

	// Log with masked secret
	Info("authentication attempt", SecretAttr("secret", secret))

	output := buf.String()

	// Verify the full secret is NOT in the output
	if strings.Contains(output, secret) {
		t.Errorf("secret should not be logged in plain text, output: %s", output)
	}

	// Verify the masked version IS in the output
	if !strings.Contains(output, "my***") {
		t.Errorf("masked secret should be in output, got: %s", output)
	}
}

func TestLoggerReturnsInstance(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter(&buf, false)

	logger := Logger()
	if logger == nil {
		t.Error("Logger() should not return nil")
	}

	logger.Info("direct logger test")
	output := buf.String()
	if !strings.Contains(output, "direct logger test") {
		t.Errorf("expected output from direct logger, got: %s", output)
	}
}
