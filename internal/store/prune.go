package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// pruningPrefix marks a run directory that is being removed: it is renamed
// away first so the run disappears from every reader at once.
const pruningPrefix = ".prune-"

// PruneOptions tunes one Prune pass.
type PruneOptions struct {
	// OlderThan is how long ago a run must have ended to be removed.
	OlderThan time.Duration
	// Now reads the clock; nil means time.Now.
	Now func() time.Time
	// DryRun counts what would be removed and removes nothing.
	DryRun bool
	// Budget bounds the pass: once it has been spent the pass stops cleanly
	// before the next run. Zero means no bound.
	Budget time.Duration
	// Warn receives a line for each problem that did not stop the pass.
	Warn func(msg string)
	// BeforeLock runs for each run that passed the first check, just before
	// its lock is taken; AfterRun runs once a run has been dealt with.
	BeforeLock func(runID string)
	AfterRun   func(runID string)
}

// PruneResult counts what a pass did to the runs it examined.
type PruneResult struct {
	Removed    int   // runs removed (or, in a dry run, that would be)
	Kept       int   // runs with a valid state that do not qualify, or could not be taken
	Skipped    int   // runs whose state is missing or malformed
	Failed     int   // runs that qualified but could not be removed
	BytesFreed int64 // size of the removed run directories
	Stopped    bool  // the budget ran out before every run was examined
}

type verdict int

const (
	verdictRemove verdict = iota
	verdictKeep
	verdictMalformed
)

// pruneVerdict says what to do with a run from its recorded state alone: only
// a terminal run with a parseable end older than the cutoff goes.
func pruneVerdict(st State, cutoff time.Time) verdict {
	switch {
	case st.State == "queued" || st.State == "running":
		return verdictKeep
	case !IsTerminal(st.State) || st.EndedAt == nil:
		return verdictMalformed
	}
	ended, err := time.Parse(time.RFC3339, *st.EndedAt)
	switch {
	case err != nil:
		return verdictMalformed
	case !ended.Before(cutoff):
		return verdictKeep
	}
	return verdictRemove
}

type pruner struct {
	s      *Store
	o      PruneOptions
	cutoff time.Time
	active map[string]bool // run ids some conversation names as its active turn
	res    PruneResult
}

func (p *pruner) warn(msg string) {
	if p.o.Warn != nil {
		p.o.Warn(msg)
	}
}

// Prune removes the directories of terminal runs that ended more than
// OlderThan ago. A run is removed only when its state says so, no conversation
// names it as the active turn, its run lock can be taken without waiting, and
// the state still says so once the lock is held. Telemetry and conversation
// records are never touched. Index files left with no run are removed.
func (s *Store) Prune(o PruneOptions) (PruneResult, error) {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	p := &pruner{s: s, o: o, cutoff: start.Add(-o.OlderThan)}
	ids, err := s.RunIDs()
	if err != nil {
		return PruneResult{}, err
	}
	sort.Strings(ids)
	p.active = p.activeRunIDs()
	expired := func() bool { return o.Budget > 0 && now().Sub(start) >= o.Budget }
	for _, id := range ids {
		if expired() {
			p.res.Stopped = true
			break
		}
		p.pruneRun(id)
		if o.AfterRun != nil {
			o.AfterRun(id)
		}
	}
	if !o.DryRun {
		p.sweepLeftovers()
		p.compactIndexes(expired)
	}
	return p.res, nil
}

// activeRunIDs collects the run ids conversations name as their active turn.
func (p *pruner) activeRunIDs() map[string]bool {
	active := map[string]bool{}
	ids, err := p.s.ConversationIDs()
	if err != nil {
		p.warn("could not list the conversations: " + err.Error())
		return active
	}
	for _, id := range ids {
		c, err := p.s.ReadConversation(id)
		if err != nil {
			p.warn("could not read conversation " + id + ": " + err.Error())
			continue
		}
		if c.ActiveRunID != nil {
			active[*c.ActiveRunID] = true
		}
	}
	return active
}

func (p *pruner) pruneRun(id string) {
	st, err := p.s.ReadState(id)
	if err != nil {
		p.res.Skipped++
		return
	}
	switch pruneVerdict(st, p.cutoff) {
	case verdictMalformed:
		p.res.Skipped++
		return
	case verdictKeep:
		p.res.Kept++
		return
	}
	if p.active[id] {
		p.res.Kept++
		return
	}
	if p.o.DryRun {
		p.countIfFree(id)
		return
	}
	if p.o.BeforeLock != nil {
		p.o.BeforeLock(id)
	}
	p.removeUnderLock(id)
}

// countIfFree counts a run a dry run would remove, probing its lock without
// creating the lock file.
func (p *pruner) countIfFree(id string) {
	held, err := p.s.RunLockHeld(id)
	if err != nil || held {
		p.res.Kept++
		return
	}
	p.res.Removed++
	p.res.BytesFreed += dirSize(p.s.RunDir(id))
}

// removeUnderLock takes the run's lock, checks once more that the run still
// qualifies, and removes it.
func (p *pruner) removeUnderLock(id string) {
	lock, err := p.s.TryLockRun(id)
	switch {
	case errors.Is(err, ErrRunLockHeld):
		p.res.Kept++
		return
	case err != nil:
		p.fail(id, err)
		return
	}
	defer lock.Close()
	st, err := p.s.ReadState(id)
	if err != nil {
		p.res.Skipped++
		return
	}
	if v := pruneVerdict(st, p.cutoff); v != verdictRemove {
		p.res.Kept++
		return
	}
	if p.namedActive(st) {
		p.res.Kept++
		return
	}
	size := dirSize(p.s.RunDir(id))
	if err := p.s.removeRunDir(id); err != nil {
		p.fail(id, err)
		return
	}
	p.res.Removed++
	p.res.BytesFreed += size
}

// namedActive reports whether the run's own conversation, read now, names it
// as the active turn, or cannot be read to tell.
func (p *pruner) namedActive(st State) bool {
	c, err := p.s.ReadConversation(st.ConversationID)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false
	case err != nil:
		return true
	}
	return c.ActiveRunID != nil && *c.ActiveRunID == st.RunID
}

func (p *pruner) fail(id string, err error) {
	p.res.Failed++
	p.warn("could not remove run " + id + ": " + err.Error())
}

// removeRunDir renames the run directory out of the runs listing, then deletes
// it, so no reader sees a half-removed run.
func (s *Store) removeRunDir(id string) error {
	gone := filepath.Join(s.Home, "runs", pruningPrefix+id)
	if err := os.RemoveAll(gone); err != nil {
		return err
	}
	if err := os.Rename(s.RunDir(id), gone); err != nil {
		return err
	}
	return os.RemoveAll(gone)
}

// sweepLeftovers deletes directories an earlier pass renamed away but could
// not finish deleting.
func (p *pruner) sweepLeftovers() {
	entries, err := os.ReadDir(filepath.Join(p.s.Home, "runs"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), pruningPrefix) {
			if err := os.RemoveAll(filepath.Join(p.s.Home, "runs", e.Name())); err != nil {
				p.warn("could not remove " + e.Name() + ": " + err.Error())
			}
		}
	}
}

// compactIndexes drops from every session index the runs that are gone, and
// removes an index left with none.
func (p *pruner) compactIndexes(expired func() bool) {
	entries, err := os.ReadDir(p.s.sessionIndexDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if expired() {
			p.res.Stopped = true
			return
		}
		if err := p.s.compactIndex(filepath.Join(p.s.sessionIndexDir(), e.Name())); err != nil {
			p.warn("could not tidy the session index " + e.Name() + ": " + err.Error())
		}
	}
}

// compactIndex rewrites one index file without the ids whose run is gone. It
// holds the index lock, so no admission appends between the read and the
// rewrite.
func (s *Store) compactIndex(path string) error {
	unlock, err := s.lockIndex()
	if err != nil {
		return err
	}
	defer unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	kept := make([]string, 0)
	all := newestFirst(data)
	for i := len(all) - 1; i >= 0; i-- { // back to oldest first
		if s.RunExists(all[i]) {
			kept = append(kept, all[i])
		}
	}
	if len(kept) == len(all) {
		return nil
	}
	if len(kept) == 0 {
		return os.Remove(path)
	}
	return writeFileAtomic(path, []byte(strings.Join(kept, "\n")+"\n"))
}

// dirSize sums the sizes of the files under dir.
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			n += info.Size()
		}
		return nil
	})
	return n
}
