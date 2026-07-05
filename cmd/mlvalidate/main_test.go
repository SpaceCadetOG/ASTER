package main

import "testing"

func TestFailIsCallable(t *testing.T) {
	if testing.Short() {
		t.Skip("smoke")
	}
}
