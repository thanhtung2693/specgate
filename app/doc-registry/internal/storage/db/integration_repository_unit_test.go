package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func TestIsUniqueViolationUsesTypedErrors(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		err  error
		want bool
	}{
		"gorm duplicate":    {err: gorm.ErrDuplicatedKey, want: true},
		"postgres unique":   {err: &pgconn.PgError{Code: "23505"}, want: true},
		"wrapped postgres":  {err: fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505"}), want: true},
		"postgres not null": {err: &pgconn.PgError{Code: "23502"}, want: false},
		"similar prose":     {err: errors.New("duplicate key"), want: false},
		"nil":               {err: nil, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := isUniqueViolation(testCase.err); got != testCase.want {
				t.Fatalf("isUniqueViolation(%v) = %t, want %t", testCase.err, got, testCase.want)
			}
		})
	}
}
