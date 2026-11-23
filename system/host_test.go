package main

import "testing"

// orOS STORE-1: status() names the free and total bytes of the home disk.
func TestDiskSpace(t *testing.T) {
	free, total, ok := diskSpace(t.TempDir())
	if !ok || total <= 0 || free < 0 || free > total {
		t.Fatalf("diskSpace = %d, %d, %v", free, total, ok)
	}
	if _, _, ok := diskSpace("/no/such/dir/here"); ok {
		t.Fatal("a missing folder has no disk space")
	}
}
