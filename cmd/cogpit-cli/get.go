package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// listLimit is the most rows one get prints: resources_list's maximum.
const listLimit = 1000

// defaultGetTimeout bounds connecting and the cache's first list.
const defaultGetTimeout = time.Minute

const getAbout = `Reads objects of one kind from a cluster, as Cogpit's views show them, and exits. It connects
through a headless engine, waits for the cache's first list of the kind and reads from it: the
commands resources_list (a list) and object_get (one object), dispatched with origin CLI. It
never writes to the cluster and never runs a credential plugin the user has not allowed in
Cogpit's window.

<kind> is a kind key as kinds_list names it: the plural for built-in kinds (pods, deployments),
<plural>.<group> for custom resources (certificates.cert-manager.io). A Kubernetes kind (Pod) or a
view's title (Pods) works too when it names one kind.

Output: table prints the kind's columns as Cogpit's view shows them (columns empty in every row
left out); json and yaml print resources_list's result for a list (reference, status and the cells
by column title) and the object's manifest for one object (managed fields dropped, Secret values
redacted).`

// getListParams are resources.list's parameters.
type getListParams struct {
	Kind       string   `json:"kind"`
	Namespaces []string `json:"namespaces,omitempty"`
	Filter     string   `json:"filter,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

// getObjectParams are object.get's parameters.
type getObjectParams struct {
	Ref model.ObjectRef `json:"ref"`
}

type getOptions struct {
	common
	context    string
	namespaces []string
	output     string
	filter     string
	kind, name string
}

func runGet(ctx context.Context, args []string, e *env) int {
	fs := newFlags("get", "<kind> [name] --context <context> [flags]", getAbout)
	var o getOptions
	o.register(fs)
	fs.StringVar(&o.context, "context", "", "the context to read: an id as 'cogpit-cli contexts' lists it (\"<context> [<file>]\") or a context name only one file defines; required")
	namespace := fs.String("n", "", "namespaces, comma-separated; empty: every namespace, or for a named object the context's namespace")
	fs.StringVar(namespace, "namespace", "", "same as -n")
	fs.StringVar(&o.output, "o", outTable, "output format: table, json or yaml")
	fs.StringVar(&o.output, "output", outTable, "same as -o")
	fs.StringVar(&o.filter, "filter", "", "search text as in the view's search box: every term must occur in a cell, case-insensitively")
	timeout := fs.Duration("timeout", defaultGetTimeout, "how long connecting and the cache's first list may take")
	if code, done := fs.parse(args, e); done {
		return code
	}
	switch {
	case len(fs.positional) == 0:
		return fs.usageError(e, "a kind is required, e.g. `cogpit-cli get pods --context <context>`")
	case len(fs.positional) > 2:
		return fs.usageError(e, "get takes a kind and at most one name")
	case o.context == "":
		return fs.usageError(e, "--context is required (`cogpit-cli contexts` lists them)")
	case !validOutput(o.output):
		return fs.usageError(e, fmt.Sprintf("unknown output format %q: want table, json or yaml", o.output))
	case *timeout <= 0:
		return fs.usageError(e, "--timeout must be positive")
	}
	o.kind = fs.positional[0]
	if len(fs.positional) == 2 {
		o.name = fs.positional[1]
	}
	for _, ns := range strings.Split(*namespace, ",") {
		if ns = strings.TrimSpace(ns); ns != "" && !slices.Contains(o.namespaces, ns) {
			o.namespaces = append(o.namespaces, ns)
		}
	}
	if o.name != "" && len(o.namespaces) > 1 {
		return fs.usageError(e, "a named object takes one namespace")
	}
	o.apply(e)

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	s, closeSession, err := e.newSession(sessionOptions{kubeconfig: o.kubeconfig})
	if err != nil {
		return fail(e, err)
	}
	defer closeSession()
	res, err := connect(ctx, s, o.context)
	if err != nil {
		return fail(e, err)
	}
	warnUnchecked(e, s, res)
	kind, err := resolveKind(ctx, s, o.kind)
	if err != nil {
		return fail(e, err)
	}
	if o.name != "" {
		err = getOne(ctx, e, s, kind, o)
	} else {
		err = getList(ctx, e, s, kind, o)
	}
	if err != nil {
		return fail(e, err)
	}
	return exitOK
}

// connect connects the session to a context with cluster.connect, as the window and agents do,
// and returns the command's result.
func connect(ctx context.Context, s *engine.Session, contextName string) (commands.ConnectResult, error) {
	var res commands.ConnectResult
	if _, err := dispatchInto(ctx, s, "cluster.connect", map[string]string{"context": contextName}, &res); err != nil {
		return res, err
	}
	if res.State != engine.Connected {
		msg := res.Error
		if msg == "" {
			msg = "the connection is " + string(res.State)
		}
		return res, fmt.Errorf("connect %s: %s", contextName, msg)
	}
	return res, nil
}

// warnUnchecked writes one warning line to standard error when the connection does not check the
// API server's certificate (insecure-skip-tls-verify; the owner's A29, H-37): the status bar's
// "TLS not checked" and the connect log's line, for a command that has neither. Standard output,
// the table or the JSON, stays as it is.
func warnUnchecked(e *env, s *engine.Session, res commands.ConnectResult) {
	if !res.TLSUnchecked {
		return
	}
	server := "its server"
	if b := s.Backend(); b != nil && b.Conn != nil {
		server = b.Conn.Server()
	}
	fmt.Fprintf(e.stderr, "cogpit-cli: warning: %s for %s. %s.\n", engine.TLSUncheckedLabel, safeText(res.Context),
		safeText(engine.InsecureWarning(server)))
}

// resolveKind finds the kind a user named: a kind key, else the one kind whose Kubernetes kind or
// title it names, case-insensitively. A name that is not a built-in kind's waits for the cluster's
// custom resource definitions first, bounded by ctx.
func resolveKind(ctx context.Context, s *engine.Session, name string) (commands.KindInfo, error) {
	b := s.Backend()
	if b == nil {
		return commands.KindInfo{}, engine.ErrNotConnected
	}
	if _, ok := b.Profile(name); !ok {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for !b.CustomKindsSettled() {
			select {
			case <-ctx.Done():
				return commands.KindInfo{}, fmt.Errorf("the cluster's custom resource definitions are still being listed: %w", ctx.Err())
			case <-tick.C:
			}
		}
	}
	var all []commands.KindInfo
	if _, err := dispatchInto(ctx, s, "kinds.list", nil, &all); err != nil {
		return commands.KindInfo{}, err
	}
	for _, k := range all {
		if k.Kind == name {
			return k, nil
		}
	}
	var matches []commands.KindInfo
	for _, k := range all {
		if strings.EqualFold(k.Kind, name) || strings.EqualFold(k.APIKind, name) || strings.EqualFold(k.Title, name) {
			matches = append(matches, k)
		}
	}
	keys := func(list []commands.KindInfo) string {
		out := make([]string, len(list))
		for i, k := range list {
			out[i] = k.Kind
		}
		return strings.Join(out, ", ")
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return commands.KindInfo{}, fmt.Errorf("%w %q; this cluster serves: %s", engine.ErrUnknownKind, name, keys(all))
	}
	return commands.KindInfo{}, fmt.Errorf("%q names several kinds; use one of their keys: %s", name, keys(matches))
}

func getList(ctx context.Context, e *env, s *engine.Session, kind commands.KindInfo, o getOptions) error {
	var res commands.ListResult
	data, err := dispatchInto(ctx, s, "resources.list", getListParams{
		Kind: kind.Kind, Namespaces: o.namespaces, Filter: o.filter, Limit: listLimit,
	}, &res)
	if err != nil {
		return err
	}
	if res.Total > res.Returned {
		fmt.Fprintf(e.stderr, "cogpit-cli: showing %d of %d %s, sorted by name; narrow them with -n or --filter\n",
			res.Returned, res.Total, kind.Title)
	}
	if o.output != outTable {
		return writeResult(e.stdout, o.output, data)
	}
	if len(res.Rows) == 0 {
		where := ""
		if kind.Namespaced && len(o.namespaces) > 0 {
			where = " in " + strings.Join(o.namespaces, ", ")
		}
		if o.filter != "" {
			where += " matching " + o.filter
		}
		fmt.Fprintf(e.stderr, "No %s found%s.\n", kind.Title, safeText(where))
		return nil
	}
	return writeRows(e, kind, res.RowTable)
}

func getOne(ctx context.Context, e *env, s *engine.Session, kind commands.KindInfo, o getOptions) error {
	ns := ""
	if kind.Namespaced {
		if len(o.namespaces) == 1 {
			ns = o.namespaces[0]
		} else if b := s.Backend(); b != nil && b.Namespace != "" {
			ns = b.Namespace
		} else {
			ns = "default"
		}
	}
	notFound := func() error {
		if ns != "" {
			return fmt.Errorf("%s %q not found in namespace %q", kind.Title, o.name, ns)
		}
		return fmt.Errorf("%s %q not found", kind.Title, o.name)
	}
	if o.output == outTable {
		params := getListParams{Kind: kind.Kind, Filter: o.name, Limit: listLimit}
		if ns != "" {
			params.Namespaces = []string{ns}
		}
		var res commands.ListResult
		if _, err := dispatchInto(ctx, s, "resources.list", params, &res); err != nil {
			return err
		}
		if i := res.Find(ns, o.name); i >= 0 {
			one := res.RowTable
			one.Rows = one.Rows[i : i+1]
			return writeRows(e, kind, one)
		}
		if res.Total > res.Returned {
			return fmt.Errorf("%s %q is not among the first %d whose cells contain its name; use -o yaml", kind.Title, o.name, res.Returned)
		}
		return notFound()
	}

	var m engine.DetailModel
	ref := model.ObjectRef{Kind: kind.Kind, Namespace: ns, Name: o.name}
	if _, err := dispatchInto(ctx, s, "object.get", getObjectParams{Ref: ref}, &m); err != nil {
		return err
	}
	switch {
	case m.Loading:
		return fmt.Errorf("%w %s yet", engine.ErrNotSynced, kind.Kind)
	case m.Missing:
		return notFound()
	case m.YAML == "":
		return errors.New("the object cannot be shown as YAML")
	}
	if o.output == outYAML {
		_, err := fmt.Fprint(e.stdout, m.YAML)
		return err
	}
	data, err := yaml.YAMLToJSON([]byte(m.YAML))
	if err != nil {
		return err
	}
	return writeJSON(e.stdout, json.RawMessage(data))
}

// writeRows prints rows under the kind's columns as its view shows them: the columns it does not
// hide by default, in its order, without the columns that are empty in every row (the metrics
// columns, which a headless read does not fill, and an unused warning column); the name column
// always.
func writeRows(e *env, kind commands.KindInfo, rows engine.RowTable) error {
	// The result's table names an object by its reference's columns; the kind's Name and Namespace
	// columns, which only repeat them, are not in it.
	column := func(c model.Column) string { return rows.ColumnOf(c.ID, c.Title) }
	var cols []model.Column
	for _, c := range kind.Columns {
		if c.Hidden {
			continue
		}
		used := c.ID == "name"
		for i := range rows.Rows {
			if rows.Cell(i, column(c)) != "" {
				used = true
				break
			}
		}
		if used {
			cols = append(cols, c)
		}
	}
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = strings.ToUpper(c.Title)
	}
	table := make([][]string, len(rows.Rows))
	for i := range rows.Rows {
		line := make([]string, len(cols))
		for j, c := range cols {
			line[j] = rows.Cell(i, column(c))
		}
		table[i] = line
	}
	return writeTable(e.stdout, header, table)
}
