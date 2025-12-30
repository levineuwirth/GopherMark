package audit

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/levineuwirth/gophermark/internal/models"
)

type LinkStatus int

const (
	StatusPending LinkStatus = iota
	StatusAlive
	StatusDead
	StatusTimeout
)

type LinkResult struct {
	Bookmark   *models.Bookmark
	Status     LinkStatus
	StatusCode int
}

type Auditor struct {
	results   map[int64]LinkResult
	mu        sync.RWMutex
	workers   int
	timeout   time.Duration
	userAgent string
}

func NewAuditor(workers int) *Auditor {
	if workers <= 0 {
		workers = 10
	}
	return &Auditor{
		results:   make(map[int64]LinkResult),
		workers:   workers,
		timeout:   5 * time.Second,
		userAgent: "GopherMark/1.0",
	}
}

func (a *Auditor) AuditAll(ctx context.Context, root *models.Bookmark) <-chan LinkResult {
	resultChan := make(chan LinkResult, 100)

	go func() {
		defer close(resultChan)

		bookmarks := collectBookmarks(root)

		jobs := make(chan *models.Bookmark, len(bookmarks))
		for _, b := range bookmarks {
			jobs <- b
		}
		close(jobs)

		var wg sync.WaitGroup
		for i := 0; i < a.workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for bookmark := range jobs {
					select {
					case <-ctx.Done():
						return
					default:
						result := a.checkLink(bookmark)
						a.mu.Lock()
						a.results[bookmark.ID] = result
						a.mu.Unlock()
						resultChan <- result
					}
				}
			}()
		}

		wg.Wait()
	}()

	return resultChan
}

func (a *Auditor) checkLink(bookmark *models.Bookmark) LinkResult {
	if bookmark.URL == "" {
		return LinkResult{
			Bookmark: bookmark,
			Status:   StatusDead,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, bookmark.URL, nil)
	if err != nil {
		return LinkResult{
			Bookmark: bookmark,
			Status:   StatusDead,
		}
	}

	req.Header.Set("User-Agent", a.userAgent)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return LinkResult{
				Bookmark: bookmark,
				Status:   StatusTimeout,
			}
		}
		return LinkResult{
			Bookmark: bookmark,
			Status:   StatusDead,
		}
	}
	defer resp.Body.Close()

	status := StatusAlive
	if resp.StatusCode >= 400 {
		status = StatusDead
	}

	return LinkResult{
		Bookmark:   bookmark,
		Status:     status,
		StatusCode: resp.StatusCode,
	}
}

func (a *Auditor) GetResult(bookmarkID int64) (LinkResult, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result, ok := a.results[bookmarkID]
	return result, ok
}

func (a *Auditor) GetDeadLinks() []LinkResult {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var dead []LinkResult
	for _, result := range a.results {
		if result.Status == StatusDead || result.Status == StatusTimeout {
			dead = append(dead, result)
		}
	}
	return dead
}

func collectBookmarks(node *models.Bookmark) []*models.Bookmark {
	var bookmarks []*models.Bookmark

	if node.IsBookmark() && node.URL != "" {
		bookmarks = append(bookmarks, node)
	}

	for _, child := range node.Children {
		bookmarks = append(bookmarks, collectBookmarks(child)...)
	}

	return bookmarks
}
