package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashIsSaltedAndVerifiable(t *testing.T) {
	a, err := HashPassword("uma senha longa")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("uma senha longa")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || strings.Contains(a, "uma senha longa") {
		t.Fatal("password hash must be salted and must not contain plaintext")
	}
	if !CheckPassword(a, "uma senha longa") || CheckPassword(a, "errada") {
		t.Fatal("password verification mismatch")
	}
}
