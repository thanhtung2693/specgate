package local

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
)

const (
	transferStoreVersion          = "2"
	checkoutGenerationMetadataKey = "checkout_generation"
	enhancedWriterFunction        = "specgate_enhanced_writer"
	transferWriterFunction        = "specgate_transfer_writer"
)

// CheckoutGeneration returns the receiving generation, or an empty string
// for a store that has never been received.
func (s *Store) CheckoutGeneration(ctx context.Context) (string, error) {
	_, generation, err := enhancedStoreFormat(ctx, s.db)
	return generation, err
}

// InitializeTransferGeneration rotates the checkout generation on a private
// staged receive store before publication. Historical rows retain their IDs
// and original observations. Close the private store before publishing its
// database file so SQLite can checkpoint the committed WAL.
func (s *Store) InitializeTransferGeneration(ctx context.Context) (string, error) {
	generation, err := newID()
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	version, _, err := enhancedStoreFormat(ctx, tx)
	if err != nil {
		return "", err
	}
	if err := installStoreWriterGuard(ctx, tx, transferWriterFunction, version != ""); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key, value) VALUES (?, ?), (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		enhancedStoreMetadataKey, transferStoreVersion, checkoutGenerationMetadataKey, generation); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return generation, nil
}

func enhancedStoreFormat(ctx context.Context, q verificationQuerier) (version, generation string, err error) {
	var marker, generationID sql.NullString
	err = q.QueryRowContext(ctx, `SELECT (SELECT value FROM metadata WHERE key=?),
		(SELECT value FROM metadata WHERE key=?)`, enhancedStoreMetadataKey, checkoutGenerationMetadataKey).Scan(&marker, &generationID)
	if err != nil {
		return "", "", err
	}
	if !marker.Valid || marker.String == enhancedStoreVersion {
		if generationID.Valid {
			return "", "", fmt.Errorf("%w: checkout generation requires a receiving store version", ErrStoreIncompatible)
		}
		return marker.String, "", nil
	}
	if marker.String != transferStoreVersion {
		return "", "", fmt.Errorf("%w: enhanced Local store version %q", ErrStoreIncompatible, marker.String)
	}
	decoded, decodeErr := hex.DecodeString(generationID.String)
	if decodeErr != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != generationID.String {
		return "", "", fmt.Errorf("%w: invalid receiving checkout generation", ErrStoreIncompatible)
	}
	return marker.String, generationID.String, nil
}
