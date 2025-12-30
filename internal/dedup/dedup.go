package dedup

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/levineuwirth/gophermark/internal/models"
)

var debugLog *log.Logger

func init() {
	f, err := os.OpenFile("/tmp/gophermark-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		debugLog = log.New(f, "", log.Ltime|log.Lmicroseconds|log.Lshortfile)
	}
}

type DuplicateGroup struct {
	URL       string
	Bookmarks []*models.Bookmark
}

func FindDuplicates(db *sql.DB) ([]DuplicateGroup, error) {
	if debugLog != nil {
		debugLog.Println("FindDuplicates: entering function")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if debugLog != nil {
		debugLog.Println("FindDuplicates: context created with 30s timeout")
	}

	query := `
		WITH duplicate_urls AS (
			SELECT p.url, COUNT(b.id) as cnt
			FROM moz_places p
			INNER JOIN moz_bookmarks b ON b.fk = p.id
			WHERE b.fk IS NOT NULL
			GROUP BY p.url
			HAVING cnt > 1
		)
		SELECT
			p.url,
			b.id,
			b.type,
			b.fk,
			b.parent,
			b.position,
			COALESCE(b.title, ''),
			b.dateAdded,
			b.lastModified,
			b.guid,
			COALESCE(p.visit_count, 0)
		FROM moz_places p
		INNER JOIN moz_bookmarks b ON b.fk = p.id
		INNER JOIN duplicate_urls d ON d.url = p.url
		ORDER BY p.url, b.id
	`

	if debugLog != nil {
		debugLog.Println("FindDuplicates: executing query")
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		if debugLog != nil {
			debugLog.Printf("FindDuplicates: query failed: %v", err)
		}
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()
	if debugLog != nil {
		debugLog.Println("FindDuplicates: query executed, processing rows")
	}

	urlMap := make(map[string][]*models.Bookmark)

	for rows.Next() {
		var url string
		var b models.Bookmark
		var fk sql.NullInt64

		err := rows.Scan(
			&url,
			&b.ID,
			&b.Type,
			&fk,
			&b.Parent,
			&b.Position,
			&b.Title,
			&b.DateAdded,
			&b.LastModified,
			&b.GUID,
			&b.VisitCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		if fk.Valid {
			b.FK = &fk.Int64
		}
		b.URL = url

		urlMap[url] = append(urlMap[url], &b)
	}

	if err := rows.Err(); err != nil {
		if debugLog != nil {
			debugLog.Printf("FindDuplicates: rows error: %v", err)
		}
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if debugLog != nil {
		debugLog.Printf("FindDuplicates: finished processing rows, found %d URLs", len(urlMap))
	}

	var groups []DuplicateGroup
	for url, bookmarks := range urlMap {
		if len(bookmarks) > 1 {
			groups = append(groups, DuplicateGroup{
				URL:       url,
				Bookmarks: bookmarks,
			})
		}
	}

	if debugLog != nil {
		debugLog.Printf("FindDuplicates: returning %d duplicate groups", len(groups))
	}

	return groups, nil
}
