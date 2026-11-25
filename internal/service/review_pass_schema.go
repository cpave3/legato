package service

import (
	"context"
	"log"
)

// schemaKnown says whether the database is at a schema this binary knows.
// A newer Legato that migrated it asked this process to exit (spec LEG-559
// M7); until it does, its tick moves nothing on, since its reads and
// writes would follow tables it does not know. Logged once.
func (s *ReviewPassService) schemaKnown(ctx context.Context) bool {
	status, err := s.db.SchemaStatus(ctx)
	if err == nil && !status.Newer() {
		return true
	}
	s.schemaOnce.Do(func() {
		if err != nil {
			log.Printf("review passes: not ticking: reading the schema: %v", err)
			return
		}
		log.Printf("review passes: not ticking: the database is at schema %d, newer than this Legato knows (%d); restart Legato", status.Current, status.Supported)
	})
	return false
}
