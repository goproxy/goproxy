package goproxy

import (
	"net/url"
	"testing"
)

func TestParseRawURL(t *testing.T) {
	if u, err := parseRawURL("example.com"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("http://example.com"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "http://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("https://example.com"); err != nil {
		t.Fatalf("unexpected error %q", err)
	} else if u == nil {
		t.Fatal("unexpected nil")
	} else if got, want := u.String(), "https://example.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if u, err := parseRawURL("\n"); err == nil {
		t.Fatal("expected error")
	} else if u != nil {
		t.Errorf("got %v, want nil", u)
	}

	if u, err := parseRawURL("scheme://example.com"); err == nil {
		t.Fatal("expected error")
	} else if u != nil {
		t.Errorf("got %v, want nil", u)
	}
}

func TestAppendURL(t *testing.T) {
	us := appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"foobar",
	).String()
	if want := "https://example.com/foobar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"foo",
		"bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"",
		"foo",
		"",
		"bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"foo/bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"/foo/bar",
	).String()
	if want := "https://example.com/foo/bar"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}

	us = appendURL(
		&url.URL{
			Scheme: "https",
			Host:   "example.com",
		},
		"/foo/bar/",
	).String()
	if want := "https://example.com/foo/bar/"; us != want {
		t.Errorf("got %q, want %q", us, want)
	}
}

func TestRedactedURL(t *testing.T) {
	ru := redactedURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	})
	if want := "https://example.com"; ru != want {
		t.Errorf("got %q, want %q", ru, want)
	}

	ru = redactedURL(&url.URL{
		Scheme: "https",
		User:   url.User("user"),
		Host:   "example.com",
	})
	if want := "https://user@example.com"; ru != want {
		t.Errorf("got %q, want %q", ru, want)
	}

	ru = redactedURL(&url.URL{
		Scheme: "https",
		User:   url.UserPassword("user", "password"),
		Host:   "example.com",
	})
	if want := "https://user:xxxxx@example.com"; ru != want {
		t.Errorf("got %q, want %q", ru, want)
	}
}
