package settings

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Repository is the interface the service uses to persist settings.
type Repository interface {
	GetAll(ctx context.Context) ([]Setting, error)
	PutBatch(ctx context.Context, items []Setting) error
}

// Service manages settings with an in-memory cache, encryption for sensitive
// values, and a background refresh goroutine.
type Service struct {
	repo   Repository
	crypto *Crypto
	log    *slog.Logger

	mu    sync.RWMutex
	cache map[string]string
	ttl   time.Duration

	cancel context.CancelFunc
}

// NewService creates a settings service with a 30s cache TTL.
func NewService(repo Repository, crypto *Crypto) (*Service, error) {
	return NewServiceWithTTL(repo, crypto, 30*time.Second)
}

// NewServiceWithTTL creates a service with a custom TTL (for testing).
func NewServiceWithTTL(repo Repository, crypto *Crypto, ttl time.Duration) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		repo:   repo,
		crypto: crypto,
		log:    slog.Default().With("pkg", "settings"),
		cache:  make(map[string]string),
		ttl:    ttl,
		cancel: cancel,
	}
	if err := s.refresh(); err != nil {
		for k, v := range Defaults {
			s.cache[k] = v
		}
		go s.backgroundRefresh(ctx)
		return s, fmt.Errorf("settings: initial load failed (using defaults): %w", err)
	}
	go s.backgroundRefresh(ctx)
	return s, nil
}

func (s *Service) Stop() {
	s.cancel()
}

func (s *Service) backgroundRefresh(ctx context.Context) {
	ticker := time.NewTicker(s.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.refresh(); err != nil {
				s.log.Warn("background refresh failed", "err", err)
			}
		}
	}
}

func (s *Service) refresh() error {
	rows, err := s.repo.GetAll(context.Background())
	if err != nil {
		return err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		if !IsValidKey(r.Key) {
			continue
		}
		val := r.Value
		if r.Encrypted && s.crypto != nil {
			dec, err := s.crypto.Decrypt(val)
			if err != nil {
				m[r.Key] = ""
				continue
			}
			val = dec
		}
		m[r.Key] = val
	}
	for _, k := range AllKeys {
		if _, ok := m[k]; !ok {
			m[k] = Defaults[k]
		}
	}

	s.mu.Lock()
	s.cache = m
	s.mu.Unlock()
	return nil
}

func (s *Service) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.cache[key]; ok {
		return v
	}
	return Defaults[key]
}

// GetBool returns the setting as a bool (false if unparseable).
func (s *Service) GetBool(key string) bool {
	v, _ := strconv.ParseBool(s.Get(key))
	return v
}

// GetInt returns the setting as an int, falling back to def if unparseable.
func (s *Service) GetInt(key string, def int) int {
	n, err := strconv.Atoi(s.Get(key))
	if err != nil {
		return def
	}
	return n
}

// GetAll returns all settings with sensitive values masked.
func (s *Service) GetAll() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		if IsSensitive(k) && v != "" {
			out[k] = MaskedValue
		} else {
			out[k] = v
		}
	}
	return out
}

// GetAllUnmasked returns decrypted values for every key (including secrets).
// Call only from the trusted internal governance service.
func (s *Service) GetAllUnmasked() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// Update validates keys and values, encrypts sensitive values, and persists to the DB.
func (s *Service) Update(values map[string]string) error {
	items := make([]Setting, 0, len(values))
	for k, v := range values {
		if !IsValidKey(k) {
			return fmt.Errorf("settings: unknown key %q", k)
		}
		if err := validateValue(k, v); err != nil {
			return fmt.Errorf("settings: invalid value for %q: %w", k, err)
		}
		if IsSensitive(k) && v == MaskedValue {
			continue
		}
		encrypted := false
		storeVal := v
		if IsSensitive(k) && v != "" {
			if s.crypto == nil {
				return fmt.Errorf("settings: cannot store sensitive key %q without encryption key", k)
			}
			enc, err := s.crypto.Encrypt(v)
			if err != nil {
				return fmt.Errorf("settings: encrypt %q: %w", k, err)
			}
			storeVal = enc
			encrypted = true
		}
		items = append(items, Setting{
			Key:       k,
			Value:     storeVal,
			Encrypted: encrypted,
		})
	}
	if len(items) == 0 {
		return nil
	}
	if err := s.repo.PutBatch(context.Background(), items); err != nil {
		return err
	}
	return s.refresh()
}
