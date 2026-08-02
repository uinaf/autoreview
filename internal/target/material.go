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
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/uinaf/autoreview/internal/protocol"
)

type nulRecordWriter struct {
	buffer []byte
	handle func([]byte) error
	err    error
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
			writer.err = fmt.Errorf("Git path record exceeds protocol maximum")
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
		return fmt.Errorf("Git returned unterminated path record")
	}
	return nil
}

func (collector *Collector) validateTrackedWorktree(ctx context.Context, root string, plan *targetPlan) error {
	flags := &nulRecordWriter{handle: func(raw []byte) error {
		if len(raw) < 3 || raw[1] != ' ' {
			return fmt.Errorf("Git returned malformed index flag record")
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
		if err := validateRegularOrMissingFile(root, path); err != nil {
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
	if err := copyStableFile(filepath.Dir(originalIndex), filepath.Base(originalIndex), filepath.Join(directory, "index")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("copy Git index: %w", err)
	}
	excludeRoot := filepath.Dir(filepath.Dir(originalExclude))
	excludeRelative, err := filepath.Rel(excludeRoot, originalExclude)
	if err != nil {
		return nil, fmt.Errorf("resolve Git exclude path: %w", err)
	}
	if err := copyStableFile(excludeRoot, filepath.ToSlash(excludeRelative), filepath.Join(directory, "info", "exclude")); err != nil && !errors.Is(err, os.ErrNotExist) {
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
		return nil, fmt.Errorf("Git returned invalid empty-tree object ID")
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
	defer index.Close()
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
		return "", fmt.Errorf("Git metadata path %q is not absolute", name)
	}
	return path, nil
}

func copyStableFile(root, relative, destination string) error {
	input, before, err := openRegularFile(root, relative)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
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
			if specialGitMode(oldMode) || specialGitMode(newMode) {
				return fmt.Errorf("symlink or gitlink input %q is unsupported", path)
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

func specialGitMode(mode string) bool {
	return mode == "120000" || mode == "160000"
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
	files := make(map[string][]byte, len(blobs))
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
		check, err := collector.git.runSandbox(ctx, root, plan.sandbox, []byte(blob.oid+"\n"), 128<<10, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
		if err != nil {
			return nil, fmt.Errorf("inspect deleted blob %q: %w", blob.path, err)
		}
		fields := strings.Fields(string(check))
		if len(fields) != 3 || fields[0] != blob.oid || fields[1] != "blob" {
			return nil, fmt.Errorf("Git returned invalid deleted blob metadata for %q", blob.path)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("Git returned invalid deleted blob size for %q", blob.path)
		}
		remaining := budget.Remaining()
		budget.Add("deleted:"+blob.path, size)
		budget.AddFraming(sectionFramingBytes("UNTRUSTED-DELETED-FILE", blob.path, size))
		if remaining < 0 || size > remaining {
			continue
		}
		if size > int64(^uint64(0)>>1)-256 {
			return nil, fmt.Errorf("deleted blob %q is too large", blob.path)
		}
		output, err := collector.git.runSandbox(ctx, root, plan.sandbox, []byte(blob.oid+"\n"), size+256, "cat-file", "--batch")
		if err != nil {
			return nil, fmt.Errorf("read deleted blob %q: %w", blob.path, err)
		}
		headerEnd := bytes.IndexByte(output, '\n')
		if headerEnd < 0 || int64(len(output)-headerEnd-2) != size || output[len(output)-1] != '\n' {
			return nil, fmt.Errorf("Git returned incomplete deleted blob %q", blob.path)
		}
		content := append([]byte(nil), output[headerEnd+1:len(output)-1]...)
		if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
			return nil, fmt.Errorf("deleted blob %q contains binary or invalid UTF-8 content", blob.path)
		}
		if _, exists := files[blob.path]; exists {
			return nil, fmt.Errorf("deleted path %q appears more than once", blob.path)
		}
		if !budget.Exceeded() {
			files[blob.path] = content
		}
	}
	return files, nil
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
	_, _ = hash.Write(head)
	_, _ = hash.Write([]byte{0})
	index, err := os.Open(filepath.Join(plan.sandbox.directory, "index"))
	if errors.Is(err, os.ErrNotExist) {
		_, _ = hash.Write([]byte("no-index"))
	} else if err != nil {
		return "", fmt.Errorf("open copied index for fingerprint: %w", err)
	} else {
		if _, err := io.Copy(hash, index); err != nil {
			index.Close()
			return "", fmt.Errorf("fingerprint copied index: %w", err)
		}
		if err := index.Close(); err != nil {
			return "", fmt.Errorf("close copied index: %w", err)
		}
	}
	_, _ = hash.Write([]byte{0})
	if err := collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, hash, "status", "--porcelain=v2", "-z", "--untracked-files=all"); err != nil {
		return "", fmt.Errorf("fingerprint worktree: %w", err)
	}
	_, _ = hash.Write([]byte{0})
	fingerprintPlan := &targetPlan{oldRevision: worktreeBase, local: true, sandbox: plan.sandbox, attributes: plan.attributes}
	for _, arguments := range diffCommands(fingerprintPlan, "--binary", "--full-index") {
		if err := collector.git.runSandboxWithAttributesTo(ctx, root, plan.sandbox, nil, hash, arguments...); err != nil {
			return "", fmt.Errorf("fingerprint tracked worktree: %w", err)
		}
		_, _ = hash.Write([]byte{0})
	}
	if err := collector.git.runConfiguredTo(ctx, root, nil, hash, "", nil, "config", "list", "--local", "--null", "--includes"); err != nil {
		return "", fmt.Errorf("fingerprint repository Git config: %w", err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
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
	defer file.Close()
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
	base := filepath.Base(lower)
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == ".npmrc" || base == ".pypirc" || base == ".netrc" || base == ".git-credentials" || base == "credentials.json" || base == "id_rsa" || base == "id_ed25519" || strings.HasSuffix(base, ".tfstate") || strings.HasSuffix(base, ".tfstate.backup") {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
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
