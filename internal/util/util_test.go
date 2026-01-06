package util

import (
	"fmt"
	"testing"
)

func TestConvertBytes(t *testing.T) {
	tests := []struct {
		name      string
		inBytes   int64
		outAmount float64
		outUnit   string
	}{
		{
			name:      "unchanged below KiB",
			inBytes:   1000,
			outAmount: 1000.0,
			outUnit:   BytesUnitBytes,
		},
		{
			name:      "above KiB correct",
			inBytes:   1024,
			outAmount: 1.0,
			outUnit:   BytesUnitKiB,
		},
		{
			name:      "above MiB correct",
			inBytes:   1024 * 1024,
			outAmount: 1.0,
			outUnit:   BytesUnitMiB,
		},
		{
			name:      "above GiB correct",
			inBytes:   1024 * 1024 * 1024,
			outAmount: 1.0,
			outUnit:   BytesUnitGiB,
		},
		{
			name:      "above TiB correct",
			inBytes:   1024 * 1024 * 1024 * 1024,
			outAmount: 1.0,
			outUnit:   BytesUnitTiB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, unit := ConvertBytes(tt.inBytes)

			if out != tt.outAmount {
				t.Errorf("converted amount is '%f' but '%f' was expected", out, tt.outAmount)
			}

			if unit != tt.outUnit {
				t.Errorf("converted unit is '%s' but '%s' was expected", unit, tt.outUnit)
			}
		})
	}
}

func TestConvertBytesToUnit(t *testing.T) {
	tests := []struct {
		name      string
		inBytes   int64
		inUnit    string
		outAmount float64
	}{
		{
			name:      "unchanged with KiB",
			inBytes:   1000,
			inUnit:    BytesUnitBytes,
			outAmount: 1000.0,
		},
		{
			name:      "with KiB correct",
			inBytes:   512,
			inUnit:    BytesUnitKiB,
			outAmount: 0.5,
		},
		{
			name:      "with MiB correct",
			inBytes:   512 * 1024,
			inUnit:    BytesUnitMiB,
			outAmount: 0.5,
		},
		{
			name:      "with GiB correct",
			inBytes:   512 * 1024 * 1024,
			inUnit:    BytesUnitGiB,
			outAmount: 0.5,
		},
		{
			name:      "with TiB correct",
			inBytes:   512 * 1024 * 1024 * 1024,
			inUnit:    BytesUnitTiB,
			outAmount: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ConvertBytesToUnit(tt.inBytes, tt.inUnit)

			if out != tt.outAmount {
				t.Errorf("converted amount is '%f' but '%f' was expected", out, tt.outAmount)
			}
		})
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		expectedUser string
		expectedHost string
	}{
		{
			name:         "host only",
			target:       "hostname",
			expectedUser: "",
			expectedHost: "hostname",
		},
		{
			name:         "user and host",
			target:       "user@hostname",
			expectedUser: "user",
			expectedHost: "hostname",
		},
		{
			name:         "root and host",
			target:       "root@hostname",
			expectedUser: "root",
			expectedHost: "hostname",
		},
		{
			name:         "host with domain",
			target:       "hostname.example.com",
			expectedUser: "",
			expectedHost: "hostname.example.com",
		},
		{
			name:         "user and host with domain",
			target:       "user@hostname.example.com",
			expectedUser: "user",
			expectedHost: "hostname.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, hostname := ParseTarget(tt.target)

			if user != tt.expectedUser {
				t.Errorf("expected user '%s', got '%s'", tt.expectedUser, user)
			}
			if hostname != tt.expectedHost {
				t.Errorf("expected hostname '%s', got '%s'", tt.expectedHost, hostname)
			}
		})
	}
}

func TestBuildStoreAddress(t *testing.T) {
	tests := []struct {
		name           string
		user           string
		hostname       string
		expectedOutput string
	}{
		{
			name:           "user and hostname",
			user:           "user",
			hostname:       "hostname",
			expectedOutput: "ssh-ng://user@hostname",
		},
		{
			name:           "empty user defaults to current user",
			user:           "",
			hostname:       "hostname",
			expectedOutput: fmt.Sprintf("ssh-ng://%s@hostname", GetUser()),
		},
		{
			name:           "root user",
			user:           "root",
			hostname:       "hostname",
			expectedOutput: "ssh-ng://root@hostname",
		},
		{
			name:           "hostname with domain",
			user:           "user",
			hostname:       "hostname.example.com",
			expectedOutput: "ssh-ng://user@hostname.example.com",
		},
		{
			name:           "empty user with domain",
			user:           "",
			hostname:       "hostname.example.com",
			expectedOutput: fmt.Sprintf("ssh-ng://%s@hostname.example.com", GetUser()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildStoreAddress(tt.user, tt.hostname)

			if result != tt.expectedOutput {
				t.Errorf("expected '%s', got '%s'", tt.expectedOutput, result)
			}
		})
	}
}
