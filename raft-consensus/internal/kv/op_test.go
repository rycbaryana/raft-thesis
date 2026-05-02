package kv

import "testing"

func TestEncodeDecodePut(t *testing.T) {
	raw, err := EncodePut("foo", "bar")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	cmd, err := DecodeCommand(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if cmd.Type != OpPut || cmd.Key != "foo" || cmd.Value != "bar" {
		t.Fatalf("unexpected command decoded: %+v", cmd)
	}
}
