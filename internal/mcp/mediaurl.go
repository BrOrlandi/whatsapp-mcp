package mcp

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// checkMediaURL refuses a media URL that points back inside the deployment.
//
// The URL is not fetched here — it is handed to Evolution, which fetches it from
// inside the Docker network, where RabbitMQ's management API, MinIO, both
// PostgreSQL instances and Evolution itself are reachable without credentials in
// a request. Left unchecked, "send this image" is a server-side request forgery
// with the result delivered to a WhatsApp number: an attacker who gets a
// sentence into the model's context — and message content is written by third
// parties — could have an internal endpoint read out to their own phone.
//
// So the rule is the narrow one: http(s) only, a hostname that is not a literal
// private address, and no credentials smuggled in the userinfo. Resolution
// itself still happens inside Evolution, so this cannot catch a public hostname
// that resolves to a private address; the network is expected to be the other
// half of that defence.
func checkMediaURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("url is not a valid URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("url must be http or https, so that file://, data:// and gopher:// cannot be used to read the server itself")
	}
	if parsed.User != nil {
		return fmt.Errorf("url must not carry a username or password")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("url must name a host")
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateAddress(ip) {
		return fmt.Errorf("url points at a private or loopback address, which would make this a request against the server's own network")
	}
	if isInternalName(host) {
		return fmt.Errorf("url points at a name on the server's own network rather than at a public address")
	}
	return nil
}

func isPrivateAddress(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast()
}

// isInternalName rejects the names the Compose stack gives its own services,
// plus the shapes a bare container or host name takes. A public media URL always
// has a dot in it.
func isInternalName(host string) bool {
	lowered := strings.ToLower(strings.TrimSuffix(host, "."))
	switch lowered {
	case "localhost", "evolution-go", "rabbitmq", "minio", "postgres-mcp", "postgres-evolution", "whatsapp-mcp":
		return true
	}
	if strings.HasSuffix(lowered, ".localhost") || strings.HasSuffix(lowered, ".local") || strings.HasSuffix(lowered, ".internal") {
		return true
	}
	// A single label with no dot cannot be a public name; it is a container or
	// a host on the local search domain.
	return !strings.Contains(lowered, ".")
}
