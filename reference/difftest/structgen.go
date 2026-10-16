package difftest

// Structured datasets. The random generator (gen.go) rarely produces
// exact ties or exactly independent items, but these are where the two
// implementations' last-bit differences matter: measures that equal the
// evaluation threshold exactly (e.g. -eb -d0 or a lift of exactly 1 for
// independent items, docs/differences.md F-1) and frequency ties, where
// the item order depends on the sort. The generators here produce such
// data on purpose; newCase uses them for some cases (structKind), and the
// large-data mode (large.go) uses the equal-frequency and block
// generators at scale.

// structKind selects a structured dataset of a small case.
type structKind uint8

const (
	structNone        structKind = iota
	structIndependent            // products of independent item groups
	structTies                   // many equal item and set frequencies
)

// structured draws a structured dataset with the item names and the
// number of items of g. weighted selects weights (else every transaction
// has weight 1 and repetitions express the multiplicities).
func structured(r *rng, kind structKind, g genConfig, weighted bool) []transaction {
	names := make([]string, g.numItems)
	for i := range names {
		names[i] = g.names.itemName(i)
	}
	if kind == structIndependent {
		return genIndependent(r, names, weighted)
	}
	n := max(4, min(g.numTransactions, 60))
	switch r.intN(3) {
	case 0:
		return genEqualFreq(r, names, n, 1+r.intN(n), weighted)
	case 1:
		return genCyclic(r, names, weighted)
	default:
		return genFewDistinct(r, names, weighted)
	}
}

// genIndependent returns a database in which items of different groups
// are exactly independent: the items are split into two or three groups,
// each group has a few patterns (random subsets of its items) with
// multiplicities, and the database holds every combination of one
// pattern per group with the product of their multiplicities. For items
// a and b of different groups supp(ab)*N = supp(a)*supp(b) holds exactly,
// so measures of the rule a -> b are at their independence values (lift 1,
// zero differences, a log ratio of 0).
func genIndependent(r *rng, names []string, weighted bool) []transaction {
	n := len(names)
	groups := min(n, 2+r.intN(2))
	maxMult := 4
	if groups == 3 {
		maxMult = 2 // at most 6^3 unit-weight transactions
	}
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	r.shuffle(n, func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
	type pattern struct {
		items []string
		mult  int64
	}
	pats := make([][]pattern, groups)
	for gi := range groups {
		var members []string
		for k := gi; k < n; k += groups {
			members = append(members, names[perm[k]])
		}
		for range 1 + r.intN(3) {
			var p pattern
			for _, it := range members {
				if chance(r, 0.6) {
					p.items = append(p.items, it)
				}
			}
			p.mult = 1 + int64(r.intN(maxMult))
			pats[gi] = append(pats[gi], p)
		}
	}
	var txs []transaction
	var rec func(gi int, items []string, mult int64)
	rec = func(gi int, items []string, mult int64) {
		if gi == groups {
			if weighted {
				txs = append(txs, transaction{items: append([]string{}, items...), weight: mult})
				return
			}
			for range mult {
				txs = append(txs, transaction{items: append([]string{}, items...), weight: 1})
			}
			return
		}
		for _, p := range pats[gi] {
			rec(gi+1, append(items, p.items...), mult*p.mult)
		}
	}
	rec(0, nil, 1)
	shuffleTxs(r, txs)
	return txs
}

// genEqualFreq returns numTx transactions in which every item occurs in
// exactly k of them (k is clamped to [1, numTx]), at random positions.
func genEqualFreq(r *rng, names []string, numTx, k int, weighted bool) []transaction {
	k = max(1, min(k, numTx))
	txs := make([]transaction, numTx)
	w := equalWeight(r, weighted)
	for t := range txs {
		txs[t].weight = w
	}
	idx := make([]int, numTx)
	for i := range idx {
		idx[i] = i
	}
	for _, it := range names {
		for j := range k { // partial Fisher-Yates: k distinct transactions
			p := j + r.intN(numTx-j)
			idx[j], idx[p] = idx[p], idx[j]
			txs[idx[j]].items = append(txs[idx[j]].items, it)
		}
	}
	for t := range txs {
		items := txs[t].items
		r.shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		if items == nil {
			txs[t].items = []string{}
		}
	}
	return txs
}

// genCyclic returns windows of a cyclic item sequence: transaction t holds
// items t, t+1, ..., t+L-1 (mod n), each window repeated the same number
// of times. All items have the same frequency, and all sets of the same
// shape (distances) tie.
func genCyclic(r *rng, names []string, weighted bool) []transaction {
	n := len(names)
	l := 1 + r.intN(n)
	rep := 1 + r.intN(4)
	w := equalWeight(r, weighted)
	var txs []transaction
	for t := range n {
		items := make([]string, l)
		for k := range l {
			items[k] = names[(t+k)%n]
		}
		for range rep {
			txs = append(txs, transaction{items: append([]string{}, items...), weight: w})
		}
	}
	shuffleTxs(r, txs)
	return txs
}

// genFewDistinct returns a few distinct random transactions, each
// repeated the same number of times: supports are multiples of one
// count, with many equal values.
func genFewDistinct(r *rng, names []string, weighted bool) []transaction {
	rep := 1 + r.intN(6)
	w := equalWeight(r, weighted)
	var txs []transaction
	for range 2 + r.intN(4) {
		var items []string
		for _, it := range names {
			if chance(r, 0.5) {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []string{}
		}
		for range rep {
			txs = append(txs, transaction{items: append([]string{}, items...), weight: w})
		}
	}
	shuffleTxs(r, txs)
	return txs
}

// genBlocks returns numTx transactions over blocks of items: each
// transaction picks one to three blocks (block j with weight 1/(j+1)),
// contains each item of a picked block with probability density, and a
// few random noise items. Items of a block co-occur and have similar
// frequencies, which gives many closed and maximal sets and ties.
func genBlocks(r *rng, names []string, numTx, blockSize int, density float64, noise int) []transaction {
	n := len(names)
	blockSize = max(1, min(blockSize, n))
	numBlocks := (n + blockSize - 1) / blockSize
	pop := make([]uint64, numBlocks)
	var total uint64
	for j := range pop {
		pop[j] = 1 << 20 / uint64(j+1)
		total += pop[j]
	}
	txs := make([]transaction, numTx)
	for t := range txs {
		seen := map[int]bool{}
		var items []string
		add := func(i int) {
			if !seen[i] {
				seen[i] = true
				items = append(items, names[i])
			}
		}
		for range 1 + r.intN(3) {
			u := r.uint64N(total)
			j := 0
			for u >= pop[j] {
				u -= pop[j]
				j++
			}
			for i := j * blockSize; i < min(n, (j+1)*blockSize); i++ {
				if r.float() < density {
					add(i)
				}
			}
		}
		for range r.intN(noise + 1) {
			add(r.intN(n))
		}
		r.shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		if items == nil {
			items = []string{}
		}
		txs[t] = transaction{items: items, weight: 1}
	}
	return txs
}

// equalWeight returns the common weight of a tie dataset.
func equalWeight(r *rng, weighted bool) int64 {
	if !weighted {
		return 1
	}
	return 1 + int64(r.intN(3))
}

func shuffleTxs(r *rng, txs []transaction) {
	r.shuffle(len(txs), func(i, j int) { txs[i], txs[j] = txs[j], txs[i] })
}

// independenceThreshold returns the -d value (in percent) at which the
// measure of a rule between independent items lies exactly at the
// threshold, if there is a simple one: the log ratio (-eb for item sets),
// the differences of confidence, lift, conviction and the conditional
// probability ratio to their independence values, the importance and the
// chi^2 and information measures are 0; lift and conviction are 1.
func independenceThreshold(c *tcase) (float64, bool) {
	switch c.eval {
	case 'b':
		return 0, !c.isRules()
	case 'l', 'v':
		return 100, true
	case 'd', 'a', 'q', 'e', 'r', 'u', 'j', 'n', 'i', 'y':
		return 0, true
	}
	return 0, false
}
