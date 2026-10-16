package report

import "github.com/dimchansky/fpgrowth-go/internal/tract"

// Sink receives the item sets and association rules accepted by a
// Reporter, in report order. It replaces upstream's output file: the text
// formatting of the fpgrowth program lives in internal/isrout, which
// implements Sink.
//
// The slices in ItemSet and Rule alias the reporter's internal buffers and
// are only valid during the call; a Sink must copy them to retain them and
// must not modify them. A non-nil error aborts the report in progress; the
// reporter method that made the call returns that error unchanged.
type Sink interface {
	// ItemSet receives an accepted item set.
	ItemSet(s ItemSet) error
	// Rule receives an accepted association rule.
	Rule(r Rule) error
}

// ItemSet is an item set passed to Sink.ItemSet.
type ItemSet struct {
	// Items are the items in report order: the items added with
	// Reporter.Add in insertion order, followed by the perfect extensions
	// in the order they were appended during expansion.
	Items []tract.Item
	// Supp is the support of the item set.
	Supp tract.Supp
	// Weight is upstream's item set weight (the %w family of output
	// codes). fpgrowth never sets item weights, so it is the total
	// transaction weight for sets reported while the prefix is empty
	// (the empty set and the expansions of the top-level perfect
	// extensions) and 0 otherwise.
	Weight float64
	// Eval is the evaluation of the item set: the value of the
	// evaluation function (SetEval) if there is one, else the value most
	// recently passed to ReportEval (0 if none).
	Eval float64
}

// Rule is an association rule passed to Sink.Rule.
type Rule struct {
	Head     tract.Item   // rule head (consequent)
	Body     []tract.Item // rule body (antecedent) in report order
	Supp     tract.Supp   // support of the rule (body and head together)
	BodySupp tract.Supp   // support of the body
	HeadSupp tract.Supp   // support of the head
	Eval     float64      // evaluation passed to Reporter.ReportRule
}
