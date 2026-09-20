package httputil

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadDecompressedBoundedDistinguishesLimitBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "N-1", size: 7},
		{name: "N", size: 8},
		{name: "N+1", size: 9, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readDecompressedBounded(bytes.NewReader(bytes.Repeat([]byte{'x'}, tc.size)), 8)
			if tc.wantErr {
				var maxErr *http.MaxBytesError
				if !errors.As(err, &maxErr) {
					t.Fatalf("expected MaxBytesError, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.size {
				t.Fatalf("size mismatch: got=%d want=%d", len(got), tc.size)
			}
		})
	}
}

func TestReadDecompressedBoundedPropagatesReaderError(t *testing.T) {
	want := errors.New("synthetic read failure")
	_, err := readDecompressedBounded(io.MultiReader(strings.NewReader("ok"), errorReader{err: want}), 8)
	if !errors.Is(err, want) {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }
