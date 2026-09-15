package mcp

import "testing"

// A media URL is handed to Evolution, which fetches it from inside the Docker
// network. Anything that resolves back into that network turns "send this
// image" into a read of an internal service delivered to a phone number.
func TestMediaURLRefusesTheServersOwnNetwork(t *testing.T) {
	refused := []string{
		"http://127.0.0.1:15672/api/overview",
		"http://localhost/",
		"http://rabbitmq:15672/api/queues",
		"http://minio:9000/evolution-media/",
		"http://postgres-mcp:5432/",
		"http://10.0.0.5/secret",
		"http://192.168.1.10/secret",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/",
		"file:///etc/passwd",
		"gopher://evolution-go:4000/",
		"http://user:pass@example.com/photo.jpg",
		"http://evolution-go/",
		"http://intranet/photo.jpg",
	}
	for _, raw := range refused {
		if err := checkMediaURL(raw); err == nil {
			t.Errorf("checkMediaURL(%q) allowed it", raw)
		}
	}

	allowed := []string{
		"https://example.com/photo.jpg",
		"http://example.com/photo.jpg",
		"https://cdn.example.com:8443/a/b/c.pdf?token=1",
		"https://203.0.113.7/photo.jpg",
	}
	for _, raw := range allowed {
		if err := checkMediaURL(raw); err != nil {
			t.Errorf("checkMediaURL(%q) refused it: %v", raw, err)
		}
	}
}
