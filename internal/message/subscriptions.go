package message

import (
	"fmt"
)

// "My areas": every area a caller may read is in, unless they took it
// out -- the new scan, QWK packets and the reader app then leave it
// out (Telnet K, the portal's area list, the reader). An area added
// later is in. Messages to the caller (tome) are found everywhere.

// UnsubscribedAreaIDs returns the areas userID took out.
func (s *Store) UnsubscribedAreaIDs(userID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT area_id FROM area_unsubscribed WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("message: areas left out by %d: %w", userID, err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("message: areas left out by %d: %w", userID, err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// InMyAreas reports, per area, whether it's one of userID's areas.
func (s *Store) InMyAreas(userID int64) (func(areaID int64) bool, error) {
	out, err := s.UnsubscribedAreaIDs(userID)
	if err != nil {
		return nil, err
	}
	return func(id int64) bool { return !out[id] }, nil
}

// SetAreaSubscribed puts areaID into userID's areas or takes it out.
func (s *Store) SetAreaSubscribed(userID, areaID int64, in bool) error {
	var err error
	if in {
		_, err = s.db.Exec(`DELETE FROM area_unsubscribed WHERE user_id = ? AND area_id = ?`, userID, areaID)
	} else {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO area_unsubscribed (user_id, area_id) VALUES (?, ?)`, userID, areaID)
	}
	if err != nil {
		return fmt.Errorf("message: area %d for %d: %w", areaID, userID, err)
	}
	return nil
}

// SetUnsubscribedAreas replaces the areas userID took out.
func (s *Store) SetUnsubscribedAreas(userID int64, areaIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("message: areas for %d: %w", userID, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM area_unsubscribed WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("message: areas for %d: %w", userID, err)
	}
	for _, id := range areaIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO area_unsubscribed (user_id, area_id) VALUES (?, ?)`, userID, id); err != nil {
			return fmt.Errorf("message: areas for %d: %w", userID, err)
		}
	}
	return tx.Commit()
}

// QWKSelectedAreaIDs is "my areas" seen as a selection: the areas in,
// or an empty map when none was taken out (everything).
func (s *Store) QWKSelectedAreaIDs(userID int64) (map[int64]bool, error) {
	out, err := s.UnsubscribedAreaIDs(userID)
	if err != nil || len(out) == 0 {
		return map[int64]bool{}, err
	}
	all, err := s.AllAreas()
	if err != nil {
		return nil, err
	}
	in := map[int64]bool{}
	for _, a := range all {
		if !out[a.ID] {
			in[a.ID] = true
		}
	}
	return in, nil
}

// SetQWKSelectedAreas sets "my areas" from a selection: exactly
// areaIDs in; none (or nil) means every area.
func (s *Store) SetQWKSelectedAreas(userID int64, areaIDs []int64) error {
	if len(areaIDs) == 0 {
		return s.SetUnsubscribedAreas(userID, nil)
	}
	keep := map[int64]bool{}
	for _, id := range areaIDs {
		keep[id] = true
	}
	all, err := s.AllAreas()
	if err != nil {
		return err
	}
	var out []int64
	for _, a := range all {
		if !keep[a.ID] {
			out = append(out, a.ID)
		}
	}
	return s.SetUnsubscribedAreas(userID, out)
}
