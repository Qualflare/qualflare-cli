package auth

import (
	"strings"
	"testing"
)

func TestValidate_Accept(t *testing.T) {
	cases := []string{
		"myapp",
		"my-app",
		"my_app",
		"app1",
		"a",
		"0app",
		strings.Repeat("a", 63),
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			if err := Validate(id); err != nil {
				t.Fatalf("Validate(%q) returned error: %v", id, err)
			}
		})
	}
}

func TestValidate_Reject(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"upper":     "MyApp",
		"dot":       "my.app",
		"space":     "my app",
		"leadingHy": "-app",
		"leadingUS": "_app",
		"tooLong":   strings.Repeat("a", 64),
		"slash":     "my/app",
		"colon":     "my:app",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(id); err == nil {
				t.Fatalf("Validate(%q) accepted invalid id", id)
			}
		})
	}
}

func TestValidate_Reserved(t *testing.T) {
	for name := range ReservedNames {
		t.Run(name, func(t *testing.T) {
			err := Validate(name)
			if err == nil {
				t.Fatalf("Validate(%q) accepted reserved name", name)
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("Validate(%q) error = %v, want contains 'reserved'", name, err)
			}
		})
	}
}
