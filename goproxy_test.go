package goproxy

import "testing"

func TestPrefixToIfNotIn(t *testing.T) {
	got := prefixToIfNotIn("foo: bar", "foo")
	if want := "foo: bar"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	got = prefixToIfNotIn("bar", "foo")
	if want := "foo: bar"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	got = prefixToIfNotIn("foobar", "foo")
	if want := "foobar"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestStringSliceContains(t *testing.T) {
	if !stringSliceContains([]string{"foo", "bar"}, "foo") {
		t.Error("want true")
	}

	if stringSliceContains([]string{"foo", "bar"}, "foobar") {
		t.Error("want false")
	}
}

func TestGlobsMatchPath(t *testing.T) {
	if !globsMatchPath("foobar", "foobar") {
		t.Error("want true")
	}

	if !globsMatchPath("foo", "foo/bar") {
		t.Error("want true")
	}

	if globsMatchPath("foo", "bar/foo") {
		t.Error("want false")
	}

	if globsMatchPath("foo", "foobar") {
		t.Error("want false")
	}

	if !globsMatchPath("foo/bar", "foo/bar") {
		t.Error("want true")
	}

	if globsMatchPath("foo/bar", "foobar") {
		t.Error("want false")
	}

	if !globsMatchPath("foo,bar", "foo") {
		t.Error("want true")
	}

	if !globsMatchPath("foo,", "foo") {
		t.Error("want true")
	}

	if !globsMatchPath(",bar", "bar") {
		t.Error("want true")
	}

	if globsMatchPath("", "foobar") {
		t.Error("want false")
	}
}
