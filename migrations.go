package task

import "embed"

// Migrations contains the SQL migration files for axon-task.
// Composition roots pass this to migration.Run from axon-base.
//
//go:embed migrations/*.sql
var Migrations embed.FS
