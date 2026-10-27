package engine

import (
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The alert board's cards (the owner, 2026-10-27: first the severity, "danach eine kurze Beschreibung
// des Problems und dann schon eine schnell zu erfassende Liste der konkreten Instanzen", and "vermeiden
// den gleichen Text oder Description zweimal auszugeben"): a card is one alertname of one severity,
// its text once and its alerts as the rows of a table of what tells them apart. finishCard picks the
// columns (what differs between the alerts; with one alert, the object and what its text names),
// puts what all alerts share into the card's head (Context), and makes one text of the alerts'
// summaries and one of their descriptions (cardText): a column's value or a number that differs
// between them becomes a slot each row fills. A text that one card text and a row's values do not
// give back exactly stays that row's own (Message, Detail): the board never shows a text the cluster
// did not send.

// maxCardColumns bounds a card's label columns (the namespace, the object, the container and the
// labels that tell its alerts apart); maxValueColumns its number columns. A card is a column of the
// board's masonry: more do not fit beside each other.
const (
	maxCardColumns  = 4
	maxValueColumns = 2
)

// The labels, kinds and titles the cards read by name.
const (
	labelPod       = "pod"
	labelNode      = "node"
	labelInstance  = "instance"
	labelService   = "service"
	labelContainer = ParamContainer
	labelCRName    = "name" // kube-state-metrics' custom resource's name
	groupApps      = "apps"
	resourcePods   = "pods"
	titleNamespace = "Namespace"
)

// plumbingLabels name the scrape and the alerting pipeline rather than what an alert is about: never
// a column. The namespace and the object are columns of their own (cardColumns).
var plumbingLabels = labelSet("alertname severity job endpoint service prometheus prometheus_replica uid metrics_path " +
	"receiver namespace customresource_group customresource_kind customresource_version")

// shortValueKeys are the columns whose values stand for themselves in a text even when shorter than
// three characters (a mount point "/", a device "sda"): any other short value ("1", "ok") is too
// likely a word or a number of the text.
var shortValueKeys = labelSet("namespace object container node device mountpoint instance")

// labelOrder ranks the label columns after the namespace, the object and the container: the ones
// that locate an alert first.
var labelOrder = strings.Fields("node nodename hostname host instance device mountpoint")

// labelSet is a set of the names in a space-separated list.
func labelSet(names string) map[string]bool {
	out := map[string]bool{}
	for _, n := range strings.Fields(names) {
		out[n] = true
	}
	return out
}

// cardLabels are an alert's labels as the board reads them: an exported_<label> (what Prometheus
// writes when a scraped label clashes with one of the target's, honor_labels false) stands for
// <label>, whose value is then the scrape target's (the exporter's namespace).
func cardLabels(in map[string]string) map[string]string {
	out := maps.Clone(in)
	for k, v := range in {
		if l, ok := strings.CutPrefix(k, "exported_"); ok && l != "" && v != "" {
			out[l] = v
			delete(out, k)
		}
	}
	return out
}

// finishCard makes a card of the alerts grouped into it: its shared severity label and runbook, its
// head's objects, its columns and the rows' values, and its texts. ownerOf finds the object that
// controls an alert's pod (nil: none known).
func finishCard(c *AlertCard, ownerOf func(AlertObject) *AlertObject) {
	items := c.Alerts
	if len(items) == 0 {
		return
	}
	c.SeverityLabel = items[0].SeverityLabel
	c.Runbook = items[0].Runbook
	for _, it := range items[1:] {
		if it.SeverityLabel != c.SeverityLabel {
			c.SeverityLabel = ""
		}
		if it.Runbook != c.Runbook {
			c.Runbook = ""
		}
	}
	values := make([]map[string]string, len(items))
	for i := range values {
		values[i] = map[string]string{}
	}
	c.Context, c.Columns = cardColumns(items, values, ownerOf)
	keys := make([]string, 0, len(c.Columns))
	for _, col := range c.Columns {
		keys = append(keys, col.Key)
	}
	summaries := make([]string, len(items))
	descriptions := make([]string, len(items))
	for i, it := range items {
		summaries[i], descriptions[i] = it.Summary, it.Description
	}
	var own [2][]string
	c.Summary, own[0] = cardText(summaries, values, keys, c)
	c.Description, own[1] = cardText(descriptions, values, keys, c)
	for i := range items {
		it := &items[i]
		it.Message, it.Detail = own[0][i], own[1][i]
		it.Summary, it.Description = "", ""
		if c.Runbook != "" {
			it.Runbook = ""
		}
		if len(values[i]) > 0 {
			it.Values = values[i]
		}
	}
}

// cardColumns picks a card's head objects and its rows' columns, and writes each row's values into
// values: the namespace (in the head when all share it), the object the alerts are about (in the
// head when several alerts share one), the object that controls their pods when all share one (a
// CloudNativePG Cluster, a Deployment), the pod's container, and the labels that tell the alerts
// apart; with one alert, the labels its texts name.
func cardColumns(items []AlertItem, values []map[string]string, ownerOf func(AlertObject) *AlertObject) (head []AlertObject, cols []AlertColumn) {
	n := len(items)
	ns := make([]string, n)
	subj := make([]*AlertObject, n)
	for i, it := range items {
		for j, o := range it.Objects {
			if o.Label == namespaceKey {
				ns[i] = o.Ref.Name
			} else {
				subj[i] = &it.Objects[j]
			}
		}
		if subj[i] == nil && scrapedPod(it.labels[labelPod], it.labels) {
			// The namespace of the exporter that was scraped (node-exporter's), not of anything the
			// alert is about: the search and the namespace filter still find it there.
			ns[i] = ""
		}
	}
	switch {
	case same(ns, func(s string) string { return s }) && ns[0] != "":
		for _, o := range items[0].Objects {
			if o.Label == namespaceKey {
				head = append(head, o)
			}
		}
	case slices.ContainsFunc(ns, func(s string) bool { return s != "" }):
		cols = append(cols, AlertColumn{Key: ColumnNamespace, Title: titleNamespace, Link: ColumnNamespace})
		for i, v := range ns {
			values[i][ColumnNamespace] = v
		}
	}

	taken := map[string]bool{} // the labels the object and the container stand for
	first := slices.IndexFunc(subj, func(o *AlertObject) bool { return o != nil })
	if first >= 0 {
		for _, o := range subj {
			if o != nil {
				taken[subjectLabel(*o)] = true
			}
		}
		refs := func(o *AlertObject) string {
			if o == nil {
				return ""
			}
			return o.Ref.Kind + "\x00" + o.Ref.Namespace + "\x00" + o.Ref.Name
		}
		if n > 1 && same(subj, refs) {
			head = append(head, *subj[0])
		} else {
			cols = append(cols, AlertColumn{Key: ColumnObject, Title: subj[first].Title, Link: ColumnObject})
			for i, o := range subj {
				if o != nil {
					values[i][ColumnObject] = o.Ref.Name
				}
			}
		}
		if subj[first].Label == labelPod && ownerOf != nil {
			if owner := sharedOwner(subj, ownerOf); owner != nil {
				head = append(head, *owner)
			}
		}
		if subj[first].Label == labelPod {
			taken[labelContainer] = true
			containers := make([]string, n)
			for i, o := range subj {
				if o != nil {
					containers[i] = o.Container
				}
			}
			if slices.ContainsFunc(containers, func(s string) bool { return s != "" }) && (n == 1 || !same(containers, func(s string) string { return s })) {
				cols = append(cols, AlertColumn{Key: labelContainer, Title: labelContainer})
				for i, v := range containers {
					values[i][labelContainer] = v
				}
			}
		}
	}

	var labels []string
	for _, l := range candidateLabels(items, taken, first >= 0) {
		vals := make([]string, n)
		for i, it := range items {
			vals[i] = it.labels[l]
		}
		if n > 1 && same(vals, func(s string) string { return s }) {
			continue // the same for all: says nothing about one of them
		}
		if n == 1 && !namedIn(vals[0], l, items[0].Summary, items[0].Description) {
			continue
		}
		if repeats(vals, ns, subj) {
			continue // the namespace or the object again (x509-certificate-exporter's secret_namespace)
		}
		labels = append(labels, l)
	}
	for _, l := range labels {
		if len(cols) >= maxCardColumns {
			break
		}
		cols = append(cols, AlertColumn{Key: l, Title: l})
		for i, it := range items {
			values[i][l] = it.labels[l]
		}
	}
	return head, cols
}

// candidateLabels are the labels that may be a card's columns, in their order (labelOrder, then by
// name): its alerts' labels but the pipeline's, the object's and the container's (taken), the
// scraped exporter's pod and container, and the scrape target's instance while the alerts name an
// object.
func candidateLabels(items []AlertItem, taken map[string]bool, haveObject bool) []string {
	set := map[string]bool{}
	for _, it := range items {
		scraped := scrapedPod(it.labels[labelPod], it.labels)
		for k := range it.labels {
			switch {
			case plumbingLabels[k], taken[k], strings.HasPrefix(k, "__"):
			case (k == labelPod || k == labelContainer) && scraped:
			case k == labelInstance && haveObject:
			default:
				set[k] = true
			}
		}
	}
	out := slices.Sorted(maps.Keys(set))
	rank := func(l string) int {
		if i := slices.Index(labelOrder, l); i >= 0 {
			return i
		}
		return len(labelOrder)
	}
	slices.SortStableFunc(out, func(a, b string) int { return rank(a) - rank(b) })
	return out
}

// sharedOwner is the object that controls all the alerts' pods, nil when one has none known or they
// differ.
func sharedOwner(subj []*AlertObject, ownerOf func(AlertObject) *AlertObject) *AlertObject {
	var shared *AlertObject
	for _, o := range subj {
		if o == nil {
			return nil
		}
		owner := ownerOf(*o)
		if owner == nil || shared != nil && owner.Ref != shared.Ref {
			return nil
		}
		shared = owner
	}
	return shared
}

// repeats reports whether a label's values are each its alert's namespace or object name.
func repeats(vals, ns []string, subj []*AlertObject) bool {
	for i, v := range vals {
		if v != ns[i] && (subj[i] == nil || v != subj[i].Ref.Name) {
			return false
		}
	}
	return true
}

// subjectLabel is the label an alert's object is named by: the label of its kind ("pod",
// "deployment"), kube-state-metrics' "name" of a custom resource, x509-certificate-exporter's
// "secret_name".
func subjectLabel(o AlertObject) string {
	switch o.Label {
	case "customresource":
		return labelCRName
	case "secret":
		return "secret_name"
	}
	return o.Label
}

// same reports whether key gives the same for every element of xs.
func same[T any](xs []T, key func(T) string) bool {
	for _, x := range xs[1:] {
		if key(x) != key(xs[0]) {
			return false
		}
	}
	return true
}

// namedIn reports whether a label's value stands in one of the texts as a word of its own.
func namedIn(value, key string, texts ...string) bool {
	if !slotValue(value, key) {
		return false
	}
	for _, t := range texts {
		if len(valueAt(t, value)) > 0 {
			return true
		}
	}
	return false
}

// slotValue reports whether a value may become a slot of a card's text: three characters or more,
// or a short value of a column that names a thing (shortValueKeys); never true or false.
func slotValue(value, key string) bool {
	switch {
	case value == "", strings.EqualFold(value, "true"), strings.EqualFold(value, "false"):
		return false
	case len(value) >= 3:
		return true
	}
	return shortValueKeys[key]
}

// valueAt returns where a value stands in a text as a word of its own: not inside a longer name
// (letters, digits, '_' and '-' around it).
func valueAt(text, value string) []int {
	var at []int
	for i := 0; ; {
		j := strings.Index(text[i:], value)
		if j < 0 {
			return at
		}
		start, end := i+j, i+j+len(value)
		if (start == 0 || !nameByte(text[start-1])) && (end == len(text) || !nameByte(text[end])) {
			at = append(at, start)
		}
		i = start + 1
	}
}

func nameByte(b byte) bool {
	return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// slotMark delimits a slot in a text being made a card's text: "\x00pod\x00".
const slotMark = "\x00"

// cardText makes one text of a card's alerts' texts (their summaries, or their descriptions): the
// rows' values of the columns (keys, in the order they win a tie) become slots where a text holds
// them; numbers that still differ become value slots (new columns of c, their values written into
// values). It returns the card's text and, per alert, its own text where the card's does not give it
// back ("" where it does). The card's text is then the one most of the alerts share.
func cardText(texts []string, values []map[string]string, keys []string, c *AlertCard) ([]AlertTextPart, []string) {
	own := make([]string, len(texts))
	if !slices.ContainsFunc(texts, func(t string) bool { return t != "" }) {
		return nil, own
	}
	marked := make([]string, len(texts))
	for i, t := range texts {
		marked[i] = markSlots(t, values[i], keys)
	}
	if !same(marked, func(s string) string { return s }) {
		if tmpl, nums, ok := numberSlots(marked); ok && addValueColumns(c, values, nums, &tmpl) {
			marked = []string{tmpl}
		}
	}
	// The text most alerts share; the others keep their own.
	counts := map[string]int{}
	best := marked[0]
	for _, m := range marked {
		counts[m]++
		if counts[m] > counts[best] {
			best = m
		}
	}
	parts := textParts(best)
	for i, t := range texts {
		if fill(parts, values[i]) != t {
			own[i] = t
		}
	}
	return parts, own
}

// markSlots marks in a text where a column's value stands as a word of its own, the longest value
// first at a place, then the column first in keys.
func markSlots(text string, vals map[string]string, keys []string) string {
	type match struct {
		start, end, rank int
		key              string
	}
	var ms []match
	for r, k := range keys {
		v := vals[k]
		if !slotValue(v, k) {
			continue
		}
		for _, at := range valueAt(text, v) {
			ms = append(ms, match{at, at + len(v), r, k})
		}
	}
	if len(ms) == 0 {
		return text
	}
	slices.SortFunc(ms, func(a, b match) int {
		if a.start != b.start {
			return a.start - b.start
		}
		if a.end != b.end {
			return b.end - a.end
		}
		return a.rank - b.rank
	})
	var out strings.Builder
	at := 0
	for _, m := range ms {
		if m.start < at {
			continue // inside a longer value already marked
		}
		out.WriteString(text[at:m.start])
		out.WriteString(slotMark + m.key + slotMark)
		at = m.end
	}
	out.WriteString(text[at:])
	return out.String()
}

var (
	tokenRE  = regexp.MustCompile(`\s+|\S+`)
	numberRE = regexp.MustCompile(`^[-+]?[0-9][0-9.,]*$`)
)

// numberSlots finds the one text of marked texts that differ only in numbers: words in the same
// places, where the ones that differ are a number each between text all share ("3.2%" and "12.5%"
// are 3.2 and 12.5 before "%"). It returns the text with a slot per such place ("\x00#0\x00"),
// each text's numbers by place, and false when the texts differ otherwise or in more places than
// maxValueColumns.
func numberSlots(marked []string) (string, [][]string, bool) {
	toks := make([][]string, len(marked))
	for i, m := range marked {
		toks[i] = tokenRE.FindAllString(m, -1)
		if len(toks[i]) != len(toks[0]) {
			return "", nil, false
		}
	}
	var out strings.Builder
	var nums [][]string
	for p := range toks[0] {
		col := make([]string, len(toks))
		for i := range toks {
			col[i] = toks[i][p]
		}
		if same(col, func(s string) string { return s }) {
			out.WriteString(col[0])
			continue
		}
		if len(nums) == maxValueColumns {
			return "", nil, false
		}
		pre, suf := commonEnds(col)
		mid := make([]string, len(col))
		for i, t := range col {
			mid[i] = t[len(pre) : len(t)-len(suf)]
			if !numberRE.MatchString(mid[i]) {
				return "", nil, false
			}
		}
		out.WriteString(pre + slotMark + "#" + strconv.Itoa(len(nums)) + slotMark + suf)
		nums = append(nums, mid)
	}
	return out.String(), nums, true
}

// commonEnds returns the text the words all begin and all end with, never cutting into a number
// ("12" and "15" share no "1").
func commonEnds(words []string) (pre, suf string) {
	pre, suf = words[0], words[0]
	for _, w := range words[1:] {
		for !strings.HasPrefix(w, pre) {
			pre = pre[:len(pre)-1]
		}
		for !strings.HasSuffix(w, suf) {
			suf = suf[1:]
		}
	}
	for pre != "" && strings.ContainsRune("0123456789.,+-", rune(pre[len(pre)-1])) {
		pre = pre[:len(pre)-1]
	}
	for suf != "" && strings.ContainsRune("0123456789.,", rune(suf[0])) {
		suf = suf[1:]
	}
	short := len(words[0])
	for _, w := range words {
		short = min(short, len(w))
	}
	if len(pre)+len(suf) > short { // a word that is the other's prefix and suffix at once
		suf = ""
	}
	return pre, suf
}

// addValueColumns gives the numbers numberSlots found columns of the card ("value:1", "value:2"; a
// column that holds the same numbers already serves again), writes each alert's into values, and
// names the slots in tmpl after them. It reports false, and changes nothing, when there is no room
// for them.
func addValueColumns(c *AlertCard, values []map[string]string, nums [][]string, tmpl *string) bool {
	have := 0
	for _, col := range c.Columns {
		if isValueColumn(col.Key) {
			have++
		}
	}
	keys := make([]string, len(nums))
	var added []string
	for p, ns := range nums {
		for _, col := range c.Columns {
			if isValueColumn(col.Key) && slices.EqualFunc(values, ns, func(v map[string]string, n string) bool { return v[col.Key] == n }) {
				keys[p] = col.Key
				break
			}
		}
		if keys[p] == "" {
			if have == maxValueColumns {
				return false
			}
			have++
			keys[p] = valueColumn + strconv.Itoa(have)
			added = append(added, keys[p])
		}
	}
	for p, ns := range nums {
		for i, n := range ns {
			values[i][keys[p]] = n
		}
		*tmpl = strings.Replace(*tmpl, slotMark+"#"+strconv.Itoa(p)+slotMark, slotMark+keys[p]+slotMark, 1)
	}
	for _, k := range added {
		c.Columns = append(c.Columns, AlertColumn{Key: k, Title: "value"})
	}
	return true
}

// valueColumn begins the key of a number column: "value:1". A label's name has no colon, so it
// never takes a label's key.
const valueColumn = "value:"

func isValueColumn(key string) bool { return strings.HasPrefix(key, valueColumn) }

// textParts turns a marked text into a card's text parts.
func textParts(marked string) []AlertTextPart {
	var out []AlertTextPart
	for i, piece := range strings.Split(marked, slotMark) {
		switch {
		case piece == "":
		case i%2 == 1:
			out = append(out, AlertTextPart{Slot: piece})
		default:
			out = append(out, AlertTextPart{Text: piece})
		}
	}
	return out
}

// fill writes a row's values into a card's text: the text the row's alert stands for.
func fill(parts []AlertTextPart, vals map[string]string) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Slot != "" {
			b.WriteString(vals[p.Slot])
		} else {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
