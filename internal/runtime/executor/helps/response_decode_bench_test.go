package helps

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)


func benchZstdSSE(b *testing.B, size int) []byte {
	b.Helper()
	r := rand.New(rand.NewSource(1))
	words := strings.Fields("the function return error context request response model input output tool call result value index token cache stream event message content type text")
	var plain strings.Builder
	for i := 0; plain.Len() < size; i++ {
		var delta strings.Builder
		for j, n := 0, 4+r.Intn(12); j < n; j++ {
			delta.WriteString(words[r.Intn(len(words))])
			delta.WriteByte(' ')
		}
		fmt.Fprintf(&plain, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":%d,\"delta\":%q}\n\n", i, delta.String())
	}
	encoder, errNewWriter := zstd.NewWriter(nil)
	if errNewWriter != nil {
		b.Fatal(errNewWriter)
	}
	defer func() { _ = encoder.Close() }()
	return encoder.EncodeAll([]byte(plain.String()), nil)
}

var benchZstdHeader = http.Header{"Content-Encoding": {"zstd"}}


func BenchmarkDecodeResponseBodyZstd(b *testing.B) {
	for _, size := range []int{64 << 10, 2 << 20} {
		wire := benchZstdSSE(b, size)
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					body, errDecode := DecodeResponseBody(io.NopCloser(bytes.NewReader(wire)), benchZstdHeader)
					if errDecode != nil {
						b.Fatal(errDecode)
					}
					if _, errCopy := io.Copy(io.Discard, body); errCopy != nil {
						b.Fatal(errCopy)
					}
					_ = body.Close()
				}
			})
		})
	}
}



func BenchmarkDecodeResponseBodyOpenStreams(b *testing.B) {
	const streams = 256
	wire := benchZstdSSE(b, 256<<10)
	var heap float64
	for i := 0; i < b.N; i++ {
		runtime.GC()
		var before, during runtime.MemStats
		runtime.ReadMemStats(&before)
		bodies := make([]io.ReadCloser, streams)
		buf := make([]byte, 512)
		for s := range bodies {
			body, errDecode := DecodeResponseBody(io.NopCloser(bytes.NewReader(wire)), benchZstdHeader)
			if errDecode != nil {
				b.Fatal(errDecode)
			}
			if _, errRead := body.Read(buf); errRead != nil {
				b.Fatal(errRead)
			}
			bodies[s] = body
		}
		runtime.GC()
		runtime.ReadMemStats(&during)
		heap += float64(int64(during.HeapInuse)-int64(before.HeapInuse)) / streams / 1024
		for _, body := range bodies {
			_ = body.Close()
		}
	}
	b.ReportMetric(heap/float64(b.N), "heap-KiB/stream")
}



func BenchmarkDecodeResponseBodyUnlabeledFirstByte(b *testing.B) {
	var blocked time.Duration
	for i := 0; i < b.N; i++ {
		reader, writer := io.Pipe()
		go func() {
			time.Sleep(20 * time.Millisecond)
			_, _ = writer.Write([]byte("data: {}\n\n"))
			_ = writer.Close()
		}()
		began := time.Now()
		body, errDecode := DecodeResponseBody(reader, http.Header{})
		blocked += time.Since(began)
		if errDecode != nil {
			b.Fatal(errDecode)
		}
		_, _ = io.Copy(io.Discard, body)
		_ = body.Close()
	}
	b.ReportMetric(float64(blocked.Milliseconds())/float64(b.N), "blocked-ms")
}
