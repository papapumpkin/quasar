package dag

// UnionFind implements a disjoint-set (union-find) data structure with
// path compression and union by rank. It partitions a set of string
// elements into equivalence classes that can be queried efficiently.
type UnionFind struct {
	parent map[string]string
	rank   map[string]int
}

// NewUnionFind creates an empty UnionFind.
func NewUnionFind() *UnionFind {
	return &UnionFind{
		parent: make(map[string]string),
		rank:   make(map[string]int),
	}
}

// Add inserts an element as its own singleton set. If the element
// already exists, this is a no-op.
func (uf *UnionFind) Add(x string) {
	if _, ok := uf.parent[x]; ok {
		return
	}
	uf.parent[x] = x
	uf.rank[x] = 0
}

// Find returns the representative (root) of the set containing x.
// Path compression is applied so subsequent queries are nearly O(1).
// If x has not been added, it is auto-added as a singleton first.
func (uf *UnionFind) Find(x string) string {
	if _, ok := uf.parent[x]; !ok {
		uf.Add(x)
		return x
	}
	if uf.parent[x] != x {
		uf.parent[x] = uf.Find(uf.parent[x]) // path compression
	}
	return uf.parent[x]
}

// Union merges the sets containing x and y. Union by rank keeps the
// tree balanced. Both elements are auto-added if not already present.
func (uf *UnionFind) Union(x, y string) {
	rx := uf.Find(x)
	ry := uf.Find(y)
	if rx == ry {
		return
	}
	// Attach the shorter tree under the taller one.
	switch {
	case uf.rank[rx] < uf.rank[ry]:
		uf.parent[rx] = ry
	case uf.rank[rx] > uf.rank[ry]:
		uf.parent[ry] = rx
	default:
		uf.parent[ry] = rx
		uf.rank[rx]++
	}
}

// Connected reports whether x and y belong to the same set.
func (uf *UnionFind) Connected(x, y string) bool {
	return uf.Find(x) == uf.Find(y)
}

// Components returns the disjoint sets as a map from each set's
// representative to the list of members. The member lists are
// returned in no guaranteed order.
func (uf *UnionFind) Components() map[string][]string {
	groups := make(map[string][]string)
	for x := range uf.parent {
		root := uf.Find(x)
		groups[root] = append(groups[root], x)
	}
	return groups
}
