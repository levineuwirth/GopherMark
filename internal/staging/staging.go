package staging

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type StagingDB struct {
	originalPath string
	stagingPath  string
	conn         *sql.DB
}

func CreateStaging(originalPath string) (*StagingDB, error) {
	tempDir := os.TempDir()
	stagingPath := filepath.Join(tempDir, fmt.Sprintf("gophermark-staging-%d.sqlite", os.Getpid()))

	if err := copyFile(originalPath, stagingPath); err != nil {
		return nil, fmt.Errorf("failed to create staging copy: %w", err)
	}

	conn, err := sql.Open("sqlite", stagingPath)
	if err != nil {
		os.Remove(stagingPath)
		return nil, fmt.Errorf("failed to open staging database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		os.Remove(stagingPath)
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	return &StagingDB{
		originalPath: originalPath,
		stagingPath:  stagingPath,
		conn:         conn,
	}, nil
}

func (s *StagingDB) Conn() *sql.DB {
	return s.conn
}

func (s *StagingDB) Commit() error {
	if running, process := isBrowserRunning(); running {
		return fmt.Errorf("cannot commit: %s is still running (close it first)", process)
	}

	if err := s.conn.Close(); err != nil {
		return fmt.Errorf("failed to close staging connection: %w", err)
	}

	backupPath := s.originalPath + ".backup"
	if err := copyFile(s.originalPath, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	if err := os.Rename(s.stagingPath, s.originalPath); err != nil {
		os.Rename(backupPath, s.originalPath)
		return fmt.Errorf("failed to swap databases: %w", err)
	}
	// TODO: configure so we can allow user to save the backup elsewhere
	os.Remove(backupPath)

	return nil
}

func (s *StagingDB) Rollback() error {
	if s.conn != nil {
		s.conn.Close()
	}
	return os.Remove(s.stagingPath)
}

func (s *StagingDB) Close() error {
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			return err
		}
	}
	if _, err := os.Stat(s.stagingPath); err == nil {
		return os.Remove(s.stagingPath)
	}
	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}

func isBrowserRunning() (bool, string) {
	processes := []string{"firefox", "librewolf", "firefox-bin", "librewolf-bin"}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux", "darwin":
		cmd = exec.Command("pgrep", "-i", strings.Join(processes, "|"))
	case "windows":
		cmd = exec.Command("tasklist")
	default:
		return false, ""
	}

	output, err := cmd.Output()
	if err != nil {
		return false, ""
	}

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		return len(output) > 0, detectWhichBrowser(string(output))
	}

	outputStr := strings.ToLower(string(output))
	for _, proc := range processes {
		if strings.Contains(outputStr, proc) {
			return true, proc
		}
	}

	return false, ""
}

func detectWhichBrowser(output string) string {
	output = strings.ToLower(output)
	if strings.Contains(output, "librewolf") {
		return "LibreWolf"
	}
	if strings.Contains(output, "firefox") {
		return "Firefox"
	}
	return "Browser"
}

func (s *StagingDB) UpdateBookmarkTitle(bookmarkID int64, newTitle string) error {
	_, err := s.conn.Exec("UPDATE moz_bookmarks SET title = ?, lastModified = ? WHERE id = ?",
		newTitle, currentMicroseconds(), bookmarkID)
	return err
}

func (s *StagingDB) UpdateBookmarkURL(placeID int64, newURL string) error {
	_, err := s.conn.Exec("UPDATE moz_places SET url = ?, last_visit_date = ? WHERE id = ?",
		newURL, currentMicroseconds(), placeID)
	return err
}

func (s *StagingDB) DeleteBookmark(bookmarkID int64) error {
	_, err := s.conn.Exec("DELETE FROM moz_bookmarks WHERE id = ?", bookmarkID)
	return err
}

func (s *StagingDB) MoveBookmark(bookmarkID, newParentID int64, newPosition int) error {
	_, err := s.conn.Exec("UPDATE moz_bookmarks SET parent = ?, position = ?, lastModified = ? WHERE id = ?",
		newParentID, newPosition, currentMicroseconds(), bookmarkID)
	return err
}

func (s *StagingDB) AddBookmark(parentID int64, title, url string) error {
	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var placeID int64
	err = tx.QueryRow("SELECT id FROM moz_places WHERE url = ?", url).Scan(&placeID)
	if err != nil {
		result, err := tx.Exec(`
			INSERT INTO moz_places (url, title, rev_host, hidden, typed, frecency, last_visit_date, guid)
			VALUES (?, ?, '', 0, 0, -1, ?, lower(hex(randomblob(16))))
		`, url, title, currentMicroseconds())
		if err != nil {
			return fmt.Errorf("failed to insert place: %w", err)
		}
		placeID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get place ID: %w", err)
		}
	}

	var maxPosition int
	err = tx.QueryRow("SELECT COALESCE(MAX(position), -1) FROM moz_bookmarks WHERE parent = ?", parentID).Scan(&maxPosition)
	if err != nil {
		return fmt.Errorf("failed to get max position: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO moz_bookmarks (type, fk, parent, position, title, dateAdded, lastModified, guid)
		VALUES (1, ?, ?, ?, ?, ?, ?, lower(hex(randomblob(16))))
	`, placeID, parentID, maxPosition+1, title, currentMicroseconds(), currentMicroseconds())
	if err != nil {
		return fmt.Errorf("failed to insert bookmark: %w", err)
	}

	return tx.Commit()
}

func currentMicroseconds() int64 {
	return int64(time.Now().UnixNano() / 1000)
}

func (s *StagingDB) FindOrCreateScratchFolder() (int64, error) {
	var folderID int64
	err := s.conn.QueryRow("SELECT id FROM moz_bookmarks WHERE type = 2 AND title = 'Scratch'").Scan(&folderID)
	if err == nil {
		return folderID, nil
	}

	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to query scratch folder: %w", err)
	}

	var menuID int64
	err = s.conn.QueryRow("SELECT id FROM moz_bookmarks WHERE guid = 'menu________'").Scan(&menuID)
	if err != nil {
		return 0, fmt.Errorf("failed to find bookmarks menu: %w", err)
	}

	var maxPosition int
	err = s.conn.QueryRow("SELECT COALESCE(MAX(position), -1) FROM moz_bookmarks WHERE parent = ?", menuID).Scan(&maxPosition)
	if err != nil {
		return 0, fmt.Errorf("failed to get max position: %w", err)
	}

	result, err := s.conn.Exec(`
		INSERT INTO moz_bookmarks (type, fk, parent, position, title, dateAdded, lastModified, guid)
		VALUES (2, NULL, ?, ?, 'Scratch', ?, ?, lower(hex(randomblob(16))))
	`, menuID, maxPosition+1, currentMicroseconds(), currentMicroseconds())
	if err != nil {
		return 0, fmt.Errorf("failed to create scratch folder: %w", err)
	}

	folderID, err = result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get folder ID: %w", err)
	}

	return folderID, nil
}
