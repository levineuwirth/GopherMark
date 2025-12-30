package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/levineuwirth/gophermark/internal/models"
)

func (db *DB) FetchAllBookmarks() ([]*models.Bookmark, error) {
	query := `
		SELECT
			b.id,
			b.type,
			b.fk,
			b.parent,
			b.position,
			b.title,
			b.dateAdded,
			b.lastModified,
			b.guid,
			COALESCE(p.url, '') as url,
			COALESCE(p.visit_count, 0) as visit_count
		FROM moz_bookmarks b
		LEFT JOIN moz_places p ON b.fk = p.id
		ORDER BY b.parent, b.position
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query bookmarks: %w", err)
	}
	defer rows.Close()

	var bookmarks []*models.Bookmark

	for rows.Next() {
		var b models.Bookmark
		var fk sql.NullInt64
		var title sql.NullString
		var url sql.NullString
		var dateAdded, lastModified int64

		err := rows.Scan(
			&b.ID,
			&b.Type,
			&fk,
			&b.Parent,
			&b.Position,
			&title,
			&dateAdded,
			&lastModified,
			&b.GUID,
			&url,
			&b.VisitCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bookmark: %w", err)
		}

		if fk.Valid {
			b.FK = &fk.Int64
		}
		if title.Valid {
			b.Title = title.String
		}
		if url.Valid {
			b.URL = url.String
		}

		b.DateAdded = time.Unix(0, dateAdded*1000)
		b.LastModified = time.Unix(0, lastModified*1000)

		b.Children = make([]*models.Bookmark, 0)

		bookmarks = append(bookmarks, &b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating bookmarks: %w", err)
	}

	return bookmarks, nil
}

func BuildTree(bookmarks []*models.Bookmark) (*models.Bookmark, error) {
	bookmarkMap := make(map[int64]*models.Bookmark)
	for _, b := range bookmarks {
		bookmarkMap[b.ID] = b
	}

	var root *models.Bookmark
	for _, b := range bookmarks {
		if b.Parent == 0 {
			root = b
			break
		}
	}

	if root == nil {
		return nil, fmt.Errorf("no root bookmark found")
	}

	for _, b := range bookmarks {
		if b.Parent != 0 {
			if parent, exists := bookmarkMap[b.Parent]; exists {
				parent.Children = append(parent.Children, b)
			}
		}
	}

	return root, nil
}

func GetFolders(root *models.Bookmark) []*models.Bookmark {
	var folders []*models.Bookmark

	var traverse func(*models.Bookmark)
	traverse = func(node *models.Bookmark) {
		if node.IsFolder() {
			folders = append(folders, node)
		}
		for _, child := range node.Children {
			traverse(child)
		}
	}

	traverse(root)
	return folders
}

func GetBookmarksInFolder(folder *models.Bookmark) []*models.Bookmark {
	var bookmarks []*models.Bookmark

	for _, child := range folder.Children {
		if child.IsBookmark() {
			bookmarks = append(bookmarks, child)
		}
	}

	return bookmarks
}
