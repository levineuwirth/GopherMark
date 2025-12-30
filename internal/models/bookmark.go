package models

import "time"

type BookmarkType int

const (
	TypeBookmark  BookmarkType = 1
	TypeFolder    BookmarkType = 2
	TypeSeparator BookmarkType = 3
)

type Bookmark struct {
	ID           int64
	Type         BookmarkType
	FK           *int64 // Foreign key to moz_places, null for folders
	Parent       int64
	Position     int
	Title        string
	DateAdded    time.Time
	LastModified time.Time
	GUID         string

	URL        string
	VisitCount int

	Children []*Bookmark
	Expanded bool
}

func (b *Bookmark) IsFolder() bool {
	return b.Type == TypeFolder
}

func (b *Bookmark) IsBookmark() bool {
	return b.Type == TypeBookmark
}
