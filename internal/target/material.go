package target

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uinaf/autoreview/internal/protocol"
)

const maximumIndexBytes = int64(1 << 30)

type nulRecordWriter struct {
	buffer []byte
	handle func([]byte) error
	err    error
}

type lineRecordWriter struct {
	buffer []byte
	handle func([]byte) error
	err    error
}

func (writer *lineRecordWriter) Write(data []byte) (int, error) {
	for _, character := range data {
		if writer.err != nil {
			continue
		}
		if character == '\n' {
			writer.err = writer.handle(writer.buffer)
			writer.buffer = writer.buffer[:0]
			continue
		}
		if len(writer.buffer) >= 512 {
			writer.err = fmt.Errorf("git line record exceeds safe maximum")
			continue
		}
		writer.buffer = append(writer.buffer, character)
	}
	return len(data), nil
}

func (writer *lineRecordWriter) Err() error {
	if writer.err != nil {
		return writer.err
	}
	if len(writer.buffer) != 0 {
		return fmt.Errorf("git returned an unterminated line record")
	}
	return nil
}

func (writer *nulRecordWriter) Write(data []byte) (int, error) {
	for _, character := range data {
		if writer.err != nil {
			continue
		}
		if character == 0 {
			writer.err = writer.handle(writer.buffer)
			writer.buffer = writer.buffer[:0]
			continue
		}
		if len(writer.buffer) >= 4*protocol.MaxPathCharacters+128 {
			writer.err = fmt.Errorf("git path record exceeds protocol maximum")
			continue
		}
		writer.buffer = append(writer.buffer, character)
	}
	return len(data), nil
}

func (writer *nulRecordWriter) Err() error {
	if writer.err != nil {
		return writer.err
	}
	if len(writer.buffer) != 0 {
		return fmt.Errorf("git returned unterminated path record")
	}
	return nil
}

func (collector *Collector) validateTrackedWorktree(ctx context.Context, root string, plan *targetPlan) error {
	flags := &nulRecordWriter{handle: func(raw []byte) error {
		if len(raw) < 3 || raw[1] != ' ' {
			return fmt.Errorf("git returned malformed index flag record")
		}
		tag := raw[0]
		if tag == 'S' || (tag >= 'a' && tag <= 'z') {
			return fmt.Errorf("index flags that hide worktree changes are unsupported for %q", raw[2:])
		}
		return nil
	}}
	if err := collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, flags, "ls-files", "-v", "-z"); err != nil {
		return fmt.Errorf("inspect index flags: %w", err)
	}
	if err := flags.Err(); err != nil {
		return err
	}
	parser := &nulRecordWriter{handle: func(raw []byte) error {
		path := string(raw)
		if err := protocolPath(path); err != nil {
			return fmt.Errorf("tracked path %q: %w", path, err)
		}
		if err := validateTrackedWorktreePath(root, path); err != nil {
			return fmt.Errorf("inspect tracked path %q: %w", path, err)
		}
		return nil
	}}
	if err := collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, parser, "ls-files", "--modified", "--deleted", "-z"); err != nil {
		return fmt.Errorf("list tracked paths: %w", err)
	}
	return parser.Err()
}

func (collector *Collector) newGitSandbox(ctx context.Context, root string) (_ *gitSandbox, returnErr error) {
	objectFormatOutput, err := collector.git.run(ctx, root, nil, 128<<10, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, fmt.Errorf("resolve Git object format: %w", err)
	}
	objectFormat := strings.TrimSpace(string(objectFormatOutput))
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return nil, fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
	originalObjects, err := collector.gitMetadataPath(ctx, root, "objects")
	if err != nil {
		return nil, err
	}
	originalIndex, err := collector.gitMetadataPath(ctx, root, "index")
	if err != nil {
		return nil, err
	}
	originalExclude, err := collector.gitMetadataPath(ctx, root, "info/exclude")
	if err != nil {
		return nil, err
	}

	directory, err := os.MkdirTemp("", "autoreview-git-")
	if err != nil {
		return nil, fmt.Errorf("create isolated Git directory: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	if _, err := collector.git.run(ctx, filepath.Dir(directory), nil, 128<<10, "init", "--quiet", "--bare", "--object-format="+objectFormat, directory); err != nil {
		return nil, fmt.Errorf("initialize isolated Git directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "objects", "info"), 0o700); err != nil {
		return nil, fmt.Errorf("create isolated object database: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "info"), 0o700); err != nil {
		return nil, fmt.Errorf("create isolated Git metadata: %w", err)
	}
	alternate := filepath.Join(directory, "original-objects")
	if err := os.Symlink(originalObjects, alternate); err != nil {
		return nil, fmt.Errorf("link original Git objects: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "objects", "info", "alternates"), []byte(alternate+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("configure isolated Git objects: %w", err)
	}
	config := "[core]\n\trepositoryFormatVersion = 0\n\tbare = false\n"
	if objectFormat == "sha256" {
		config = "[core]\n\trepositoryFormatVersion = 1\n\tbare = false\n[extensions]\n\tobjectFormat = sha256\n"
	}
	if err := os.WriteFile(filepath.Join(directory, "config"), []byte(config), 0o600); err != nil {
		return nil, fmt.Errorf("write isolated Git config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "HEAD"), []byte("ref: refs/heads/autoreview\n"), 0o600); err != nil {
		return nil, fmt.Errorf("initialize isolated Git HEAD: %w", err)
	}
	// Preserve the original index mtime. A naive copy advances the file's
	// timestamp and closes Git's racy-git window, which can hide same-size
	// worktree edits (including symlink retargets) on the verification collect.
	var indexModTime time.Time
	var hasIndex bool
	if info, statErr := os.Stat(originalIndex); statErr == nil {
		indexModTime = info.ModTime()
		hasIndex = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Git index: %w", statErr)
	}
	copiedIndex := filepath.Join(directory, "index")
	if err := copyStableFile(filepath.Dir(originalIndex), filepath.Base(originalIndex), copiedIndex, maximumIndexBytes); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("copy Git index: %w", err)
		}
	} else if hasIndex {
		if err := os.Chtimes(copiedIndex, indexModTime, indexModTime); err != nil {
			return nil, fmt.Errorf("preserve Git index mtime: %w", err)
		}
	}
	excludeRoot := filepath.Dir(filepath.Dir(originalExclude))
	excludeRelative, err := filepath.Rel(excludeRoot, originalExclude)
	if err != nil {
		return nil, fmt.Errorf("resolve Git exclude path: %w", err)
	}
	if err := copyStableFile(excludeRoot, filepath.ToSlash(excludeRelative), filepath.Join(directory, "info", "exclude"), metadataLimit); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("copy Git excludes: %w", err)
	}

	sandbox := &gitSandbox{
		directory: directory,
		environment: []string{
			"GIT_DIR=" + directory,
			"GIT_WORK_TREE=" + root,
			"GIT_INDEX_FILE=" + filepath.Join(directory, "index"),
			"GIT_NO_REPLACE_OBJECTS=1",
		},
	}
	if err := rejectSplitIndex(filepath.Join(directory, "index"), objectFormat); err != nil {
		return nil, err
	}
	emptyTree, err := collector.git.runSandbox(ctx, root, sandbox, nil, 128<<10, "hash-object", "-w", "-t", "tree", "--stdin")
	if err != nil {
		return nil, fmt.Errorf("write isolated empty tree: %w", err)
	}
	sandbox.attributeSource = strings.TrimSpace(string(emptyTree))
	if !validObjectID(sandbox.attributeSource) {
		return nil, fmt.Errorf("git returned invalid empty-tree object ID")
	}
	head, unborn, err := collector.resolveHEAD(ctx, root)
	if err != nil {
		return nil, err
	}
	if unborn {
		head = sandbox.attributeSource
	}
	if err := os.WriteFile(filepath.Join(directory, "HEAD"), []byte(head+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write isolated Git HEAD: %w", err)
	}
	return sandbox, nil
}

func rejectSplitIndex(path, objectFormat string) error {
	checksumBytes := int64(20)
	if objectFormat == "sha256" {
		checksumBytes = 32
	}
	index, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open copied Git index: %w", err)
	}
	defer func() { _ = index.Close() }()
	info, err := index.Stat()
	if err != nil {
		return fmt.Errorf("inspect copied Git index: %w", err)
	}
	contentBytes := info.Size() - checksumBytes
	if contentBytes < 12 {
		return fmt.Errorf("copied Git index is truncated")
	}
	reader := bufio.NewReader(io.LimitReader(index, contentBytes))
	header := make([]byte, 12)
	if _, err := io.ReadFull(reader, header); err != nil {
		return fmt.Errorf("read copied Git index header: %w", err)
	}
	if string(header[:4]) != "DIRC" {
		return fmt.Errorf("copied Git index has an invalid signature")
	}
	version := binary.BigEndian.Uint32(header[4:8])
	if version < 2 || version > 4 {
		return fmt.Errorf("copied Git index has unsupported version %d", version)
	}
	entryCount := binary.BigEndian.Uint32(header[8:12])
	fixedBytes := int64(42) + checksumBytes
	if uint64(entryCount) > uint64(contentBytes-12)/uint64(fixedBytes+1) {
		return fmt.Errorf("copied Git index entry count exceeds its size")
	}
	offset := int64(12)
	fixed := make([]byte, fixedBytes)
	for entry := uint32(0); entry < entryCount; entry++ {
		if _, err := io.ReadFull(reader, fixed); err != nil {
			return fmt.Errorf("read copied Git index entry %d: %w", entry, err)
		}
		offset += fixedBytes
		flags := binary.BigEndian.Uint16(fixed[len(fixed)-2:])
		extendedBytes := int64(0)
		if flags&0x4000 != 0 {
			if version == 2 {
				return fmt.Errorf("copied Git index version 2 entry has extended flags")
			}
			var extended [2]byte
			if _, err := io.ReadFull(reader, extended[:]); err != nil {
				return fmt.Errorf("read copied Git index entry %d flags: %w", entry, err)
			}
			offset += int64(len(extended))
			extendedBytes = int64(len(extended))
		}
		if version == 4 {
			for encodedBytes := 0; ; encodedBytes++ {
				if encodedBytes >= 10 {
					return fmt.Errorf("copied Git index entry %d has an invalid path prefix", entry)
				}
				value, err := reader.ReadByte()
				if err != nil {
					return fmt.Errorf("read copied Git index entry %d path prefix: %w", entry, err)
				}
				offset++
				if value&0x80 == 0 {
					break
				}
			}
		}
		pathBytes := int64(0)
		for {
			value, err := reader.ReadByte()
			if err != nil {
				return fmt.Errorf("read copied Git index entry %d path: %w", entry, err)
			}
			offset++
			if value == 0 {
				break
			}
			pathBytes++
			if pathBytes > 4*protocol.MaxPathCharacters+128 {
				return fmt.Errorf("copied Git index entry %d path exceeds protocol maximum", entry)
			}
		}
		if version != 4 {
			entryBytes := fixedBytes + extendedBytes + pathBytes
			paddingBytes := int64(8) - entryBytes%8
			remainingPadding := paddingBytes - 1
			for padding := int64(0); padding < remainingPadding; padding++ {
				value, err := reader.ReadByte()
				if err != nil {
					return fmt.Errorf("read copied Git index entry %d padding: %w", entry, err)
				}
				offset++
				if value != 0 {
					return fmt.Errorf("copied Git index entry %d has invalid padding", entry)
				}
			}
		}
	}
	for offset < contentBytes {
		if contentBytes-offset < 8 {
			return fmt.Errorf("copied Git index has a truncated extension")
		}
		var extension [8]byte
		if _, err := io.ReadFull(reader, extension[:]); err != nil {
			return fmt.Errorf("read copied Git index extension: %w", err)
		}
		offset += int64(len(extension))
		size := int64(binary.BigEndian.Uint32(extension[4:]))
		if size > contentBytes-offset {
			return fmt.Errorf("copied Git index extension exceeds its size")
		}
		if string(extension[:4]) == "link" {
			return fmt.Errorf("split Git indexes are unsupported")
		}
		if _, err := io.CopyN(io.Discard, reader, size); err != nil {
			return fmt.Errorf("read copied Git index extension: %w", err)
		}
		offset += size
	}
	return nil
}

func (collector *Collector) gitMetadataPath(ctx context.Context, root, name string) (string, error) {
	output, err := collector.git.run(ctx, root, nil, 128<<10, "rev-parse", "--path-format=absolute", "--git-path", name)
	if err != nil {
		return "", fmt.Errorf("resolve Git metadata path %q: %w", name, err)
	}
	path := strings.TrimSpace(string(output))
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("git metadata path %q is not absolute", name)
	}
	return path, nil
}

func copyStableFile(root, relative, destination string, limit int64) error {
	input, before, err := openRegularFile(root, relative)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	if before.Size() > limit {
		return fmt.Errorf("file exceeds safe copy limit of %d bytes", limit)
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.CopyN(output, input, limit+1)
	closeErr := output.Close()
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > limit {
		return fmt.Errorf("file exceeds safe copy limit of %d bytes", limit)
	}
	after, err := input.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("file changed while copying")
	}
	return nil
}

type deletedBlob struct {
	path string
	oid  string
}

func (collector *Collector) changedPaths(ctx context.Context, root string, plan *targetPlan, maxBytes int64) ([]string, []deletedBlob, error) {
	var paths []string
	var inventoryBytes int64
	for _, arguments := range diffCommands(plan, "--numstat", "-z") {
		parser := &nulRecordWriter{handle: func(record []byte) error {
			fields := bytes.SplitN(record, []byte{'\t'}, 3)
			if len(fields) != 3 {
				return fmt.Errorf("git numstat returned malformed record")
			}
			if string(fields[0]) == "-" || string(fields[1]) == "-" {
				return fmt.Errorf("binary input %q is unsupported", fields[2])
			}
			if _, err := strconv.ParseInt(string(fields[0]), 10, 64); err != nil {
				return fmt.Errorf("git numstat returned invalid addition count")
			}
			if _, err := strconv.ParseInt(string(fields[1]), 10, 64); err != nil {
				return fmt.Errorf("git numstat returned invalid deletion count")
			}
			path := string(fields[2])
			inventoryBytes += int64(len(path) + 1)
			if inventoryBytes > maxBytes {
				return &SizeError{Limit: maxBytes, Actual: inventoryBytes, Contributors: []Contributor{{Name: "framing", Bytes: inventoryBytes}}}
			}
			paths = append(paths, path)
			return nil
		}}
		if err := collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, parser, arguments...); err != nil {
			return nil, nil, fmt.Errorf("list changed paths: %w", err)
		}
		if err := parser.Err(); err != nil {
			return nil, nil, err
		}
	}
	deleted, err := collector.inspectRawModes(ctx, root, plan)
	if err != nil {
		return nil, nil, err
	}
	return paths, deleted, nil
}

func (collector *Collector) inspectRawModes(ctx context.Context, root string, plan *targetPlan) ([]deletedBlob, error) {
	var deleted []deletedBlob
	for _, arguments := range diffCommands(plan, "--raw", "--abbrev=64", "-z") {
		var rawHeader []byte
		parser := &nulRecordWriter{handle: func(record []byte) error {
			if rawHeader == nil {
				rawHeader = append([]byte(nil), record...)
				return nil
			}
			header := strings.Fields(string(rawHeader))
			rawHeader = nil
			if len(header) < 5 || !strings.HasPrefix(header[0], ":") {
				return fmt.Errorf("git raw diff returned malformed record")
			}
			oldMode := strings.TrimPrefix(header[0], ":")
			newMode := header[1]
			path := string(record)
			if gitlinkMode(oldMode) || gitlinkMode(newMode) {
				return fmt.Errorf("gitlink input %q is unsupported", path)
			}
			if newMode == "000000" {
				deleted = append(deleted, deletedBlob{path: path, oid: header[2]})
			}
			return nil
		}}
		if err := collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, parser, arguments...); err != nil {
			return nil, fmt.Errorf("inspect changed modes: %w", err)
		}
		if err := parser.Err(); err != nil {
			return nil, err
		}
		if rawHeader != nil {
			return nil, fmt.Errorf("git raw diff returned incomplete record")
		}
	}
	return deleted, nil
}

func diffCommands(plan *targetPlan, format ...string) [][]string {
	common := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames"}
	common = append(common, format...)
	if plan.local {
		staged := append(append([]string(nil), common...), "--cached", plan.oldRevision, "--")
		unstaged := append(append([]string(nil), common...), "--")
		return [][]string{staged, unstaged}
	}
	arguments := append(append([]string(nil), common...), plan.oldRevision)
	if plan.newRevision != "" {
		arguments = append(arguments, plan.newRevision)
	}
	return [][]string{append(arguments, "--")}
}

func gitlinkMode(mode string) bool {
	return mode == "160000"
}

func (collector *Collector) untrackedFiles(ctx context.Context, root string, plan *targetPlan, budget *byteBudget) (map[string][]byte, error) {
	files := map[string][]byte{}
	parser := &nulRecordWriter{handle: func(rawPath []byte) error {
		path := string(rawPath)
		if err := protocolPath(path); err != nil {
			return fmt.Errorf("untracked path %q: %w", path, err)
		}
		if sensitivePath(path) {
			return fmt.Errorf("sensitive path %q is not reviewable", path)
		}
		content, size, err := budget.Read(root, path, "untracked:"+path)
		if err != nil {
			return fmt.Errorf("read untracked file %q: %w", path, err)
		}
		budget.AddFraming(sectionFramingBytes("UNTRUSTED-UNTRACKED-FILE", path, size))
		if !budget.Exceeded() {
			files[path] = content
		}
		return nil
	}}
	if err := collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, parser, "ls-files", "--others", "--exclude-standard", "-z"); err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	return files, parser.Err()
}

func (collector *Collector) deletedFiles(ctx context.Context, root string, plan *targetPlan, blobs []deletedBlob, budget *byteBudget) (map[string][]byte, error) {
	files := map[string][]byte{}
	if len(blobs) == 0 {
		return files, nil
	}
	var checkInput bytes.Buffer
	seen := make(map[string]struct{})
	for _, blob := range blobs {
		if err := protocolPath(blob.path); err != nil {
			return nil, fmt.Errorf("deleted path %q: %w", blob.path, err)
		}
		if sensitivePath(blob.path) {
			return nil, fmt.Errorf("sensitive path %q is not reviewable", blob.path)
		}
		if !validObjectID(blob.oid) {
			return nil, fmt.Errorf("deleted path %q has invalid blob ID", blob.path)
		}
		if _, exists := seen[blob.path]; exists {
			return nil, fmt.Errorf("deleted path %q appears more than once", blob.path)
		}
		seen[blob.path] = struct{}{}
		_, _ = checkInput.WriteString(blob.oid)
		_ = checkInput.WriteByte('\n')
	}
	materials := make([]deletedBlobMaterial, 0, len(blobs))
	metadataIndex := 0
	metadata := &lineRecordWriter{handle: func(record []byte) error {
		if metadataIndex >= len(blobs) {
			return fmt.Errorf("git returned excess deleted blob metadata")
		}
		blob := blobs[metadataIndex]
		metadataIndex++
		fields := strings.Fields(string(record))
		if len(fields) != 3 || fields[0] != blob.oid || fields[1] != "blob" {
			return fmt.Errorf("git returned invalid deleted blob metadata for %q", blob.path)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return fmt.Errorf("git returned invalid deleted blob size for %q", blob.path)
		}
		budget.Add("deleted:"+blob.path, size)
		budget.AddFraming(sectionFramingBytes("UNTRUSTED-DELETED-FILE", blob.path, size))
		if !budget.Exceeded() {
			materials = append(materials, deletedBlobMaterial{deletedBlob: blob, size: size})
		}
		return nil
	}}
	if err := collector.git.runSandboxTo(ctx, root, plan.sandbox, checkInput.Bytes(), metadata, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)"); err != nil {
		return nil, fmt.Errorf("inspect deleted blobs: %w", err)
	}
	if err := metadata.Err(); err != nil {
		return nil, err
	}
	if metadataIndex != len(blobs) {
		return nil, fmt.Errorf("git returned incomplete deleted blob metadata")
	}
	if len(materials) == 0 {
		return files, nil
	}
	var contentInput bytes.Buffer
	for _, material := range materials {
		_, _ = contentInput.WriteString(material.oid)
		_ = contentInput.WriteByte('\n')
	}
	content := newDeletedBatchWriter(materials, files)
	if err := collector.git.runSandboxTo(ctx, root, plan.sandbox, contentInput.Bytes(), content, "cat-file", "--batch"); err != nil {
		return nil, fmt.Errorf("read deleted blobs: %w", err)
	}
	if err := content.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

type deletedBlobMaterial struct {
	deletedBlob
	size int64
}

type deletedBatchWriter struct {
	materials []deletedBlobMaterial
	files     map[string][]byte
	index     int
	header    []byte
	content   []byte
	remaining int64
	err       error
}

func newDeletedBatchWriter(materials []deletedBlobMaterial, files map[string][]byte) *deletedBatchWriter {
	return &deletedBatchWriter{materials: materials, files: files, remaining: -1}
}

func (writer *deletedBatchWriter) Write(data []byte) (int, error) {
	input := data
	for len(input) > 0 && writer.err == nil {
		if writer.index >= len(writer.materials) {
			writer.err = fmt.Errorf("git returned excess deleted blob content")
			break
		}
		material := writer.materials[writer.index]
		if writer.remaining < 0 {
			newline := bytes.IndexByte(input, '\n')
			if newline < 0 {
				if len(writer.header)+len(input) > 512 {
					writer.err = fmt.Errorf("git deleted blob header exceeds safe maximum")
					break
				}
				writer.header = append(writer.header, input...)
				break
			}
			writer.header = append(writer.header, input[:newline]...)
			input = input[newline+1:]
			fields := strings.Fields(string(writer.header))
			if len(fields) != 3 || fields[0] != material.oid || fields[1] != "blob" || fields[2] != strconv.FormatInt(material.size, 10) {
				writer.err = fmt.Errorf("git returned invalid deleted blob header for %q", material.path)
				break
			}
			writer.content = make([]byte, 0, int(material.size))
			writer.remaining = material.size
			writer.header = writer.header[:0]
		}
		if writer.remaining > 0 {
			consume := int64(len(input))
			if consume > writer.remaining {
				consume = writer.remaining
			}
			writer.content = append(writer.content, input[:int(consume)]...)
			input = input[int(consume):]
			writer.remaining -= consume
		}
		if writer.remaining == 0 && len(input) > 0 {
			if input[0] != '\n' {
				writer.err = fmt.Errorf("git returned incomplete deleted blob %q", material.path)
				break
			}
			input = input[1:]
			if !utf8.Valid(writer.content) || bytes.IndexByte(writer.content, 0) >= 0 {
				writer.err = fmt.Errorf("deleted blob %q contains binary or invalid UTF-8 content", material.path)
				break
			}
			writer.files[material.path] = writer.content
			writer.content = nil
			writer.remaining = -1
			writer.index++
		}
	}
	return len(data), nil
}

func (writer *deletedBatchWriter) Err() error {
	if writer.err != nil {
		return writer.err
	}
	if writer.index != len(writer.materials) || writer.remaining != -1 || len(writer.header) != 0 {
		return fmt.Errorf("git returned incomplete deleted blob content")
	}
	return nil
}

func (collector *Collector) sourceStateHash(ctx context.Context, root string, plan *targetPlan) (string, error) {
	headRevision, unborn, err := collector.resolveHEAD(ctx, root)
	if err != nil {
		return "", err
	}
	head := []byte(headRevision)
	worktreeBase := headRevision
	if unborn {
		head = []byte("unborn")
		worktreeBase = plan.attributes
	}
	hash := sha256.New()
	if err := hashSourceSection(hash, "head", func(output io.Writer) error {
		_, err := output.Write(head)
		return err
	}); err != nil {
		return "", fmt.Errorf("fingerprint head: %w", err)
	}
	if err := hashSourceSection(hash, "index", func(output io.Writer) (returnErr error) {
		index, err := os.Open(filepath.Join(plan.sandbox.directory, "index"))
		if errors.Is(err, os.ErrNotExist) {
			_, err = io.WriteString(output, "no-index")
			return err
		}
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := index.Close(); closeErr != nil {
				returnErr = errors.Join(returnErr, closeErr)
			}
		}()
		_, err = io.Copy(output, index)
		return err
	}); err != nil {
		return "", fmt.Errorf("fingerprint copied index: %w", err)
	}
	if err := hashSourceSection(hash, "status", func(output io.Writer) error {
		return collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, output, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	}); err != nil {
		return "", fmt.Errorf("fingerprint worktree: %w", err)
	}
	fingerprintPlan := &targetPlan{oldRevision: worktreeBase, local: true, sandbox: plan.sandbox, attributes: plan.attributes}
	for index, arguments := range diffCommands(fingerprintPlan, "--binary", "--full-index") {
		if err := hashSourceSection(hash, "diff-"+strconv.Itoa(index), func(output io.Writer) error {
			return collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, output, arguments...)
		}); err != nil {
			return "", fmt.Errorf("fingerprint tracked worktree: %w", err)
		}
	}
	if err := hashSourceSection(hash, "config", func(output io.Writer) error {
		return collector.git.runConfiguredTo(ctx, root, nil, output, "", nil, "config", "list", "--local", "--null", "--includes")
	}); err != nil {
		return "", fmt.Errorf("fingerprint repository Git config: %w", err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

type sourceSectionWriter struct {
	hash  io.Writer
	bytes uint64
}

func (writer *sourceSectionWriter) Write(data []byte) (int, error) {
	written, err := writer.hash.Write(data)
	writer.bytes += uint64(written)
	return written, err
}

func hashSourceSection(destination io.Writer, label string, write func(io.Writer) error) error {
	sectionHash := sha256.New()
	section := &sourceSectionWriter{hash: sectionHash}
	if err := write(section); err != nil {
		return err
	}
	var frame [12]byte
	binary.BigEndian.PutUint32(frame[:4], uint32(len(label)))
	binary.BigEndian.PutUint64(frame[4:], section.bytes)
	if _, err := destination.Write(frame[:]); err != nil {
		return err
	}
	if _, err := io.WriteString(destination, label); err != nil {
		return err
	}
	_, err := destination.Write(sectionHash.Sum(nil))
	return err
}

func (collector *Collector) validateRepositoryConfig(ctx context.Context, root string) error {
	output, err := collector.git.run(ctx, root, nil, metadataLimit, "config", "list", "--local", "--name-only", "--null", "--includes")
	if err != nil {
		return fmt.Errorf("inspect repository Git config: %w", err)
	}
	for _, rawName := range bytes.Split(output, []byte{0}) {
		name := strings.ToLower(string(rawName))
		if name == "core.excludesfile" {
			return fmt.Errorf("repository Git config contains unsupported external excludes file")
		}
		if strings.HasPrefix(name, "filter.") || (strings.HasPrefix(name, "diff.") && (strings.HasSuffix(name, ".command") || strings.HasSuffix(name, ".textconv"))) {
			return fmt.Errorf("repository Git config contains executable filter or diff driver %q", name)
		}
	}
	return nil
}

func readContainedFile(root, relative string, retainLimit int64) ([]byte, int64, error) {
	if err := protocolPath(relative); err != nil {
		return nil, 0, err
	}
	file, before, err := openRegularFile(root, relative)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = file.Close() }()
	if before.Size() > retainLimit {
		after, err := file.Stat()
		if err != nil {
			return nil, 0, err
		}
		if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return nil, 0, fmt.Errorf("file changed while counting")
		}
		return nil, before.Size(), nil
	}
	content, err := io.ReadAll(io.LimitReader(file, retainLimit+1))
	if err != nil {
		return nil, 0, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, 0, fmt.Errorf("file changed while reading")
	}
	if int64(len(content)) != before.Size() {
		return nil, 0, fmt.Errorf("incomplete file read")
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, 0, fmt.Errorf("binary or invalid UTF-8 content")
	}
	return content, before.Size(), nil
}

func protocolPath(value string) error {
	return protocol.ValidatePath(value)
}

func sensitivePath(value string) bool {
	lower := strings.ToLower(value)
	base := path.Base(lower)
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == ".envrc" || base == ".npmrc" || base == ".pypirc" || base == ".netrc" || base == ".pgpass" || base == ".git-credentials" || base == "credentials.json" || base == "id_rsa" || base == "id_ed25519" || base == "id_ecdsa" || base == "id_dsa" || strings.HasSuffix(base, ".tfstate") || strings.HasSuffix(base, ".tfstate.backup") {
		return true
	}
	switch path.Ext(base) {
	case ".pem", ".key", ".p12", ".pfx", ".jks", ".keystore":
		return true
	default:
		return false
	}
}

func lineCount(content []byte) (int, error) {
	lines := bytes.Count(content, []byte{'\n'})
	if len(content) == 0 || content[len(content)-1] != '\n' {
		lines++
	}
	if lines < 1 {
		return 1, nil
	}
	if lines > protocol.MaxLineNumber {
		return 0, fmt.Errorf("line count exceeds protocol maximum")
	}
	return lines, nil
}
