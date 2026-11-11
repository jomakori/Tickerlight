package judge

import "testing"

// Table-driven: the bind-address gate is the only thing standing between the
// judge listener and an accidental public exposure, so every shape of
// loopback/non-loopback input gets a case.
func TestValidateBindAddr(t *testing.T) {
	cases := []struct {
		name             string
		addr             string
		allowNonLoopback bool
		wantErr          bool
	}{
		{"loopback ipv4 with port", "127.0.0.1:8081", false, false},
		{"loopback hostname with port", "localhost:8081", false, false},
		{"loopback ipv6 with port", "[::1]:8081", false, false},
		{"all interfaces rejected", "0.0.0.0:8081", false, true},
		{"bare port binds all interfaces, rejected", ":8081", false, true},
		{"public ip rejected", "10.0.0.5:8081", false, true},
		{"hostname rejected", "example.com:8081", false, true},
		{"non-loopback allowed when explicit", "0.0.0.0:8081", true, false},
		{"public ip allowed when explicit", "10.0.0.5:8081", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateBindAddr(c.addr, c.allowNonLoopback)
			if c.wantErr && err == nil {
				t.Errorf("ValidateBindAddr(%q, %v) = nil, want error", c.addr, c.allowNonLoopback)
			}
			if !c.wantErr && err != nil {
				t.Errorf("ValidateBindAddr(%q, %v) = %v, want nil", c.addr, c.allowNonLoopback, err)
			}
		})
	}
}
