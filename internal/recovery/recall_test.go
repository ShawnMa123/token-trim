package recovery

import (
	"bytes"
	"errors"
	"testing"
)

func TestHandleTextPaging(t *testing.T) {
	s := newTestStore(t)
	id := commitOne(t, s, "one\ntwo\nthree\n", "err\n")
	h, err := s.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	n, err := h.Text("stdout", &out, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := "[stdout] lines 2-2 of 3 (next offset 3)\ntwo\n"
	if out.String() != want {
		t.Fatalf("page = %q", out.String())
	}
	if n != int64(len(want)) {
		t.Fatalf("counted %d", n)
	}
	out.Reset()
	if _, err := h.Text("stderr", &out, 1, 200); err != nil || out.String() != "[stderr] lines 1-1 of 1\nerr\n" {
		t.Fatalf("stderr page = %q err=%v", out.String(), err)
	}
}

func TestHandleRawExactBytes(t *testing.T) {
	s := newTestStore(t)
	raw := []byte{0xff, 0x00, 0x41}
	id := commitOne(t, s, string(raw), "")
	h, err := s.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	n, err := h.Raw("stdout", &out)
	if err != nil || !bytes.Equal(out.Bytes(), raw) || n != int64(len(raw)) {
		t.Fatalf("raw = %v n=%d err=%v", out.Bytes(), n, err)
	}
	if _, err := h.Text("stdout", &out, 1, 1); !errors.Is(err, ErrNotText) {
		t.Fatalf("non-UTF-8 text err = %v", err)
	}
}
