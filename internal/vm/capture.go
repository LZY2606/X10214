package vm

import "fmt"

// Group captures one group's current extent in byte coordinates.
// Set=false means the group has never participated; participated but later
// undone groups are restored to their previous Snapshot on backtrack, so
// revoked captures never leak.
type Group struct {
	Set   bool `json:"set"`
	Start int  `json:"start"`
	End   int  `json:"end"`
}

// Snapshot is an immutable capture state tagged with an ID for budget
// accounting. Snapshots are value types: copying one copies all groups.
type Snapshot struct {
	ID     int     `json:"id"`
	Groups []Group `json:"groups"`
}

func (s Snapshot) clone() Snapshot {
	g := make([]Group, len(s.Groups))
	copy(g, s.Groups)
	return Snapshot{ID: s.ID, Groups: g}
}

func (s Snapshot) with(id, idx int, gr Group) Snapshot {
	ns := s.clone()
	ns.ID = id
	ns.Groups[idx] = gr
	return ns
}

// Get returns group idx (1-based).
func (s Snapshot) Get(idx int) Group {
	if idx <= 0 || idx > len(s.Groups) {
		return Group{}
	}
	return s.Groups[idx-1]
}

// CapturedText returns the captured bytes for idx ("" when unset).
func (s Snapshot) CapturedText(in *Input, idx int) (string, bool) {
	g := s.Get(idx)
	if !g.Set {
		return "", false
	}
	return in.Raw[g.Start:g.End], true
}

func (s Snapshot) String() string {
	return fmt.Sprintf("snap#%d", s.ID)
}

// snapStore hands out snapshot IDs and enforces the capture-snapshot budget.
// Every distinct capture mutation ("snapshot") is counted; resumed runs share
// the same counter so the budget covers the whole derivation.
type snapStore struct {
	nextID int
	count  int
	limit  int
	base   Snapshot
}

func newSnapStore(groupCount, limit int) *snapStore {
	return &snapStore{nextID: 1, limit: limit, base: Snapshot{ID: 0, Groups: make([]Group, groupCount)}}
}

// make returns a fresh snapshot value with a new ID, charging the budget.
func (st *snapStore) make(prev Snapshot, idx int, gr Group) (Snapshot, bool) {
	if st.limit > 0 && st.count >= st.limit {
		return prev, false
	}
	st.count++
	return prev.with(st.nextID, idx, gr), true
}

func (st *snapStore) freshID() (int, bool) {
	if st.limit > 0 && st.count >= st.limit {
		return 0, false
	}
	id := st.nextID
	st.nextID++
	st.count++
	return id, true
}
