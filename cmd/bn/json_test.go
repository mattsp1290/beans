package main

import (
	"os"
	"testing"
)

func TestWriteJSONGoesToStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })
	if err := writeJSON([]string{"x"}); err != nil {
		t.Fatal(err)
	}
	w.Close()
	buf := make([]byte, 64)
	n, _ := r.Read(buf)
	if string(buf[:n]) != "[\n  \"x\"\n]\n" {
		t.Errorf("unexpected stdout: %q", buf[:n])
	}
}
