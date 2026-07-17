package archive

import (
	"context"
	"fmt"
	"time"
)

const quotaBlockedMarker = "(daily-limit-reached)"

// QuotaReservationsByEndpoint reconstructs quota reservations from the raw
// fetch ledger. Local quota rejections never reached the official API and are
// excluded; transport and official API failures remain counted conservatively.
func (s *Store) QuotaReservationsByEndpoint(ctx context.Context, start, end time.Time) (map[string]int, error) {
	if !end.After(start) {
		return nil, fmt.Errorf("quota reservation range must have end after start")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT endpoint, COUNT(*)
FROM official_api_fetches
WHERE fetched_at >= ? AND fetched_at < ? AND instr(error, ?) = 0
GROUP BY endpoint`, start.UnixMilli(), end.UnixMilli(), quotaBlockedMarker)
	if err != nil {
		return nil, fmt.Errorf("query quota reservations: %w", err)
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var endpoint string
		var count int
		if err := rows.Scan(&endpoint, &count); err != nil {
			return nil, fmt.Errorf("scan quota reservations: %w", err)
		}
		counts[endpoint] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quota reservations: %w", err)
	}
	return counts, nil
}
