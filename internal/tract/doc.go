// Package tract manages items and transactions for the FP-growth miner.
//
// It is a port of the parts of Christian Borgelt's tract.c (compiled with
// TA_READ, as used by the fpgrowth program) and of the sorting routines from
// arrays.c that influence item order. See docs/upstream/tract.md for the
// upstream behavior this package reproduces.
//
// # Pipeline
//
// fpg_data's preparation of the data maps onto this package as follows:
//
//	b := NewItemBase[string]()      // ib_create(0, 0)
//	b.Select(...)                   // ib_readsel (or the appearance methods)
//	b.Count(scan)                   // tbg_read: pass 1, counters only
//	n := b.Recode(smin, order)      // tbg_recode(bag, smin, -1, -1, -order)
//	bag, err := b.BuildBag(scan, z) // pass 2: recode, tbg_filter(bag, z,
//	                                // NULL, 0), tbg_itsort(+1),
//	                                // tbg_sort(+1, 0), tbg_reduce(0)
//	bag.Pack(k)                     // tbg_pack(bag, k)
//
// CountBytes and BuildBagBytes are Count and BuildBag over byte-string
// identifiers for an item base of strings (the CLIs): they look the
// identifiers up without converting them to strings.
//
// Instead of storing every transaction like upstream, pass 2 inserts the
// recoded transactions into a prefix tree and the bag iterates that tree
// in exactly the order of upstream's sorted and reduced bag.
//
// # Upstream to Go
//
//	ITEM, SUPP                       Item, Supp (Supp is 64 bits)
//	TA_END                           End
//	APP_NONE/BODY/HEAD/BOTH          AppearNone/Body/Head/Both
//	appcode (+ appmap, appcmp)       ParseAppearance
//	ITEMBASE, ITEMDATA               ItemBase, itemData
//	ib_create(0, 0)                  NewItemBase
//	ib_readsel                       ItemBase.Select
//	ib_readapp                       ItemBase.SetDefaultAppearance,
//	                                 ItemBase.AddAppearance
//	ib_read (counting), tbg_read     ItemBase.Count, CountBytes
//	ib_recode, idm_sort              ItemBase.Recode
//	nocmp, asccmp, descmp,           itemComparator
//	asccmpx, descmpx
//	ptr_qsort, ptr_qrec (arrays.c)   ptrQsort, ptrQrec
//	ib_cnt                           ItemBase.Len
//	ib_name                          ItemBase.ID
//	idm_getid                        ItemBase.Code
//	ib_getfrq, ib_getxfq             ItemBase.Frequency, ItemBase.ExtFrequency
//	ib_getapp                        ItemBase.Appearance,
//	                                 ItemBase.DefaultAppearance
//	ib_getwgt                        ItemBase.TotalWeight
//	ib_maxfrq                        ItemBase.MaxFrequency
//	TABAG                            Bag
//	tbg_add, recode, tbg_filter,     ItemBase.BuildBag, BuildBagBytes
//	tbg_itsort, tbg_sort, ta_cmp,
//	tbg_reduce
//	tbg_pack, ta_pack                Bag.Pack, Bag.PackCount, Item.IsPacked,
//	                                 Item.PackedBits
//	tbg_ifrqs(bag, 0), tbg_count     Bag.Frequencies
//	tbg_itemcnt                      Bag.NumItems
//	tbg_cnt                          Bag.Len (after reduction),
//	                                 Bag.Records (before it)
//	tbg_wgt                          Bag.TotalWeight
//	tbg_max                          Bag.MaxSize
//	tbg_extent                       Bag.Extent
//	tbg_tract(bag, j), j = 0..n-1    Bag.All
//	tbg_tract(bag, j), j = n-1..0    Bag.Backward
//	(fpgpsp's surrogates, mined      DenseBase: Count, Recode, BuildBag
//	one after the other)             as above, without a map, reused
//
// Not ported here (unreachable from fpgrowth or replaced by the prefix
// tree): item weights (IB_WEIGHTS, WTRACT), the transaction tree,
// writing, ib_add2ta/ib_finta, tbg_trim, tbg_mirror, tbg_unpack,
// tbg_icnts, the bucket sort of tbg_sort and ptr_mrgsort. Reading the
// input format and -R files is done by package tabread. The surrogate
// generators of TA_SURR (tbg_ident, tbg_random, tbg_swap, tbg_shuffle,
// tbg_istab), which fpgpsp uses, are ported by package psp on arrays of
// transactions it builds from a Bag.
//
// # Deviations from upstream
//
// Negative transaction weights and an int64 overflow of the total weight
// are errors; size-weighted frequencies saturate at math.MaxInt64; the
// minimum support passed to Recode is at least 1. Each is marked with a
// "Deviation from upstream:" comment.
package tract
