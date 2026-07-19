# Repo excerpt: supplier filter handler

File path: `services/supplier/internal/handler/filter.go`

```go
func (h *FilterHandler) Apply(ctx context.Context, q FilterQuery) ([]Supplier, error) {
    if q.Region != "" {
        q = q.WithRegion(q.Region)
    }
    if q.Certification != "" {
        q = q.WithCertification(q.Certification)
    }
    return h.repo.Search(ctx, q)
}
```

The handler applies region and certification as independent predicates; there
is no composite path that combines them with an AND clause across the
underlying index.
