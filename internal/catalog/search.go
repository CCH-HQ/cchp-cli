package catalog

import (
	"sort"
	"strings"
	"unicode"

	"github.com/kljensen/snowball/english"
)

// How much a query word counts when it matches each part of an operation.
// People and agents search with the words of the command path and the English
// summary, so those decide the ranking; the operationId and the HTTP path add
// little, which keeps a command where it was when CCHP renames its operation.
const (
	weightOperationID = 100 // the query word is the whole operationId
	weightCommand     = 30  // a word of the command's group, its name or an alias
	weightGroupTitle  = 20  // a word of the group's title, such as "Provider keys"
	weightSummary     = 12  // a word of the English summary
	weightIDWord      = 8   // a word of the operationId
	weightPathWord    = 3   // a word of the HTTP path
	weightPartial     = 2   // part of a word in the command path, summary, operationId or path

	bonusArea  = 25 // every query word names the command's group
	bonusName  = 25 // the query words name the command itself, and they do not all name its group
	bonusExact = 50 // the query words are exactly the words of the command path
)

// Search ranks operations by how well they match every word of the query; an
// operation that misses a word is left out. Unavailable operations are included
// only when includeAll is set.
//
// Among equal matches a list comes first, because a search that names a thing
// without saying what to do with it is looking for the list of that thing; then
// the command whose path holds more of the words exactly as typed; then the
// command with the shorter name.
func (c *Catalog) Search(query string, includeAll bool) []*Operation {
	q := parseQuery(query)
	if len(q.words) == 0 {
		return nil
	}
	titles := map[string]string{}
	for _, g := range c.Groups {
		titles[g.Name] = g.Title
	}
	type hit struct {
		op         *Operation
		score      int
		list       int
		exactWords int
		nameWords  int
	}
	var hits []hit
	for _, op := range c.Operations {
		if !includeAll && op.Status != StatusAvailable {
			continue
		}
		d := newSearchDoc(op, titles[op.Group])
		if score, ok := d.score(q); ok {
			hits = append(hits, hit{op, score, listRank(op), d.exactWords(q), len(d.nameWords)})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.list != b.list {
			return a.list < b.list
		}
		if a.exactWords != b.exactWords {
			return a.exactWords > b.exactWords
		}
		if a.nameWords != b.nameWords {
			return a.nameWords < b.nameWords
		}
		if len(a.op.Command()) != len(b.op.Command()) {
			return len(a.op.Command()) < len(b.op.Command())
		}
		return a.op.ID < b.op.ID
	})
	out := make([]*Operation, len(hits))
	for i, h := range hits {
		out[i] = h.op
	}
	return out
}

// query is a search split into words. Each word keeps how it was typed, for
// matching a whole operationId and parts of words, and its stems, for matching
// whole words in any grammatical form.
type query struct {
	words []queryWord
	stems []string
}

type queryWord struct {
	typed string
	terms []queryTerm
}

type queryTerm struct {
	word, stem string
}

func parseQuery(s string) query {
	var q query
	var all []queryWord
	for _, field := range strings.Fields(s) {
		w := queryWord{typed: strings.ToLower(field)}
		for _, word := range splitWords(field, isIdentifier(field)) {
			w.terms = append(w.terms, queryTerm{word: word, stem: english.Stem(word, true)})
		}
		if len(w.terms) > 0 {
			all = append(all, w)
		}
	}
	// Words such as "the" or "of" match nearly everything, so they are dropped
	// unless the query has nothing else.
	for _, w := range all {
		var kept []queryTerm
		for _, t := range w.terms {
			if !english.IsStopWord(t.word) {
				kept = append(kept, t)
			}
		}
		if len(kept) > 0 {
			w.terms = kept
			q.words = append(q.words, w)
		}
	}
	if len(q.words) == 0 {
		q.words = all
	}
	for _, w := range q.words {
		for _, t := range w.terms {
			q.stems = append(q.stems, t.stem)
		}
	}
	return q
}

// searchDoc is the searchable text of one operation, as stems.
type searchDoc struct {
	id        string
	groupList []string
	group     map[string]bool // the group's name
	title     map[string]bool // the group's title
	nameWords []string
	name      map[string]bool // the command name and its aliases
	summary   map[string]bool
	idWords   map[string]bool
	path      map[string]bool
	typed     map[string]bool // the words of the command path as written
	text      []string        // lowercased command path, summary, operationId and path
}

func newSearchDoc(op *Operation, groupTitle string) searchDoc {
	command := op.Group + " " + op.Name + " " + strings.Join(op.Aliases, " ")
	d := searchDoc{
		id:        strings.ToLower(op.ID),
		groupList: stems(splitWords(op.Group, false)),
		title:     set(stems(splitWords(groupTitle, false))),
		nameWords: stems(splitWords(op.Name, false)),
		summary:   set(stems(splitWords(op.Summary, false))),
		idWords:   set(stems(splitWords(op.ID, true))),
		path:      set(stems(splitWords(op.Path, false))),
		typed:     set(splitWords(command, false)),
		text:      []string{strings.ToLower(command), strings.ToLower(op.Summary), strings.ToLower(op.ID), strings.ToLower(op.Path)},
	}
	d.group = set(d.groupList)
	names := append([]string{}, d.nameWords...)
	for _, a := range op.Aliases {
		names = append(names, stems(splitWords(a, false))...)
	}
	d.name = set(names)
	return d
}

// listRank orders the list of a group's things first, then other lists.
func listRank(op *Operation) int {
	switch {
	case op.Name == "list":
		return 0
	case strings.HasPrefix(op.ID, "list"):
		return 1
	}
	return 2
}

// exactWords counts the query words the command path holds exactly as typed,
// so "moderation events" prefers moderation-events to moderation-event.
func (d searchDoc) exactWords(q query) int {
	n := 0
	for _, w := range q.words {
		for _, t := range w.terms {
			if d.typed[t.word] {
				n++
			}
		}
	}
	return n
}

// score sums the query words' matches and bonuses. When every word names the
// command's group, as "provider keys" names the keys group, only the group
// counts: the words are about the whole group, so a command whose name repeats
// them gets no more than the group's other commands.
func (d searchDoc) score(q query) (int, bool) {
	area := len(q.stems) > 0 && allIn(q.stems, d.group, d.title)
	total := 0
	for _, w := range q.words {
		if w.typed == d.id {
			total += weightOperationID
			continue
		}
		for _, t := range w.terms {
			var s int
			switch {
			case area && d.group[t.stem]:
				s = weightCommand
			case area:
				s = weightGroupTitle
			default:
				s = d.termScore(t)
			}
			if s == 0 {
				return 0, false
			}
			total += s
		}
	}
	switch {
	case area:
		total += bonusArea
	case len(q.stems) > 0 && allIn(q.stems, d.name) && len(d.nameWords) <= len(q.stems)+1:
		// The words name the command: they are all of its name, or all but one
		// word such as its verb.
		total += bonusName
	}
	if len(q.stems) > 0 && sameWords(q.stems, append(append([]string{}, d.groupList...), d.nameWords...)) {
		total += bonusExact
	}
	return total, true
}

func (d searchDoc) termScore(t queryTerm) int {
	switch {
	case d.group[t.stem] || d.name[t.stem]:
		return weightCommand
	case d.title[t.stem]:
		return weightGroupTitle
	case d.summary[t.stem]:
		return weightSummary
	case d.idWords[t.stem]:
		return weightIDWord
	case d.path[t.stem]:
		return weightPathWord
	}
	for _, text := range d.text {
		if strings.Contains(text, t.word) {
			return weightPartial
		}
	}
	return 0
}

// isIdentifier reports whether a typed word looks like an operationId, with a
// lowercase letter or digit followed by a capital, as in listProviderKeys.
func isIdentifier(s string) bool {
	runes := []rune(s)
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
			return true
		}
	}
	return false
}

// splitWords splits text into lowercase words at anything that is not a letter
// or digit; an identifier is also split where its words start with a capital.
func splitWords(s string, identifier bool) []string {
	if identifier {
		s = Kebab(s)
	}
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func stems(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = english.Stem(w, true)
	}
	return out
}

func set(words []string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// allIn reports whether every word is in at least one of the sets.
func allIn(words []string, sets ...map[string]bool) bool {
	for _, w := range words {
		found := false
		for _, s := range sets {
			if s[w] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sameWords(a, b []string) bool {
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		return false
	}
	return allIn(a, sb)
}
