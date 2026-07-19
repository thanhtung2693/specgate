# Search and indexing

Catalog search runs on an inverted index refreshed every fifteen minutes.
Documents include supplier, product, and certification entities.

Indexer pipeline stages:
1. extract — pull updated records from the source database
2. transform — flatten nested attributes into searchable fields
3. publish — write to the index with a version stamp

Stale-read tolerance is set at thirty minutes; beyond that the entity is
considered out of date and clients should hard-refresh.
