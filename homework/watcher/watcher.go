package watcher

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type EventType string

const (
	EventTypeFileCreate  EventType = "file_created"
	EventTypeFileRemoved EventType = "file_removed"
)

var ErrDirNotExist = errors.New("dir does not exist")

// Event - событие, формируемое при изменении в файловой системе.
type Event struct {
	// Type - тип события.
	Type EventType
	// Path - путь к объекту изменения.
	Path string
}

type Watcher struct {
	// Events - канал событий
	Events chan Event
	// refreshInterval - интервал обновления списка файлов
	refreshInterval time.Duration

	// можете добавить свои поля
}

func NewDirWatcher(refreshInterval time.Duration) *Watcher {
	return &Watcher{
		refreshInterval: refreshInterval,
		Events:          make(chan Event),
	}
}

func (w *Watcher) WatchDir(ctx context.Context, path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ErrDirNotExist
	}

	oldWatchedFiles := map[string]struct{}{}
	watchedFiles := map[string]struct{}{}

	watcherImpl := func(curPath string, filetype fs.DirEntry, fileReadErr error) error {
		if fileReadErr != nil {
			return fileReadErr // Fail gracefully
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if filetype.IsDir() { // We ignore dirs
			return nil
		}

		watchedFiles[curPath] = struct{}{}

		return nil
	}

	flushMaps := func() {
		oldWatchedFiles = watchedFiles
		watchedFiles = map[string]struct{}{}
	}

	if err := filepath.WalkDir(path, watcherImpl); err != nil {
		return err
	}

	flushMaps()

	ticker := time.NewTicker(w.refreshInterval)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := filepath.WalkDir(path, watcherImpl); err != nil {
				return err
			}

			for curPath := range watchedFiles {
				if _, exists := oldWatchedFiles[curPath]; !exists {
					w.Events <- Event{
						Type: EventTypeFileCreate,
						Path: curPath,
					}
				}
			}
			for curPath := range oldWatchedFiles {
				if _, exists := watchedFiles[curPath]; !exists {
					w.Events <- Event{
						Type: EventTypeFileRemoved,
						Path: curPath,
					}
				}
			}

			flushMaps()
		}
	}
}

func (w *Watcher) Close() {
	close(w.Events)
}
