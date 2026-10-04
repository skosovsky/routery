package routeryhttp

import (
	"io"
	stdhttp "net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/routery"
)

type requestAuditReader struct {
	reader io.Reader
	bytes  atomic.Int64
	closes atomic.Int32
}

func (reader *requestAuditReader) Read(data []byte) (int, error) {
	n, err := reader.reader.Read(data)
	reader.bytes.Add(int64(n))
	return n, err
}

func (reader *requestAuditReader) Close() error {
	reader.closes.Add(1)
	return nil
}

func TestPreparedFanoutRequestReaderOwnership(t *testing.T) {
	for _, readyFactory := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepare original", true: "ready GetBody"}[readyFactory],
			func(t *testing.T) { checkFanoutRequestReaders(t, readyFactory) })
	}
}

func checkFanoutRequestReaders(t *testing.T, readyFactory bool) {
	t.Helper()
	// Arrange.
	const payload = "independent complete request bytes"
	const branches = 4
	source := &requestAuditReader{reader: strings.NewReader(payload)}
	request := mustNewRequest(t, stdhttp.MethodPut, nil)
	request.Body, request.ContentLength = source, -1
	if readyFactory {
		request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(payload)), nil }
		t.Cleanup(func() { _ = source.Close() })
	}
	prepared, err := PrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	consumedAtPreparation := source.bytes.Load()
	getBody := prepared.GetBody
	var readersMu sync.Mutex
	var readers []*requestAuditReader
	prepared.GetBody = func() (io.ReadCloser, error) {
		body, getErr := getBody()
		if getErr != nil {
			return nil, getErr
		}
		reader := &requestAuditReader{reader: body}
		readersMu.Lock()
		readers = append(readers, reader)
		readersMu.Unlock()
		// Preserve the underlying factory reader's ownership as well.
		return &requestAuditCloser{requestAuditReader: reader, underlying: body}, nil
	}
	var sent sync.WaitGroup
	sent.Add(branches)
	client := &stdhttp.Client{Transport: roundTripperFunc(func(req *stdhttp.Request) (*stdhttp.Response, error) {
		data, readErr := io.ReadAll(req.Body)
		closeErr := req.Body.Close()
		if readErr != nil || closeErr != nil || string(data) != payload {
			t.Errorf("body=%q read=%v close=%v", data, readErr, closeErr)
		}
		// All request readers are consumed and closed before any winner can return.
		sent.Done()
		sent.Wait()
		return &stdhttp.Response{StatusCode: stdhttp.StatusOK, Body: stdhttp.NoBody}, nil
	})}
	leaf := NewRouteHandler(client)
	// Act.
	result, err := routery.InvokeRouteHandler(t.Context(), prepared, routery.FirstCompleted(leaf, leaf, leaf, leaf))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Lifetime.Close() })
	// Assert.
	wantBytes, wantCloses := int64(len(payload)), int32(1)
	if readyFactory {
		wantBytes, wantCloses = 0, 0
	}
	if consumedAtPreparation != wantBytes || source.bytes.Load() != wantBytes || source.closes.Load() != wantCloses {
		t.Fatal("original stream was consumed again, left incomplete or incorrectly closed")
	}
	if request.Body != source || request.ContentLength != -1 || (request.GetBody != nil) != readyFactory {
		t.Fatal("preparation or fanout mutated original request fields")
	}
	readersMu.Lock()
	defer readersMu.Unlock()
	if len(readers) != branches {
		t.Fatalf("factory readers=%d want=%d", len(readers), branches)
	}
	seen := make(map[*requestAuditReader]bool, branches)
	for _, reader := range readers {
		if seen[reader] || reader.bytes.Load() != int64(len(payload)) || reader.closes.Load() != 1 {
			t.Fatal("attempt reader reused, incomplete or not closed exactly once")
		}
		seen[reader] = true
	}
}

type requestAuditCloser struct {
	*requestAuditReader

	underlying io.Closer
}

func (reader *requestAuditCloser) Close() error {
	_ = reader.requestAuditReader.Close()
	return reader.underlying.Close()
}
