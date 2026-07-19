package seeding

import (
	_ "embed"
)

//go:embed skills_seed.json
var skillsSeedJSON []byte
