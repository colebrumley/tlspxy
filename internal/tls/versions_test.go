package tls

import (
	cryptotls "crypto/tls"
	"testing"
)

func TestParseTLSVersion_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{"1.0", cryptotls.VersionTLS10},
		{"1.1", cryptotls.VersionTLS11},
		{"1.2", cryptotls.VersionTLS12},
		{"1.3", cryptotls.VersionTLS13},
	}
	for _, tt := range tests {
		v, err := ParseTLSVersion(tt.input)
		if err != nil {
			t.Errorf("ParseTLSVersion(%q) error = %v", tt.input, err)
		}
		if v != tt.want {
			t.Errorf("ParseTLSVersion(%q) = %d, want %d", tt.input, v, tt.want)
		}
	}
}

func TestParseTLSVersion_Invalid(t *testing.T) {
	for _, input := range []string{"1.4", "2.0", "ssl3", "bogus"} {
		_, err := ParseTLSVersion(input)
		if err == nil {
			t.Errorf("ParseTLSVersion(%q) expected error, got nil", input)
		}
	}
}

func TestParseTLSVersion_Empty(t *testing.T) {
	v, err := ParseTLSVersion("")
	if err != nil {
		t.Errorf("ParseTLSVersion(\"\") error = %v", err)
	}
	if v != 0 {
		t.Errorf("ParseTLSVersion(\"\") = %d, want 0", v)
	}
}

func TestParseCipherSuites_Valid(t *testing.T) {
	// Use the first secure cipher suite as a known-good name.
	suites := cryptotls.CipherSuites()
	if len(suites) == 0 {
		t.Skip("no cipher suites available")
	}
	name := suites[0].Name
	ids, err := ParseCipherSuites(name)
	if err != nil {
		t.Fatalf("ParseCipherSuites(%q) error = %v", name, err)
	}
	if len(ids) != 1 || ids[0] != suites[0].ID {
		t.Errorf("ParseCipherSuites(%q) = %v, want [%d]", name, ids, suites[0].ID)
	}
}

func TestParseCipherSuites_Invalid(t *testing.T) {
	_, err := ParseCipherSuites("BOGUS_CIPHER")
	if err == nil {
		t.Error("ParseCipherSuites(\"BOGUS_CIPHER\") expected error, got nil")
	}
}

func TestParseCipherSuites_Empty(t *testing.T) {
	ids, err := ParseCipherSuites("")
	if err != nil {
		t.Errorf("ParseCipherSuites(\"\") error = %v", err)
	}
	if ids != nil {
		t.Errorf("ParseCipherSuites(\"\") = %v, want nil", ids)
	}
}

func TestParseCipherSuites_MixedValidInvalid(t *testing.T) {
	suites := cryptotls.CipherSuites()
	if len(suites) == 0 {
		t.Skip("no cipher suites available")
	}
	input := suites[0].Name + ",BOGUS_CIPHER"
	_, err := ParseCipherSuites(input)
	if err == nil {
		t.Errorf("ParseCipherSuites(%q) expected error for mixed valid+invalid", input)
	}
}

func TestValidCipherSuiteNames(t *testing.T) {
	names := ValidCipherSuiteNames()
	if len(names) == 0 {
		t.Error("ValidCipherSuiteNames() returned empty list")
	}
}
