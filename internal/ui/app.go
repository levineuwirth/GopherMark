package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/levineuwirth/gophermark/internal/export"
	"github.com/levineuwirth/gophermark/internal/models"
	"github.com/levineuwirth/gophermark/internal/staging"
)

type Pane int

const (
	TreePane Pane = iota
	ListPane
)

type EditMode int

const (
	EditNone EditMode = iota
	EditTitle
	EditURL
	AddTitle
	AddURL
	SearchMode
	ExportMode
)

type Model struct {
	root          *models.Bookmark
	treeNodes     []*TreeNode
	currentFolder *models.Bookmark
	bookmarks     []*models.Bookmark

	expandedFolders map[int64]bool

	selectedBookmarks map[int64]bool

	activePane Pane
	treeCursor int
	listCursor int
	width      int
	height     int
	ready      bool
	err        error

	editMode      EditMode
	titleInput    textinput.Model
	urlInput      textinput.Model
	searchInput   textinput.Model
	statusMessage string

	searchResults []*models.Bookmark
	inSearchMode  bool

	dbPath            string
	stagingDB         *staging.StagingDB
	hasPendingChanges bool
}

func NewModel(root *models.Bookmark, folders []*models.Bookmark, dbPath string) *Model {
	expandedFolders := make(map[int64]bool)

	bookmarksBar := FindBookmarksBar(root)
	var currentFolder *models.Bookmark
	if bookmarksBar != nil {
		ExpandPath(root, bookmarksBar, expandedFolders)
		currentFolder = bookmarksBar
	} else if len(folders) > 0 {
		currentFolder = folders[0]
	}

	treeNodes := BuildFlatTree(root, expandedFolders)

	treeCursor := 0
	if bookmarksBar != nil {
		idx := FindNodeIndex(treeNodes, bookmarksBar.ID)
		if idx >= 0 {
			treeCursor = idx
		}
	}

	titleInput := textinput.New()
	titleInput.Placeholder = "Bookmark title"
	titleInput.CharLimit = 256

	urlInput := textinput.New()
	urlInput.Placeholder = "https://example.com"
	urlInput.CharLimit = 2048

	searchInput := textinput.New()
	searchInput.Placeholder = "Search bookmarks..."
	searchInput.CharLimit = 256

	return &Model{
		root:              root,
		treeNodes:         treeNodes,
		currentFolder:     currentFolder,
		bookmarks:         getBookmarksForFolder(currentFolder),
		expandedFolders:   expandedFolders,
		selectedBookmarks: make(map[int64]bool),
		activePane:        TreePane,
		treeCursor:        treeCursor,
		listCursor:        0,
		dbPath:            dbPath,
		titleInput:        titleInput,
		urlInput:          urlInput,
		searchInput:       searchInput,
		editMode:          EditNone,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.editMode == EditTitle {
		var cmd tea.Cmd
		m.titleInput, cmd = m.titleInput.Update(msg)

		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				return m.saveTitle(), nil
			case "esc":
				m.editMode = EditNone
				m.statusMessage = ""
				return m, nil
			}
		}
		return m, cmd
	}

	if m.editMode == EditURL {
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)

		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				return m.saveURL(), nil
			case "esc":
				m.editMode = EditNone
				m.statusMessage = ""
				return m, nil
			}
		}
		return m, cmd
	}

	if m.editMode == AddTitle {
		var cmd tea.Cmd
		m.titleInput, cmd = m.titleInput.Update(msg)

		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				return m.saveNewTitle(), nil
			case "esc":
				m.editMode = EditNone
				m.statusMessage = ""
				return m, nil
			}
		}
		return m, cmd
	}

	if m.editMode == AddURL {
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)

		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				return m.saveNewBookmark(), nil
			case "esc":
				m.editMode = EditNone
				m.statusMessage = ""
				return m, nil
			}
		}
		return m, cmd
	}

	if m.editMode == SearchMode {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)

		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter", "esc":
				m.exitSearchMode()
				return m, nil
			default:
				query := m.searchInput.Value()
				if query == "" {
					m.searchResults = nil
					m.inSearchMode = false
				} else {
					m.searchResults = SearchBookmarks(m.root, query)
					m.inSearchMode = true
				}
				m.listCursor = 0
			}
		}
		return m, cmd
	}

	if m.editMode == ExportMode {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "j":
				m.exportJSON()
				return m, nil
			case "h":
				m.exportHTML()
				return m, nil
			case "esc":
				m.editMode = EditNone
				m.statusMessage = ""
				return m, nil
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "Q":
			if m.stagingDB != nil {
				m.stagingDB.Close()
			}
			return m, tea.Quit

		case "q":
			if m.hasPendingChanges {
				m.statusMessage = "⚠ Unsaved changes! Press Ctrl+S to commit or Q (uppercase) to quit without saving"
				return m, nil
			}
			if m.stagingDB != nil {
				m.stagingDB.Close()
			}
			return m, tea.Quit

		case "tab":
			m.togglePane()
			return m, nil

		case "j", "down":
			m.cursorDown()
			return m, nil

		case "k", "up":
			m.cursorUp()
			return m, nil

		case "enter", " ":
			if m.activePane == TreePane {
				m.toggleOrSelectFolder()
			}
			return m, nil

		case "e":
			if m.activePane == ListPane && len(m.bookmarks) > 0 {
				m.enterEditMode()
			}
			return m, nil

		case "n":
			if m.activePane == ListPane && m.currentFolder != nil {
				m.enterAddMode()
			}
			return m, nil

		case "m":
			if m.activePane == ListPane && len(m.bookmarks) > 0 {
				m.toggleSelection()
			}
			return m, nil

		case "d":
			if m.activePane == ListPane && len(m.selectedBookmarks) > 0 {
				m.deleteSelected()
			}
			return m, nil

		case "/":
			m.enterSearchMode()
			return m, nil

		case "x":
			m.enterExportMode()
			return m, nil

		case "ctrl+s":
			if m.hasPendingChanges {
				return m.commitChanges(), nil
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	paneWidth := m.width/2 - 4
	paneHeight := m.height - 8

	treeContent := m.renderTree(paneHeight)
	treePane := m.stylePane(TreePane, treeContent, paneWidth, paneHeight)

	var listPane string
	if m.editMode != EditNone {
		listContent := m.renderEditForm(paneHeight)
		listPane = m.stylePane(ListPane, listContent, paneWidth, paneHeight)
	} else {
		listContent := m.renderList(paneHeight)
		listPane = m.stylePane(ListPane, listContent, paneWidth, paneHeight)
	}

	mainView := lipgloss.JoinHorizontal(lipgloss.Top, treePane, listPane)

	title := titleStyle.Render("GopherMark - Firefox/LibreWolf Bookmark Manager")

	help := "j/k: navigate | Space: expand/collapse | Tab: switch | /: search | n: new | e: edit | m: mark | x: export | "
	if len(m.selectedBookmarks) > 0 {
		help += fmt.Sprintf("d: delete (%d) | ", len(m.selectedBookmarks))
	}
	if m.hasPendingChanges {
		help += "Ctrl+S: commit | "
	}
	help += "q: quit"

	helpText := helpStyle.Render(help)

	statusText := ""
	if m.statusMessage != "" {
		statusStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
		statusText = statusStyle.Render(m.statusMessage)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		mainView,
		statusText,
		helpText,
	)
}

func (m *Model) renderEditForm(maxHeight int) string {
	var lines []string

	if m.editMode == ExportMode {
		lines = append(lines, folderStyle.Render("📤 Export Bookmarks"))
		lines = append(lines, "")
		lines = append(lines, "Choose export format:")
		lines = append(lines, "")
		lines = append(lines, normalItemStyle.Render("  j - Export to JSON"))
		lines = append(lines, normalItemStyle.Render("  h - Export to HTML (Netscape format)"))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("Esc: cancel"))

		return strings.Join(lines, "\n")
	}

	if m.editMode == SearchMode {
		lines = append(lines, folderStyle.Render("🔍 Search Bookmarks"))
		lines = append(lines, "")
		lines = append(lines, m.searchInput.View())
		lines = append(lines, "")

		if m.inSearchMode {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("Found %d results", len(m.searchResults))))
		} else {
			lines = append(lines, dimStyle.Render("Type to search..."))
		}
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("Enter/Esc: exit search"))

		return strings.Join(lines, "\n")
	}

	if m.editMode == AddTitle || m.editMode == AddURL {
		lines = append(lines, folderStyle.Render("➕ Add New Bookmark"))
		lines = append(lines, "")
		if m.currentFolder != nil {
			lines = append(lines, dimStyle.Render("Folder: "+m.currentFolder.Title))
			lines = append(lines, "")
		}

		lines = append(lines, "Title:")
		lines = append(lines, m.titleInput.View())
		lines = append(lines, "")

		lines = append(lines, "URL:")
		lines = append(lines, m.urlInput.View())
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("Enter: save | Esc: cancel"))

		return strings.Join(lines, "\n")
	}

	if m.listCursor >= len(m.bookmarks) {
		return "No bookmark selected"
	}

	bookmark := m.bookmarks[m.listCursor]

	lines = append(lines, folderStyle.Render("✏ Edit Bookmark"))
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("ID: "+fmt.Sprintf("%d", bookmark.ID)))
	lines = append(lines, "")

	lines = append(lines, "Title:")
	lines = append(lines, m.titleInput.View())
	lines = append(lines, "")

	lines = append(lines, "URL:")
	lines = append(lines, m.urlInput.View())
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("Enter: save | Esc: cancel"))

	return strings.Join(lines, "\n")
}

func (m *Model) renderTree(maxHeight int) string {
	var lines []string
	lines = append(lines, folderStyle.Render("📁 Folder Tree"))
	lines = append(lines, "")

	for i, node := range m.treeNodes {
		indent := strings.Repeat("  ", node.Depth)

		indicator := "  "
		if node.HasKids {
			if node.Expanded {
				indicator = "▼ "
			} else {
				indicator = "▶ "
			}
		}

		prefix := " "
		if i == m.treeCursor && m.activePane == TreePane {
			prefix = "❯"
		}

		style := normalItemStyle
		if i == m.treeCursor && m.activePane == TreePane {
			style = selectedItemStyle
		}

		titleStyle := style
		if node.Folder == m.currentFolder {
			titleStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				PaddingLeft(1)
		}

		title := node.Folder.Title
		maxLen := 35 - (node.Depth * 2)
		if len(title) > maxLen {
			title = title[:maxLen-3] + "..."
		}

		line := prefix + indent + indicator + title
		lines = append(lines, titleStyle.Render(line))
	}

	if len(m.treeNodes) == 0 {
		lines = append(lines, dimStyle.Render("  (no folders)"))
	}

	// Scroll window to keep cursor visible
	if len(lines) > maxHeight {
		start := 0
		if m.treeCursor > maxHeight/2 {
			start = m.treeCursor - maxHeight/2
		}
		end := start + maxHeight
		if end > len(lines) {
			end = len(lines)
			start = end - maxHeight
			if start < 0 {
				start = 0
			}
		}
		lines = lines[start:end]
	}

	return strings.Join(lines, "\n")
}

func (m *Model) renderList(maxHeight int) string {
	var lines []string

	var displayBookmarks []*models.Bookmark
	var headerTitle string

	if m.inSearchMode && len(m.searchResults) > 0 {
		displayBookmarks = m.searchResults
		headerTitle = fmt.Sprintf("🔍 Search Results (%d)", len(m.searchResults))
	} else if m.currentFolder != nil {
		displayBookmarks = m.bookmarks
		headerTitle = "📄 " + m.currentFolder.Title
		if m.hasPendingChanges {
			headerTitle += " [modified]"
		}
	} else {
		displayBookmarks = m.bookmarks
		headerTitle = "📄 Bookmarks"
	}

	lines = append(lines, folderStyle.Render(headerTitle))
	lines = append(lines, "")

	if len(displayBookmarks) == 0 {
		if m.inSearchMode {
			lines = append(lines, dimStyle.Render("  (no results)"))
		} else {
			lines = append(lines, dimStyle.Render("  (no bookmarks)"))
		}
	} else {
		for i, bookmark := range displayBookmarks {
			selectMark := " "
			if m.selectedBookmarks[bookmark.ID] {
				selectMark = "✓"
			}

			prefix := " " + selectMark + " "
			if i == m.listCursor && m.activePane == ListPane {
				prefix = "❯" + selectMark + " "
			}

			style := normalItemStyle
			if i == m.listCursor && m.activePane == ListPane {
				style = selectedItemStyle
			}

			title := bookmark.Title
			if title == "" {
				title = "(untitled)"
			}
			if len(title) > 38 {
				title = title[:35] + "..."
			}

			lines = append(lines, style.Render(prefix+title))
		}
	}

	// Scroll window
	if len(lines) > maxHeight {
		start := 0
		if m.listCursor > maxHeight/2 {
			start = m.listCursor - maxHeight/2
		}
		end := start + maxHeight
		if end > len(lines) {
			end = len(lines)
			start = end - maxHeight
			if start < 0 {
				start = 0
			}
		}
		lines = lines[start:end]
	}

	return strings.Join(lines, "\n")
}

func (m *Model) stylePane(pane Pane, content string, width, height int) string {
	style := paneStyle
	if pane == m.activePane {
		style = activePaneStyle
	}
	return style.Width(width).Height(height).Render(content)
}

func (m *Model) togglePane() {
	if m.activePane == TreePane {
		m.activePane = ListPane
	} else {
		m.activePane = TreePane
	}
}

func (m *Model) cursorDown() {
	if m.activePane == TreePane {
		if m.treeCursor < len(m.treeNodes)-1 {
			m.treeCursor++
		}
	} else {
		maxItems := len(m.bookmarks)
		if m.inSearchMode {
			maxItems = len(m.searchResults)
		}
		if m.listCursor < maxItems-1 {
			m.listCursor++
		}
	}
}

func (m *Model) cursorUp() {
	if m.activePane == TreePane {
		if m.treeCursor > 0 {
			m.treeCursor--
		}
	} else {
		if m.listCursor > 0 {
			m.listCursor--
		}
	}
}

func (m *Model) toggleOrSelectFolder() {
	if m.treeCursor >= len(m.treeNodes) {
		return
	}

	node := m.treeNodes[m.treeCursor]

	if node.HasKids {
		node.Expanded = !node.Expanded
		m.expandedFolders[node.Folder.ID] = node.Expanded

		m.treeNodes = BuildFlatTree(m.root, m.expandedFolders)

		newIdx := FindNodeIndex(m.treeNodes, node.Folder.ID)
		if newIdx >= 0 {
			m.treeCursor = newIdx
		}
	}

	m.currentFolder = node.Folder
	m.bookmarks = getBookmarksForFolder(m.currentFolder)
	m.listCursor = 0
}

func (m *Model) enterEditMode() {
	if m.listCursor >= len(m.bookmarks) {
		return
	}

	if m.stagingDB == nil {
		var err error
		m.stagingDB, err = staging.CreateStaging(m.dbPath)
		if err != nil {
			m.statusMessage = "Failed to create staging database: " + err.Error()
			return
		}
	}

	bookmark := m.bookmarks[m.listCursor]
	m.titleInput.SetValue(bookmark.Title)
	m.urlInput.SetValue(bookmark.URL)

	m.editMode = EditTitle
	m.titleInput.Focus()
	m.statusMessage = "Editing bookmark (changes staged until Ctrl+S)"
}

func (m *Model) enterAddMode() {
	if m.currentFolder == nil {
		return
	}

	if m.stagingDB == nil {
		var err error
		m.stagingDB, err = staging.CreateStaging(m.dbPath)
		if err != nil {
			m.statusMessage = "Failed to create staging database: " + err.Error()
			return
		}
	}

	m.titleInput.SetValue("")
	m.urlInput.SetValue("")

	m.editMode = AddTitle
	m.titleInput.Focus()
	m.statusMessage = "Adding new bookmark to " + m.currentFolder.Title
}

func (m *Model) enterSearchMode() {
	m.searchInput.SetValue("")
	m.searchInput.Focus()
	m.editMode = SearchMode
	m.inSearchMode = false
	m.searchResults = nil
	m.statusMessage = "Search mode: type to find bookmarks"
}

func (m *Model) exitSearchMode() {
	m.editMode = EditNone
	m.searchInput.Blur()
	m.inSearchMode = false
	m.searchResults = nil
	m.statusMessage = ""
}

func (m *Model) saveTitle() *Model {
	if m.listCursor >= len(m.bookmarks) {
		return m
	}

	bookmark := m.bookmarks[m.listCursor]
	newTitle := m.titleInput.Value()

	if newTitle != bookmark.Title {
		err := m.stagingDB.UpdateBookmarkTitle(bookmark.ID, newTitle)
		if err != nil {
			m.statusMessage = "Failed to update title: " + err.Error()
			m.editMode = EditNone
			return m
		}
		bookmark.Title = newTitle
		m.hasPendingChanges = true
	}

	m.editMode = EditURL
	m.urlInput.Focus()
	m.titleInput.Blur()

	return m
}

func (m *Model) saveURL() *Model {
	if m.listCursor >= len(m.bookmarks) {
		return m
	}

	bookmark := m.bookmarks[m.listCursor]
	newURL := m.urlInput.Value()

	if newURL != bookmark.URL && bookmark.FK != nil {
		err := m.stagingDB.UpdateBookmarkURL(*bookmark.FK, newURL)
		if err != nil {
			m.statusMessage = "Failed to update URL: " + err.Error()
			m.editMode = EditNone
			return m
		}
		bookmark.URL = newURL
		m.hasPendingChanges = true
	}

	m.editMode = EditNone
	m.statusMessage = "✓ Changes saved to staging (Ctrl+S to commit)"
	m.titleInput.Blur()
	m.urlInput.Blur()

	return m
}

func (m *Model) commitChanges() *Model {
	if m.stagingDB == nil {
		m.statusMessage = "No changes to commit"
		return m
	}

	err := m.stagingDB.Commit()
	if err != nil {
		m.statusMessage = "⚠ Commit failed: " + err.Error()
		return m
	}

	m.stagingDB = nil
	m.hasPendingChanges = false
	m.statusMessage = "✓ Changes committed successfully!"

	return m
}

func (m *Model) saveNewTitle() *Model {
	m.editMode = AddURL
	m.urlInput.Focus()
	m.titleInput.Blur()
	return m
}

func (m *Model) saveNewBookmark() *Model {
	if m.currentFolder == nil {
		m.statusMessage = "No folder selected"
		m.editMode = EditNone
		return m
	}

	title := m.titleInput.Value()
	url := m.urlInput.Value()

	if title == "" || url == "" {
		m.statusMessage = "Title and URL are required"
		return m
	}

	err := m.stagingDB.AddBookmark(m.currentFolder.ID, title, url)
	if err != nil {
		m.statusMessage = "Failed to add bookmark: " + err.Error()
		m.editMode = EditNone
		return m
	}

	newBookmark := &models.Bookmark{
		Title: title,
		URL:   url,
		Type:  models.TypeBookmark,
	}
	m.bookmarks = append(m.bookmarks, newBookmark)
	m.currentFolder.Children = append(m.currentFolder.Children, newBookmark)

	m.hasPendingChanges = true
	m.editMode = EditNone
	m.statusMessage = "✓ Bookmark added to staging (Ctrl+S to commit)"
	m.titleInput.Blur()
	m.urlInput.Blur()

	m.listCursor = len(m.bookmarks) - 1

	return m
}

func (m *Model) toggleSelection() {
	if m.listCursor >= len(m.bookmarks) {
		return
	}

	bookmark := m.bookmarks[m.listCursor]
	if m.selectedBookmarks[bookmark.ID] {
		delete(m.selectedBookmarks, bookmark.ID)
		m.statusMessage = fmt.Sprintf("Deselected: %s", bookmark.Title)
	} else {
		m.selectedBookmarks[bookmark.ID] = true
		m.statusMessage = fmt.Sprintf("Selected: %s (%d total)", bookmark.Title, len(m.selectedBookmarks))
	}
}

func (m *Model) deleteSelected() {
	if len(m.selectedBookmarks) == 0 {
		return
	}

	if m.stagingDB == nil {
		var err error
		m.stagingDB, err = staging.CreateStaging(m.dbPath)
		if err != nil {
			m.statusMessage = "Failed to create staging database: " + err.Error()
			return
		}
	}

	var deleteErrors []string
	deletedCount := 0
	for bookmarkID := range m.selectedBookmarks {
		err := m.stagingDB.DeleteBookmark(bookmarkID)
		if err != nil {
			deleteErrors = append(deleteErrors, err.Error())
		} else {
			deletedCount++
		}
	}

	var remainingBookmarks []*models.Bookmark
	for _, bookmark := range m.bookmarks {
		if !m.selectedBookmarks[bookmark.ID] || deleteErrors != nil {
			remainingBookmarks = append(remainingBookmarks, bookmark)
		}
	}
	m.bookmarks = remainingBookmarks

	m.selectedBookmarks = make(map[int64]bool)

	if m.listCursor >= len(m.bookmarks) && len(m.bookmarks) > 0 {
		m.listCursor = len(m.bookmarks) - 1
	}

	m.hasPendingChanges = true

	if len(deleteErrors) > 0 {
		m.statusMessage = fmt.Sprintf("⚠ Deleted %d, failed %d (Ctrl+S to commit)", deletedCount, len(deleteErrors))
	} else {
		m.statusMessage = fmt.Sprintf("✓ Deleted %d bookmarks (Ctrl+S to commit)", deletedCount)
	}
}

func (m *Model) enterExportMode() {
	m.editMode = ExportMode
	m.statusMessage = "Export mode: choose format"
}

func (m *Model) exportJSON() {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(".", fmt.Sprintf("bookmarks_%s.json", timestamp))

	err := export.ExportJSON(m.root, filename)
	if err != nil {
		m.statusMessage = "❌ Export failed: " + err.Error()
	} else {
		m.statusMessage = "✓ Exported to " + filename
	}

	m.editMode = EditNone
}

func (m *Model) exportHTML() {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(".", fmt.Sprintf("bookmarks_%s.html", timestamp))

	err := export.ExportHTML(m.root, filename)
	if err != nil {
		m.statusMessage = "❌ Export failed: " + err.Error()
	} else {
		m.statusMessage = "✓ Exported to " + filename
	}

	m.editMode = EditNone
}

func getBookmarksForFolder(folder *models.Bookmark) []*models.Bookmark {
	if folder == nil {
		return nil
	}

	var bookmarks []*models.Bookmark
	for _, child := range folder.Children {
		if child.IsBookmark() {
			bookmarks = append(bookmarks, child)
		}
	}
	return bookmarks
}
