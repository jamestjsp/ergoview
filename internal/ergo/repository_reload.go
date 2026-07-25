package ergo

import (
	"errors"
	"fmt"
	"os"
)

type repositoryState struct {
	source   fileState
	offset   int64
	lines    int
	openLine bool
	replay   replayState
}

type fileState struct {
	exists bool
	info   os.FileInfo
}

func (r *Repository) Load() (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	snapshot, state, err := r.loadFull()
	if err != nil {
		return Snapshot{}, err
	}
	r.state = state
	return snapshot, nil
}

func (r *Repository) LoadIfChanged() (Snapshot, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	observed, err := statFile(r.eventPath)
	if err != nil {
		return Snapshot{}, false, err
	}
	if r.state != nil && observed.equal(r.state.source) {
		return Snapshot{}, false, nil
	}

	if r.state == nil {
		snapshot, state, err := r.loadFull()
		if err != nil {
			return Snapshot{}, false, err
		}
		r.state = state
		return snapshot, true, nil
	}

	file, current, err := openFile(r.eventPath)
	if err != nil {
		return Snapshot{}, false, err
	}
	if file != nil {
		defer file.Close()
	}
	if current.equal(r.state.source) {
		return Snapshot{}, false, nil
	}
	if r.state.openLine || !canReplayAppend(r.state.source, current) {
		snapshot, state, err := r.loadFullFrom(file, current)
		if err != nil {
			return Snapshot{}, false, err
		}
		r.state = state
		return snapshot, true, nil
	}

	events, offset, lines, openLine, err := readEventRange(
		file,
		r.eventPath,
		r.state.offset,
		current.info.Size(),
		r.state.lines,
	)
	if err != nil {
		return Snapshot{}, false, err
	}
	if err := replayInto(&r.state.replay, events); err != nil {
		r.state = nil
		return Snapshot{}, false, fmt.Errorf("%s: %w", r.eventPath, err)
	}
	r.state.source = current
	r.state.offset = offset
	r.state.lines = lines
	r.state.openLine = openLine
	return r.snapshot(), true, nil
}

func (r *Repository) loadFull() (Snapshot, *repositoryState, error) {
	file, source, err := openFile(r.eventPath)
	if err != nil {
		return Snapshot{}, nil, err
	}
	if file != nil {
		defer file.Close()
	}
	return r.loadFullFrom(file, source)
}

func (r *Repository) loadFullFrom(file *os.File, source fileState) (Snapshot, *repositoryState, error) {
	state := &repositoryState{
		source: source,
		replay: newReplayState(),
	}
	if source.exists {
		events, offset, lines, openLine, err := readEventRange(file, r.eventPath, 0, source.info.Size(), 0)
		if err != nil {
			return Snapshot{}, nil, err
		}
		if err := replayInto(&state.replay, events); err != nil {
			return Snapshot{}, nil, fmt.Errorf("%s: %w", r.eventPath, err)
		}
		state.offset = offset
		state.lines = lines
		state.openLine = openLine
	}
	snapshot := buildSnapshot(
		r.root,
		r.ergoDir,
		r.eventPath,
		source.version(),
		state.replay.tasks,
		state.replay.deps,
	)
	return snapshot, state, nil
}

func (r *Repository) snapshot() Snapshot {
	return buildSnapshot(
		r.root,
		r.ergoDir,
		r.eventPath,
		r.state.source.version(),
		r.state.replay.tasks,
		r.state.replay.deps,
	)
}

func statFile(path string) (fileState, error) {
	info, err := os.Stat(path)
	if err == nil {
		return fileState{exists: true, info: info}, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return fileState{}, nil
	}
	return fileState{}, err
}

func openFile(path string) (*os.File, fileState, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fileState{}, nil
		}
		return nil, fileState{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fileState{}, err
	}
	return file, fileState{exists: true, info: info}, nil
}

func (state fileState) version() string {
	if !state.exists {
		return ""
	}
	return fmt.Sprintf("%d:%d", state.info.ModTime().UnixNano(), state.info.Size())
}

func (state fileState) equal(other fileState) bool {
	if state.exists != other.exists {
		return false
	}
	if !state.exists {
		return true
	}
	return state.info.Size() == other.info.Size() &&
		state.info.ModTime().Equal(other.info.ModTime()) &&
		os.SameFile(state.info, other.info)
}

func canReplayAppend(previous, current fileState) bool {
	if !previous.exists || !current.exists || !os.SameFile(previous.info, current.info) {
		return false
	}
	// Ergo appends mutations and replaces the log file when compacting. Identity,
	// size, and mtime distinguish those supported writes without opening an idle log.
	return current.info.Size() > previous.info.Size() &&
		!current.info.ModTime().Before(previous.info.ModTime())
}
