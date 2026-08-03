package target

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/uinaf/autoreview/internal/protocol"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type diffWriter struct {
	buffer       *limitBuffer
	total        int64
	prefixBytes  int64
	sectionBytes []int64
	section      int
	candidate    bool
	matched      int
	paths        []string
}

func newDiffWriter(limit int64, paths []string) *diffWriter {
	return &diffWriter{buffer: newLimitBuffer(limit), section: -1, candidate: true, paths: append([]string(nil), paths...)}
}

func (writer *diffWriter) Write(data []byte) (int, error) {
	_, _ = writer.buffer.Write(data)
	marker := []byte("diff --git ")
	for _, character := range data {
		writer.total++
		if writer.section < 0 {
			writer.prefixBytes++
		} else {
			writer.sectionBytes[writer.section]++
		}
		if writer.candidate {
			if character == marker[writer.matched] {
				writer.matched++
				if writer.matched == len(marker) {
					if writer.section < 0 {
						writer.prefixBytes -= int64(len(marker))
					} else {
						writer.sectionBytes[writer.section] -= int64(len(marker))
					}
					writer.section++
					writer.sectionBytes = append(writer.sectionBytes, int64(len(marker)))
					writer.candidate = false
				}
			} else {
				writer.candidate = false
			}
		}
		if character == '\n' {
			writer.candidate = true
			writer.matched = 0
		}
	}
	return len(data), nil
}

func (writer *diffWriter) TakeBytes() []byte {
	result := writer.buffer.buffer.Bytes()
	writer.buffer.buffer = bytes.Buffer{}
	return result
}

func (writer *diffWriter) Exceeded() bool {
	return writer.buffer.exceeded
}

func (writer *diffWriter) Contributors() []Contributor {
	if writer.prefixBytes != 0 || len(writer.sectionBytes) != len(writer.paths) {
		return []Contributor{{Name: "diff", Bytes: writer.total}}
	}
	contributors := make([]Contributor, 0, len(writer.paths))
	for index, path := range writer.paths {
		contributors = append(contributors, Contributor{Name: "diff-section:" + path, Bytes: writer.sectionBytes[index]})
	}
	return contributors
}

func (writer *diffWriter) TopContributors() []Contributor {
	if writer.prefixBytes != 0 || len(writer.sectionBytes) != len(writer.paths) {
		return []Contributor{{Name: "diff", Bytes: writer.total}}
	}
	top := make([]Contributor, 0, len(writer.paths))
	for index, path := range writer.paths {
		top = append(top, Contributor{Name: "diff-section:" + path, Bytes: writer.sectionBytes[index]})
	}
	sort.SliceStable(top, func(i, j int) bool { return top[i].Bytes > top[j].Bytes })
	if len(top) > 5 {
		top = top[:5]
	}
	return top
}

func parseDiffRanges(diff []byte, paths []string) (map[string][]protocol.LineRange, error) {
	ranges := map[string][]protocol.LineRange{}
	section := -1
	for start := 0; start <= len(diff); {
		end := bytes.IndexByte(diff[start:], '\n')
		if end < 0 {
			end = len(diff)
		} else {
			end += start
		}
		rawLine := diff[start:end]
		switch {
		case bytes.HasPrefix(rawLine, []byte("diff --git ")):
			section++
			if section >= len(paths) {
				return nil, fmt.Errorf("diff has more file sections than path inventory")
			}
		case section >= 0 && bytes.HasPrefix(rawLine, []byte("@@ ")):
			line := string(rawLine)
			matches := hunkHeader.FindStringSubmatch(line)
			if matches == nil {
				return nil, fmt.Errorf("malformed diff hunk header")
			}
			oldStart, err := lineField(matches[1], false)
			if err != nil {
				return nil, fmt.Errorf("diff hunk old start: %w", err)
			}
			oldCount, err := lineField(matches[2], true)
			if err != nil {
				return nil, fmt.Errorf("diff hunk old count: %w", err)
			}
			newStart, err := lineField(matches[3], false)
			if err != nil {
				return nil, fmt.Errorf("diff hunk new start: %w", err)
			}
			newCount, err := lineField(matches[4], true)
			if err != nil {
				return nil, fmt.Errorf("diff hunk new count: %w", err)
			}
			path := paths[section]
			start := newStart
			count := newCount
			if newCount == 0 {
				start = oldStart
				count = oldCount
			}
			if count < 1 {
				count = 1
			}
			if start < 1 {
				start = 1
			}
			if start > protocol.MaxLineNumber || count > protocol.MaxLineNumber || start > protocol.MaxLineNumber-count+1 {
				return nil, fmt.Errorf("diff line range exceeds protocol maximum")
			}
			end := start + count - 1
			ranges[path] = append(ranges[path], protocol.LineRange{StartLine: start, EndLine: end})
		}
		if end == len(diff) {
			break
		}
		start = end + 1
	}
	if section+1 != len(paths) {
		return nil, fmt.Errorf("diff file sections do not match path inventory")
	}
	for path, fileRanges := range ranges {
		ranges[path] = mergeLineRanges(fileRanges)
	}
	return ranges, nil
}

func mergeLineRanges(fileRanges []protocol.LineRange) []protocol.LineRange {
	sort.Slice(fileRanges, func(i, j int) bool { return fileRanges[i].StartLine < fileRanges[j].StartLine })
	merged := fileRanges[:0]
	for _, current := range fileRanges {
		if len(merged) == 0 || current.StartLine > merged[len(merged)-1].EndLine+1 {
			merged = append(merged, current)
			continue
		}
		if current.EndLine > merged[len(merged)-1].EndLine {
			merged[len(merged)-1].EndLine = current.EndLine
		}
	}
	return merged
}

func lineField(value string, optional bool) (int, error) {
	if value == "" {
		if optional {
			return 1, nil
		}
		return 0, fmt.Errorf("missing value")
	}
	number, err := strconv.ParseInt(value, 10, 32)
	if err != nil || number > protocol.MaxLineNumber {
		return 0, fmt.Errorf("unparsable value")
	}
	return int(number), nil
}
