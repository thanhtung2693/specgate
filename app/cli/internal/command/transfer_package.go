package command

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/specgate/specgate/app/cli/internal/local"
)

const (
	transferFormat       = "specgate.local-transfer/v1"
	transferReceiptName  = "received.json"
	transferManifestMax  = 1 << 20
	transferPayloadMax   = 512 << 20
	transferArchiveMax   = transferPayloadMax + 8<<20
	transferDirectoryMax = 64 << 10
)

var errTransferTooLarge = errors.New("transfer size limit exceeded")

type localTransferManifest struct {
	Format        string                  `json:"format"`
	CLIVersion    string                  `json:"cli_version"`
	CapturedAt    time.Time               `json:"captured_at"`
	Inventory     local.TransferInventory `json:"inventory"`
	Git           transferGit             `json:"git"`
	Note          string                  `json:"note,omitempty"`
	PayloadSize   int64                   `json:"payload_size"`
	PayloadSHA256 string                  `json:"payload_sha256"`
}

type localTransferPackage struct {
	Manifest    localTransferManifest `json:"manifest"`
	SnapshotSHA string                `json:"snapshot_sha256"`
}

type localTransferReceipt struct {
	localTransferPackage
	ReceivingGeneration string `json:"receiving_generation"`
}

func (m localTransferManifest) validate() error {
	if m.Format != transferFormat {
		if m.Format == "" {
			return fmt.Errorf("transfer manifest is missing its format")
		}
		return fmt.Errorf("%w: unsupported Local transfer archive format", local.ErrStoreIncompatible)
	}
	inv, bookmark := m.Inventory, m.Inventory.Bookmark
	switch inv.StoreVersion {
	case "", "1":
		if inv.Generation != "" {
			return fmt.Errorf("%w: unexpected source checkout generation", local.ErrStoreIncompatible)
		}
	case "2":
		if !validLowerHex(inv.Generation, 16) {
			return fmt.Errorf("%w: invalid source checkout generation", local.ErrStoreIncompatible)
		}
	default:
		return fmt.Errorf("%w: unsupported source Local store version", local.ErrStoreIncompatible)
	}
	if m.CLIVersion == "" || m.CapturedAt.IsZero() || !validLowerHex(inv.StoreID, 16) ||
		inv.WorkspaceCount <= 0 || inv.WorkspaceCount != len(inv.Workspaces) ||
		inv.UserCount <= 0 || inv.UserCount != len(inv.Users) || inv.WorkCount <= 0 ||
		bookmark.WorkID == "" || bookmark.WorkKey == "" || bookmark.ContextDigest == "" ||
		bookmark.Workspace.ID == "" || bookmark.Workspace.Slug == "" ||
		!m.Git.valid() {
		return fmt.Errorf("transfer manifest is missing valid inventory, pickup identities or Git provenance")
	}
	if m.PayloadSize > transferPayloadMax {
		return fmt.Errorf("%w: state.db is limited to 512 MiB", errTransferTooLarge)
	}
	if m.PayloadSize <= 0 || !validLowerHex(m.PayloadSHA256, sha256.Size) {
		return fmt.Errorf("transfer manifest is missing valid SQLite size or SHA-256")
	}
	return nil
}

func validLowerHex(value string, size int) bool {
	if len(value) != size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func openTransferInput(path string) (*os.File, error) {
	if err := rejectSymlinkedRemovalAncestors(path); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("transfer input must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		f.Close()
		return nil, fmt.Errorf("transfer input changed while opening it")
	}
	return f, nil
}

func requireNewTransferPath(path string) error {
	switch strings.ToLower(filepath.Base(path)) {
	case "state.db", "state.db-wal", "state.db-shm", "state.db-journal", "state.db.pre-enhanced.bak", transferReceiptName:
		return fmt.Errorf("%w: transfer destinations cannot use reserved Local state filenames", errTransferConflict)
	}
	if err := rejectSymlinkedRemovalAncestors(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("%w: use a new path for %q", os.ErrExist, path)
		}
		return err
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !parent.IsDir() {
		return fmt.Errorf("transfer destination parent must be an existing directory")
	}
	return nil
}

// Read, hash, and archive the same private bytes. Limits apply even if a source
// file grows while being read or a ZIP entry expands beyond its declaration.
func copyTransferBytes(ctx context.Context, dst io.Writer, src io.Reader, max int64, label string) (int64, string, error) {
	hash := sha256.New()
	bounded := io.LimitReader(transferContextReader{ctx, src}, max+1)
	n, err := io.Copy(io.MultiWriter(dst, hash), bounded)
	if err != nil {
		return 0, "", err
	}
	if n > max {
		return 0, "", fmt.Errorf("%w: %s is limited to %d MiB", errTransferTooLarge, label, max>>20)
	}
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(hash.Sum(nil)), nil
}

type transferContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r transferContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func writeTransferPackage(ctx context.Context, dbPath, zipPath string, manifest localTransferManifest) (result localTransferPackage, err error) {
	db, err := openTransferInput(dbPath)
	if err != nil {
		return result, err
	}
	defer db.Close()
	out, err := os.OpenFile(zipPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	hash := sha256.New()
	writer := zip.NewWriter(io.MultiWriter(out, hash))
	entry, err := writer.Create("state.db")
	if err != nil {
		return result, err
	}
	manifest.PayloadSize, manifest.PayloadSHA256, err = copyTransferBytes(ctx, entry, db, transferPayloadMax, "state.db")
	if err != nil {
		return result, err
	}
	if err := manifest.validate(); err != nil {
		return result, err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return result, err
	}
	if len(data) > transferManifestMax {
		return result, fmt.Errorf("%w: manifest.json is limited to 1 MiB", errTransferTooLarge)
	}
	entry, err = writer.Create("manifest.json")
	if err != nil {
		return result, err
	}
	if _, err := entry.Write(data); err != nil {
		return result, err
	}
	if err := writer.Close(); err != nil {
		return result, err
	}
	if err := out.Sync(); err != nil {
		return result, err
	}
	return localTransferPackage{Manifest: manifest, SnapshotSHA: hex.EncodeToString(hash.Sum(nil))}, nil
}

func readTransferPackage(ctx context.Context, inputPath, scratch, expectedSHA string) (localTransferPackage, error) {
	var result localTransferPackage
	input, err := openTransferInput(inputPath)
	if err != nil {
		return result, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return result, err
	}
	if info.Size() > transferArchiveMax {
		return result, fmt.Errorf("%w: archive is limited to 520 MiB", errTransferTooLarge)
	}
	// A private spool binds archive parsing to the same bytes that were hashed,
	// even when the original file can be modified during or after verification.
	spool, err := os.OpenFile(filepath.Join(scratch, "archive.zip"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return result, err
	}
	defer spool.Close()
	n, digest, err := copyTransferBytes(ctx, spool, input, transferArchiveMax, "archive")
	if err != nil {
		return result, err
	}
	if expectedSHA != "" && digest != expectedSHA {
		return result, fmt.Errorf("%w: archive SHA-256 differs from --expect-snapshot", errTransferConflict)
	}
	result.SnapshotSHA = digest
	entries, err := readTransferZIP(spool, n)
	if err != nil {
		return result, err
	}
	var body bytes.Buffer
	manifest := entries["manifest.json"]
	read, _, err := readTransferEntry(ctx, manifest, &body, transferManifestMax)
	if err != nil {
		return result, err
	}
	if read != int64(manifest.UncompressedSize64) {
		return result, fmt.Errorf("manifest.json size does not match its ZIP entry")
	}
	if err := decodeTransferJSON(body.Bytes(), &result.Manifest); err != nil {
		return result, fmt.Errorf("invalid transfer manifest: %w", err)
	}
	if err := result.Manifest.validate(); err != nil {
		return result, err
	}
	payload := entries["state.db"]
	if payload.UncompressedSize64 != uint64(result.Manifest.PayloadSize) {
		return result, fmt.Errorf("state.db size differs from the manifest")
	}
	dbPath := filepath.Join(scratch, "state.db")
	if err := extractTransferDB(ctx, payload, dbPath, result.Manifest); err != nil {
		return result, err
	}
	book := result.Manifest.Inventory.Bookmark
	actual, err := local.ValidateTransferSnapshot(ctx, dbPath, book.Workspace.ID, book.WorkID, book.CheckpointID)
	if err != nil {
		return result, fmt.Errorf("validate read-only SQLite snapshot: %w", err)
	}
	if !reflect.DeepEqual(actual, result.Manifest.Inventory) {
		return result, fmt.Errorf("manifest inventory or pickup identities do not match the SQLite payload")
	}
	return result, nil
}

// The fixed v1 protocol uses a comment-free, single-disk, two-entry ZIP.
// Check the directory byte count before archive/zip can allocate entries.
func readTransferZIP(archive *os.File, size int64) (map[string]*zip.File, error) {
	const eocdLength = 22
	var footer [eocdLength]byte
	if size < eocdLength {
		return nil, fmt.Errorf("invalid transfer ZIP")
	}
	if _, err := archive.ReadAt(footer[:], size-eocdLength); err != nil {
		return nil, err
	}
	dirSize := int64(binary.LittleEndian.Uint32(footer[12:16]))
	dirStart := int64(binary.LittleEndian.Uint32(footer[16:20]))
	if binary.LittleEndian.Uint32(footer[:4]) != 0x06054b50 ||
		binary.LittleEndian.Uint32(footer[4:8]) != 0 ||
		binary.LittleEndian.Uint16(footer[8:10]) != 2 || binary.LittleEndian.Uint16(footer[10:12]) != 2 ||
		binary.LittleEndian.Uint16(footer[20:22]) != 0 || dirSize > transferDirectoryMax ||
		dirStart+dirSize != size-eocdLength {
		return nil, fmt.Errorf("transfer ZIP must contain exactly manifest.json and state.db in a bounded directory")
	}
	reader, err := zip.NewReader(archive, size)
	if err != nil {
		return nil, fmt.Errorf("invalid transfer ZIP: %w", err)
	}
	if len(reader.File) != 2 {
		return nil, fmt.Errorf("transfer ZIP must contain exactly two entries")
	}
	entries := make(map[string]*zip.File, 2)
	for _, entry := range reader.File {
		var max uint64
		switch entry.Name {
		case "manifest.json":
			max = transferManifestMax
		case "state.db":
			max = transferPayloadMax
		default:
			return nil, fmt.Errorf("unexpected transfer ZIP entry; only manifest.json and state.db are allowed")
		}
		if entries[entry.Name] != nil || !entry.Mode().IsRegular() || entry.Flags&1 != 0 {
			return nil, fmt.Errorf("transfer ZIP entries must be unique, regular and unencrypted")
		}
		if entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > max {
			return nil, fmt.Errorf("%w: %s must be nonempty and at most %d MiB", errTransferTooLarge, entry.Name, max>>20)
		}
		start, err := entry.DataOffset()
		if err != nil || start < 0 || start >= dirStart || entry.CompressedSize64 > uint64(dirStart-start) {
			return nil, fmt.Errorf("transfer ZIP entry is outside the bounded payload area")
		}
		entries[entry.Name] = entry
	}
	return entries, nil
}

func readTransferEntry(ctx context.Context, entry *zip.File, target io.Writer, max int64) (int64, string, error) {
	r, err := entry.Open()
	if err != nil {
		return 0, "", err
	}
	defer r.Close()
	return copyTransferBytes(ctx, target, r, max, entry.Name)
}

func extractTransferDB(ctx context.Context, entry *zip.File, path string, manifest localTransferManifest) error {
	db, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	size, sha, err := readTransferEntry(ctx, entry, db, manifest.PayloadSize)
	if err := errors.Join(err, db.Close()); err != nil {
		return err
	}
	if size != manifest.PayloadSize || sha != manifest.PayloadSHA256 {
		return fmt.Errorf("SQLite payload size or SHA-256 differs from the manifest")
	}
	return nil
}

func decodeTransferJSON(data []byte, dest any) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON object")
	}
	return nil
}

func transferDestinationAliases(input, output string) bool {
	inDir, inErr := os.Stat(filepath.Dir(input))
	outDir, outErr := os.Stat(filepath.Dir(output))
	return strings.EqualFold(filepath.Base(input), filepath.Base(output)) &&
		inErr == nil && outErr == nil && os.SameFile(inDir, outDir)
}
