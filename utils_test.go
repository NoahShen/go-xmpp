package xmpp

import (
	"fmt"
	"testing"
)

func _TestResolveXMPPDomain(t *testing.T) {
	host, port, err := ResolveXMPPDomain("jabbercn.org")
	fmt.Printf("host: %s, port: %d, error: %v\n", host, port, err)
}
