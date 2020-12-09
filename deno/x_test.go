// Copyright 2020 William Perron. All rights reserved. MIT License.
package deno

import "testing"

func TestStripEntries(t *testing.T) {
	input := []directoryListing{
		{
			Path: "foo",
			Size: 1000,
			Type: "dir",
		},
		{
			Path: "foo/bar.js",
			Size: 200,
			Type: "file",
		},
		{
			Path: "foo/baz.ts",
			Size: 200,
			Type: "file",
		},
		{
			Path: "foo/no_bueno.yml",
			Size: 100,
			Type: "file",
		},
		{
			Path: "foo/bar.jsx",
			Size: 200,
			Type: "file",
		},
		{
			Path: "foo/baz.tsx",
			Size: 100,
			Type: "file",
		},
		{
			Path: "README.md",
			Size: 100,
			Type: "file",
		},
	}
	expected := []directoryListing{
		{
			Path: "foo/bar.js",
			Size: 200,
			Type: "file",
		},
		{
			Path: "foo/baz.ts",
			Size: 200,
			Type: "file",
		},
		{
			Path: "foo/bar.jsx",
			Size: 200,
			Type: "file",
		},
		{
			Path: "foo/baz.tsx",
			Size: 100,
			Type: "file",
		},
		{
			Path: "README.md",
			Size: 100,
			Type: "file",
		},
	}
	actual := stripUselessEntries(input)
	if len(actual) != len(expected) {
		t.Errorf("actual and expected are not the same size, expected %d", len(expected))
		t.FailNow()
	}

	l := len(actual)
	for i := 0; i < l; i++ {
		if actual[i].Path != expected[i].Path {
			t.Errorf("expected element #%d to be %s, got %s", i, expected[i].Path, actual[i].Path)
		}
	}
}

func TestStripEntriesSubdir(t *testing.T) {
	input := []directoryListing{
		{
			Path: "foo",
			Size: 0,
			Type: "dir",
		},
		{
			Path: "foo/bar",
			Size: 0,
			Type: "dir",
		},
		{
			Path: "foo/bar/baz",
			Size: 0,
			Type: "dir",
		},
		{
			Path: "foo/bar/baz/foo.js",
			Size: 0,
			Type: "file",
		},
	}
	expected := []directoryListing{
		{
			Path: "foo/bar/baz/foo.js",
			Size: 0,
			Type: "file",
		},
	}
	actual := stripUselessEntries(input)
	if len(actual) != len(expected) {
		t.Errorf("actual and expected are not the same size, expected %d", len(expected))
		t.FailNow()
	}

	l := len(actual)
	for i := 0; i < l; i++ {
		if actual[i].Path != expected[i].Path {
			t.Errorf("expected element #%d to be %s, got %s", i, expected[i].Path, actual[i].Path)
		}
	}
}

func TestStripEntriesToEmpty(t *testing.T) {
	input := []directoryListing{
		{
			Path: "foo",
			Size: 0,
			Type: "dir",
		},
		{
			Path: "foo/bar",
			Size: 0,
			Type: "dir",
		},
		{
			Path: "foo/bar/baz",
			Size: 0,
			Type: "dir",
		},
	}
	actual := stripUselessEntries(input)
	if len(actual) != 0 {
		t.Errorf("expected output to be empty, got list of length %d", len(actual))
	}
}
