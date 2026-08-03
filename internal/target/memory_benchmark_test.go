package target

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/uinaf/autoreview/internal/protocol"
)

func TestLimitStringBuilderResultDoesNotAliasLaterWrites(t *testing.T) {
	builder := newLimitStringBuilder(64)
	_, _ = builder.WriteString("frozen")
	payload := builder.String()
	_, _ = builder.WriteString("-later-write")
	if payload != "frozen" {
		t.Fatalf("frozen payload changed to %q", payload)
	}
}

func TestDiffWriterTransferDoesNotAliasLaterWrites(t *testing.T) {
	writer := newDiffWriter(1024, []string{"file.txt"})
	_, _ = writer.Write([]byte("diff --git a/file.txt b/file.txt\n+x\n"))
	diff := writer.TakeBytes()
	want := append([]byte(nil), diff...)
	_, _ = writer.Write(bytes.Repeat([]byte("z"), len(diff)*2))
	if !bytes.Equal(diff, want) {
		t.Fatal("transferred diff changed after writer reuse")
	}
}

func BenchmarkComposeBundle(b *testing.B) {
	for _, size := range []int{1 << 20, 16 << 20, 64 << 20, 128 << 20} {
		b.Run(fmt.Sprintf("diff-%dMiB", size>>20), func(b *testing.B) {
			diff := []byte(strings.Repeat("x", size))
			target := protocol.Target{
				Mode: protocol.TargetLocal,
				Files: []protocol.ReviewedFile{{
					FilePath:   "large.txt",
					LineRanges: []protocol.LineRange{{StartLine: 1, EndLine: 1}},
				}},
			}
			contributors := []Contributor{{Name: "diff-section:large.txt", Bytes: int64(len(diff))}}
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for range b.N {
				payload, _, _, err := composeBundle(target, "sha256:state", "review it", diff, contributors, nil, nil, nil, int64(size+(1<<20)))
				if err != nil {
					b.Fatal(err)
				}
				if len(payload) <= len(diff) {
					b.Fatal("bundle omitted framing")
				}
			}
		})
	}
}

func BenchmarkParseDiffRangesManyShortLines(b *testing.B) {
	const lines = 1_000_000
	var diff strings.Builder
	diff.Grow(len("diff --git a/many.txt b/many.txt\n") + len("@@ -0,0 +1,1000000 @@\n") + lines*3)
	diff.WriteString("diff --git a/many.txt b/many.txt\n")
	diff.WriteString("@@ -0,0 +1,1000000 @@\n")
	for range lines {
		diff.WriteString("+x\n")
	}
	input := []byte(diff.String())
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for range b.N {
		ranges, err := parseDiffRanges(input, []string{"many.txt"})
		if err != nil {
			b.Fatal(err)
		}
		if len(ranges["many.txt"]) != 1 {
			b.Fatalf("ranges = %v", ranges)
		}
	}
}
