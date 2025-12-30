package export

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"strings"
	"time"

	"github.com/levineuwirth/gophermark/internal/models"
)

type BookmarkExport struct {
	Title     string           `json:"title"`
	URL       string           `json:"url,omitempty"`
	Type      string           `json:"type"`
	Children  []BookmarkExport `json:"children,omitempty"`
	DateAdded string           `json:"dateAdded,omitempty"`
}

func ExportJSON(root *models.Bookmark, outputPath string) error {
	exported := convertToExport(root)

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(exported); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

func ExportHTML(root *models.Bookmark, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	fmt.Fprintf(file, `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<!-- This is an automatically generated file.
     It will be read and overwritten.
     DO NOT EDIT! -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
`)

	writeHTMLBookmarks(file, root, 1)

	fmt.Fprintf(file, "</DL><p>\n")

	return nil
}

func convertToExport(b *models.Bookmark) BookmarkExport {
	export := BookmarkExport{
		Title:     b.Title,
		URL:       b.URL,
		DateAdded: b.DateAdded.Format(time.RFC3339),
	}

	if b.IsFolder() {
		export.Type = "folder"
		export.Children = make([]BookmarkExport, 0, len(b.Children))
		for _, child := range b.Children {
			export.Children = append(export.Children, convertToExport(child))
		}
	} else {
		export.Type = "bookmark"
	}

	return export
}

func writeHTMLBookmarks(file *os.File, b *models.Bookmark, depth int) {
	indent := strings.Repeat("    ", depth)

	if b.IsFolder() {
		if b.Title != "" {
			addDate := b.DateAdded.Unix()
			fmt.Fprintf(file, "%s<DT><H3 ADD_DATE=\"%d\">%s</H3>\n", indent, addDate, html.EscapeString(b.Title))
			fmt.Fprintf(file, "%s<DL><p>\n", indent)
		}

		for _, child := range b.Children {
			writeHTMLBookmarks(file, child, depth+1)
		}

		if b.Title != "" {
			fmt.Fprintf(file, "%s</DL><p>\n", indent)
		}
	} else {
		addDate := b.DateAdded.Unix()
		fmt.Fprintf(file, "%s<DT><A HREF=\"%s\" ADD_DATE=\"%d\">%s</A>\n",
			indent,
			html.EscapeString(b.URL),
			addDate,
			html.EscapeString(b.Title))
	}
}
