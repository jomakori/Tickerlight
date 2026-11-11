package judge

import (
	"fmt"
	"net"
)

// ValidateBindAddr rejects a non-loopback bind address unless
// allowNonLoopback is true. The judge scorer is meant to run next to the
// engine that calls it, never on a public interface: a loopback-only default
// means a misconfigured deploy fails closed instead of quietly exposing
// /score to the network.
func ValidateBindAddr(addr string, allowNonLoopback bool) error {
	if allowNonLoopback {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// A bare port (":8080") or malformed address: treat the whole
		// string as the host candidate so obviously-wrong input ("", a
		// hostname with no port) still gets caught here instead of
		// surfacing as a confusing listener error later.
		host = addr
	}
	if host == "" {
		return fmt.Errorf("judge addr %q binds all interfaces; set -judge-allow-non-loopback to permit this", addr)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("judge addr %q is not a loopback address; set -judge-allow-non-loopback to permit this", addr)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("judge addr %q is not a loopback address; set -judge-allow-non-loopback to permit this", addr)
	}
	return nil
}
