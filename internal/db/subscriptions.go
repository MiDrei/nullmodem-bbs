package db

import (
	"database/sql"
	"fmt"
	"time"
)

// migrateAreaSelections turns the old QWK area selections (the areas
// a caller picked; none meant all) into area_unsubscribed (the areas
// they left out) once -- so areas added later reach them too.
func migrateAreaSelections(db *sql.DB) error {
	res, err := db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('area_selections_migrated', ?)`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("db: area selections: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db: area selections: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO area_unsubscribed (user_id, area_id)
		SELECT u.user_id, a.id FROM (SELECT DISTINCT user_id FROM qwk_area_selections) u
		JOIN message_areas a ON a.pending = 0
		WHERE NOT EXISTS (SELECT 1 FROM qwk_area_selections q WHERE q.user_id = u.user_id AND q.area_id = a.id)`); err != nil {
		return fmt.Errorf("db: area selections: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM qwk_area_selections`); err != nil {
		return fmt.Errorf("db: area selections: %w", err)
	}
	return tx.Commit()
}
