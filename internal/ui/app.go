package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/levineuwirth/gophermark/internal/audit"
	"github.com/levineuwirth/gophermark/internal/db"
	"github.com/levineuwirth/gophermark/internal/dedup"
	"github.com/levineuwirth/gophermark/internal/export"
	"github.com/levineuwirth/gophermark/internal/models"
	"github.com/levineuwirth/gophermark/internal/staging"
)

var debugLog *log.Logger

func init() {
	f, err := os.OpenFile("/tmp/gophermark-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		debugLog = log.New(f, "", log.Ltime|log.Lmicroseconds|log.Lshortfile)
	}
}

type Pane int

const (
	TreePane Pane = iota
	ListPane
	InspectorPane
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
	AuditMode
	DedupMode
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

	showInspector    bool
	auditResults     map[int64]string
	auditInProgress  bool
	auditTotal       int
	auditCompleted   int
	dedupGroups      []string
	dedupSelected    int
	dedupScanning    bool
	scanSpinner      int
	viewCount        int
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
		auditResults:      make(map[int64]string),
		showInspector:     false,
	}
}

type auditProgressMsg struct {
	total     int
	completed int
	result    audit.LinkResult
}

type auditCompleteMsg struct{}

type dedupResultMsg struct {
	groups []dedup.DuplicateGroup
	err    error
}

type dedupTickMsg struct{}

type auditTickMsg struct{}

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

	if m.editMode == AuditMode {
		if _, ok := msg.(tea.KeyMsg); ok {
			if !m.auditInProgress {
				m.editMode = EditNone
				m.statusMessage = ""
				return m, nil
			}
		}
	}

	if m.editMode == DedupMode {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if m.dedupScanning {
				return m, nil
			}

			switch keyMsg.String() {
			case "j", "down":
				if len(m.dedupGroups) > 0 && m.dedupSelected < len(m.dedupGroups)-1 {
					m.dedupSelected++
				}
				return m, nil
			case "k", "up":
				if len(m.dedupGroups) > 0 && m.dedupSelected > 0 {
					m.dedupSelected--
				}
				return m, nil
			default:
				m.editMode = EditNone
				m.statusMessage = ""
				return m, nil
			}
		}
	}

	switch msg := msg.(type) {
	case auditProgressMsg:
		m.auditTotal = msg.total
		m.auditCompleted = msg.completed
		if msg.result.Status == audit.StatusDead || msg.result.Status == audit.StatusTimeout {
			m.auditResults[msg.result.Bookmark.ID] = "DEAD"
		} else {
			m.auditResults[msg.result.Bookmark.ID] = "OK"
		}
		return m, nil

	case auditTickMsg:
		if m.auditInProgress {
			m.scanSpinner = (m.scanSpinner + 1) % 4
			return m, m.tickAudit()
		}
		return m, nil

	case auditCompleteMsg:
		m.auditInProgress = false
		deadCount := 0
		for _, status := range m.auditResults {
			if status == "DEAD" {
				deadCount++
			}
		}
		m.statusMessage = fmt.Sprintf("✓ Audit complete: %d dead links found", deadCount)
		return m, nil

	case dedupTickMsg:
		if debugLog != nil {
			debugLog.Printf("Update: received dedupTickMsg, scanning=%v", m.dedupScanning)
		}
		if m.dedupScanning {
			m.scanSpinner = (m.scanSpinner + 1) % 4
			return m, m.tickDedup()
		}
		return m, nil

	case dedupResultMsg:
		if debugLog != nil {
			debugLog.Printf("Update: received dedupResultMsg with %d groups, err=%v", len(msg.groups), msg.err)
		}
		m.dedupScanning = false
		if msg.err != nil {
			m.statusMessage = "❌ Dedup failed: " + msg.err.Error()
			m.editMode = EditNone
			if debugLog != nil {
				debugLog.Println("Update: dedupResultMsg handling complete (error case)")
			}
			return m, nil
		}

		if debugLog != nil {
			debugLog.Println("Update: building group summaries")
		}
		var groupSummaries []string
		for _, group := range msg.groups {
			groupSummaries = append(groupSummaries, fmt.Sprintf("%s (%d duplicates)", group.URL, len(group.Bookmarks)))
		}
		m.dedupGroups = groupSummaries
		m.dedupSelected = 0

		if len(msg.groups) == 0 {
			m.statusMessage = "✓ No duplicates found"
		} else {
			m.statusMessage = fmt.Sprintf("Found %d duplicate groups", len(msg.groups))
		}
		if debugLog != nil {
			debugLog.Println("Update: dedupResultMsg handling complete (success case)")
		}
		return m, nil

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

		case "i":
			m.toggleInspector()
			return m, nil

		case "a":
			if m.editMode == EditNone {
				return m, m.startAudit()
			}
			return m, nil

		case "D":
			if m.editMode == EditNone {
				if debugLog != nil {
					debugLog.Println("User pressed D key - starting dedup")
				}
				return m, m.startDedup()
			}
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
	m.viewCount++
	if m.viewCount%10 == 0 && debugLog != nil {
		debugLog.Printf("View: called %d times, dedupScanning=%v, editMode=%d", m.viewCount, m.dedupScanning, m.editMode)
	}

	if !m.ready {
		return "Loading..."
	}

	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	numPanes := 2
	if m.showInspector {
		numPanes = 3
	}

	paneWidth := (m.width / numPanes) - 4
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

	var mainView string
	if m.showInspector {
		inspectorContent := m.renderInspector(paneHeight)
		inspectorPane := m.stylePane(InspectorPane, inspectorContent, paneWidth, paneHeight)
		mainView = lipgloss.JoinHorizontal(lipgloss.Top, treePane, listPane, inspectorPane)
	} else {
		mainView = lipgloss.JoinHorizontal(lipgloss.Top, treePane, listPane)
	}

	title := titleStyle.Render("GopherMark - Firefox/LibreWolf Bookmark Manager")

	help := "j/k: nav | Space: toggle | Tab: switch | /: search | n: new | e: edit | m: mark | x: export | i: inspector | a: audit | D: dedup | "
	if len(m.selectedBookmarks) > 0 {
		help += fmt.Sprintf("d: delete (%d) | ", len(m.selectedBookmarks))
	}
	if m.hasPendingChanges {
		help += "Ctrl+S: commit | "
	}
	if m.auditInProgress {
		help += fmt.Sprintf("Audit: %d/%d | ", m.auditCompleted, m.auditTotal)
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

	if m.editMode == AuditMode {
		lines = append(lines, folderStyle.Render("🔍 Link Audit"))
		lines = append(lines, "")
		if m.auditInProgress {
			spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸"}
			spinner := spinnerFrames[m.scanSpinner]
			progress := fmt.Sprintf("%s Progress: %d/%d", spinner, m.auditCompleted, m.auditTotal)
			lines = append(lines, normalItemStyle.Render(progress))
			lines = append(lines, "")
			lines = append(lines, dimStyle.Render("Checking links for broken URLs..."))
		} else {
			lines = append(lines, dimStyle.Render("Audit complete"))
			lines = append(lines, "")
			lines = append(lines, dimStyle.Render("Press any key to close"))
		}
		return strings.Join(lines, "\n")
	}

	if m.editMode == DedupMode {
		lines = append(lines, folderStyle.Render("🔗 Duplicate Detection"))
		lines = append(lines, "")

		if m.dedupScanning {
			spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸"}
			spinner := spinnerFrames[m.scanSpinner]
			lines = append(lines, dimStyle.Render(spinner+" Scanning database for duplicates..."))
			lines = append(lines, "")
			lines = append(lines, dimStyle.Render("This may take a moment for large databases."))
		} else if len(m.dedupGroups) == 0 {
			lines = append(lines, dimStyle.Render("No duplicates found"))
			lines = append(lines, "")
			lines = append(lines, dimStyle.Render("Press any key to close"))
		} else {
			lines = append(lines, normalItemStyle.Render(fmt.Sprintf("Found %d duplicate groups:", len(m.dedupGroups))))
			lines = append(lines, "")

			start := 0
			end := len(m.dedupGroups)
			if end > maxHeight-6 {
				if m.dedupSelected > maxHeight/2 {
					start = m.dedupSelected - maxHeight/2
				}
				end = start + maxHeight - 6
				if end > len(m.dedupGroups) {
					end = len(m.dedupGroups)
					start = end - (maxHeight - 6)
					if start < 0 {
						start = 0
					}
				}
			}

			for i := start; i < end; i++ {
				prefix := "  "
				style := normalItemStyle
				if i == m.dedupSelected {
					prefix = "❯ "
					style = selectedItemStyle
				}
				lines = append(lines, style.Render(prefix+m.dedupGroups[i]))
			}
			lines = append(lines, "")
			lines = append(lines, dimStyle.Render("j/k: navigate | any other key: close"))
		}

		return strings.Join(lines, "\n")
	}

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

func (m *Model) renderInspector(maxHeight int) string {
	var lines []string
	lines = append(lines, folderStyle.Render("🔬 Inspector"))
	lines = append(lines, "")

	if m.activePane != ListPane || len(m.bookmarks) == 0 || m.listCursor >= len(m.bookmarks) {
		lines = append(lines, dimStyle.Render("(no bookmark selected)"))
		return strings.Join(lines, "\n")
	}

	bookmark := m.bookmarks[m.listCursor]

	lines = append(lines, normalItemStyle.Render("Title:"))
	title := bookmark.Title
	if len(title) > 30 {
		title = title[:27] + "..."
	}
	lines = append(lines, dimStyle.Render("  "+title))
	lines = append(lines, "")

	lines = append(lines, normalItemStyle.Render("URL:"))
	url := bookmark.URL
	if len(url) > 30 {
		url = url[:27] + "..."
	}
	lines = append(lines, dimStyle.Render("  "+url))
	lines = append(lines, "")

	lines = append(lines, normalItemStyle.Render("GUID:"))
	lines = append(lines, dimStyle.Render("  "+bookmark.GUID))
	lines = append(lines, "")

	lines = append(lines, normalItemStyle.Render("ID:"))
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  %d", bookmark.ID)))
	lines = append(lines, "")

	lines = append(lines, normalItemStyle.Render("Added:"))
	lines = append(lines, dimStyle.Render("  "+bookmark.DateAdded.Format("2006-01-02 15:04")))
	lines = append(lines, "")

	lines = append(lines, normalItemStyle.Render("Modified:"))
	lines = append(lines, dimStyle.Render("  "+bookmark.LastModified.Format("2006-01-02 15:04")))
	lines = append(lines, "")

	lines = append(lines, normalItemStyle.Render("Visits:"))
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  %d", bookmark.VisitCount)))
	lines = append(lines, "")

	if status, ok := m.auditResults[bookmark.ID]; ok {
		lines = append(lines, normalItemStyle.Render("Link Status:"))
		statusStyle := dimStyle
		if status == "DEAD" {
			statusStyle = lipgloss.NewStyle().Foreground(accentColor)
		}
		lines = append(lines, statusStyle.Render("  "+status))
	}

	return strings.Join(lines, "\n")
}

func (m *Model) toggleInspector() {
	m.showInspector = !m.showInspector
	if m.showInspector {
		m.statusMessage = "Inspector panel shown"
	} else {
		m.statusMessage = "Inspector panel hidden"
	}
}

func (m *Model) startAudit() tea.Cmd {
	m.editMode = AuditMode
	m.auditInProgress = true
	m.auditResults = make(map[int64]string)
	m.auditTotal = 0
	m.auditCompleted = 0
	m.scanSpinner = 0
	m.statusMessage = "Starting link audit..."

	return tea.Batch(
		m.runAudit(),
		m.tickAudit(),
	)
}

func (m *Model) tickAudit() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return auditTickMsg{}
	})
}

func (m *Model) runAudit() tea.Cmd {
	return func() tea.Msg {
		auditor := audit.NewAuditor(10)
		ctx := context.Background()
		resultChan := auditor.AuditAll(ctx, m.root)

		for range resultChan {
		}

		return auditCompleteMsg{}
	}
}

func (m *Model) startDedup() tea.Cmd {
	if debugLog != nil {
		debugLog.Println("startDedup: entering function")
	}
	m.editMode = DedupMode
	m.dedupScanning = true
	m.dedupGroups = nil
	m.scanSpinner = 0
	m.statusMessage = "Scanning for duplicates..."

	if debugLog != nil {
		debugLog.Println("startDedup: calling tea.Batch with runDedup and tickDedup")
	}
	return tea.Batch(
		m.runDedup(),
		m.tickDedup(),
	)
}

func (m *Model) runDedup() tea.Cmd {
	dbPath := m.dbPath
	if debugLog != nil {
		debugLog.Println("runDedup: creating command function")
	}
	return func() tea.Msg {
		if debugLog != nil {
			debugLog.Println("runDedup: command function executing")
		}
		if debugLog != nil {
			debugLog.Printf("runDedup: opening database at %s", dbPath)
		}
		dbConn, err := db.OpenReadOnly(dbPath)
		if err != nil {
			if debugLog != nil {
				debugLog.Printf("runDedup: database open failed: %v", err)
			}
			return dedupResultMsg{err: err}
		}
		defer dbConn.Close()

		if debugLog != nil {
			debugLog.Println("runDedup: database opened, calling FindDuplicates")
		}
		groups, err := dedup.FindDuplicates(dbConn.Conn())
		if debugLog != nil {
			debugLog.Printf("runDedup: FindDuplicates returned, groups=%d, err=%v", len(groups), err)
		}
		return dedupResultMsg{groups: groups, err: err}
	}
}

func (m *Model) tickDedup() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return dedupTickMsg{}
	})
}

func collectAllBookmarks(node *models.Bookmark) []*models.Bookmark {
	var bookmarks []*models.Bookmark

	if node.IsBookmark() {
		bookmarks = append(bookmarks, node)
	}

	for _, child := range node.Children {
		bookmarks = append(bookmarks, collectAllBookmarks(child)...)
	}

	return bookmarks
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
