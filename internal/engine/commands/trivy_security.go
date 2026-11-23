package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// trivy.imageSecurity: the Image Security page's read for agents (D-178). Agents read what the
// page shows: the header's counts, the cards and, with table, the CVEs or the findings for a search;
// ui.openView "trivy/image-security" shows the page to the user, trivy.imageSecurityTable switches
// its table. It reads Cogpit's cache only.

type imageSecurityParams struct {
	engine.OnCluster
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces to count; omitted: the namespace filter the user has set (every namespace for a cluster the window does not show)"`
	Table      string   `json:"table,omitempty" jsonschema:"include one of the page's tables: cves, one row per CVE with how many distinct images (by digest; replicas and the same image in several workloads count once) and containers it is found in and which containers; findings, one row per CVE, workload and container; omitted: no table"`
	Filter     *string  `json:"filter,omitempty" jsonschema:"the table's search as the page's search field takes it: words any cell must contain, and the tokens sev>=critical|high|medium|low, sev:<severity>, fixable, ns:<namespace>, image:<text>, pkg:<text>, cve:<id>; omitted: sev>=high, the page's own; empty: every row"`
	Limit      int      `json:"limit,omitempty" jsonschema:"maximum rows to return; default 100, at most 1000"`
}

// ImageSecurityResult is trivy.imageSecurity's result.
type ImageSecurityResult struct {
	engine.ImageSecurityModel
	// Table is the table asked for, its rows cut at the limit: the CVEs (Severity, CVE, Score,
	// Package, Fixed, Images, Containers, Runs in) or the findings (Namespace, Workload, Container,
	// Image, CVE, Severity, Score, Package, Installed, Fixed).
	Table *engine.TableSnapshot `json:"table,omitempty"`
	// Matched counts the rows the search keeps, Returned those in Table.
	Matched  int `json:"matched,omitempty"`
	Returned int `json:"returned,omitempty"`
}

// imageSecurityReads are the reads behind trivy.imageSecurity: the page's watches of the reports
// and the pods.
var imageSecurityReads = []engine.Verb{
	{Verb: nwVerbList, Group: "aquasecurity.github.io", Resource: "vulnerabilityreports"},
	{Verb: nwVerbWatch, Group: "aquasecurity.github.io", Resource: "vulnerabilityreports"},
	{Verb: nwVerbList, Resource: kindPods},
	{Verb: nwVerbWatch, Resource: kindPods},
}

func registerTrivySecurity(g *registrar) {
	add(g, engine.Spec[imageSecurityParams]{
		ID: "trivy.imageSecurity", Label: engine.ImageSecurityTitle, Danger: engine.DangerRead, Verbs: imageSecurityReads,
		Description: "Reads the Image Security page, from Trivy Operator's VulnerabilityReports and the pods in Cogpit's " +
			"cache: the distinct images of the running pods, how many a report covers and how many not, the newest scan; " +
			"the distinct CVE ids by severity (each under the worst severity a finding gives it) and how many have a " +
			"fixed version; the five worst images (by grade S to E as the Pods list's Signals column shows it, then " +
			"Critical, then High), the five worst CVEs (by severity, then images affected) and the five worst " +
			"namespaces, each with the search token that narrows the table to it; with table cves, the CVEs the search " +
			"keeps, each with its severity, score, packages, fixed versions, the distinct images it is found in (an image is " +
			"its digest, else repository and tag: replicas and the same image in several workloads count once), the " +
			"workloads' containers that run them and their names (namespace/workload/container); with table findings, " +
			"the findings, each with its workload (ref: pass it to ui_navigate or object_get), container, image, CVE, " +
			"severity, score, package and its installed and fixed version. Needs the Trivy integration on for the cluster " +
			"(cluster_settings_get lists the integrations). ui_openView with view trivy/image-security shows the page " +
			"with its CVEs; trivy_imageSecurityTable switches its table; ui_setFilter sets its search.",
		Enabled: func(s *engine.Session, p imageSecurityParams) error {
			b, err := connectedOn(s, p.Context)
			if err != nil {
				return err
			}
			if u := s.UIState(); !u.IntegrationOn(b.Context, engine.IntegrationTrivy) {
				return engine.ErrTrivyOff
			}
			return nil
		},
		Run: readImageSecurity,
	})
}

func readImageSecurity(ctx context.Context, s *engine.Session, p imageSecurityParams) (any, error) {
	limit, err := limitOf(p.Limit)
	if err != nil {
		return nil, err
	}
	view, err := imageSecurityTableView(p.Table, true)
	if err != nil {
		return nil, err
	}
	m, table, err := s.ImageSecurity(ctx, p.Context, p.Namespaces, p.Filter, view)
	if err != nil {
		return nil, err
	}
	res := ImageSecurityResult{ImageSecurityModel: *m}
	if table != nil {
		cut := *table
		res.Matched = len(table.Rows)
		cut.Rows = table.Rows[:min(len(table.Rows), limit)]
		res.Table, res.Returned = &cut, len(cut.Rows)
	}
	return res, nil
}

// The page's tables as their parameter names them.
const (
	imageSecTableCVEs     = "cves"
	imageSecTableFindings = "findings"
)

// imageSecurityTableView is the page key of the table a parameter names; "" for none when empty
// is allowed.
func imageSecurityTableView(table string, emptyOK bool) (string, error) {
	switch table {
	case imageSecTableCVEs:
		return engine.ImageSecurity, nil
	case imageSecTableFindings:
		return engine.ImageSecurityFindings, nil
	case "":
		if emptyOK {
			return "", nil
		}
	}
	return "", fmt.Errorf("table is %s or %s, not %q", imageSecTableCVEs, imageSecTableFindings, table)
}

type imageSecurityTableParams struct {
	Table string `json:"table" jsonschema:"cves: one row per CVE; findings: one row per CVE, workload and container"`
	CVE   string `json:"cve,omitempty" jsonschema:"with table findings: show that CVE's findings (its cve: token in the search in place of one there)"`
}

func registerTrivySecurityTable(g *registrar) {
	add(g, engine.Spec[imageSecurityTableParams]{
		ID: "trivy.imageSecurityTable", Label: "Image Security Table", Danger: engine.DangerUI,
		Description: "Switches the open Image Security page between its tables, as its CVEs and Findings buttons and a " +
			"click on a CVE do: cves, one row per CVE; findings, one row per CVE, workload and container, with cve the " +
			"findings of that CVE. The search carries over (to cves without its cve: tokens); each table keeps its own " +
			"sort. The switch is a step of the tab's history: ui_tab_back returns. Needs the page open. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ imageSecurityTableParams) error {
			if !engine.IsImageSecurity(s.UIState().View) {
				return errors.New("the Image Security page is not the open view")
			}
			return nil
		},
		Run: func(_ context.Context, s *engine.Session, p imageSecurityTableParams) (any, error) {
			view, err := imageSecurityTableView(p.Table, false)
			if err != nil {
				return nil, err
			}
			if p.CVE != "" && view != engine.ImageSecurityFindings {
				return nil, errors.New("cve goes with table findings")
			}
			return s.ShowImageSecurityTable(view, strings.TrimSpace(p.CVE))
		},
	})
}

// ImageSecurityPDFResult is trivy.exportImageSecurityPdf's result.
type ImageSecurityPDFResult struct {
	Filename string `json:"filename"`
	CVEs     int    `json:"cves"`
	// PDF is the document (base64 in JSON).
	PDF []byte `json:"pdf"`
}

func registerTrivySecurityPDF(g *registrar) {
	add(g, engine.Spec[engine.OnCluster]{
		ID: "trivy.exportImageSecurityPdf", Label: "Export Image Security as PDF", Danger: engine.DangerRead, Verbs: imageSecurityReads,
		Description: "Renders the Image Security page as a PDF report, as the page's Export PDF button does: A4 landscape, " +
			"the header's counts, the severity band, the top images, CVEs and namespaces, the findings curve where " +
			"Prometheus has one, then every CVE the page's search keeps (the window's namespace filter and search; " +
			"sev>=high when the page is not open), each with all the containers it runs in and their installed and " +
			"fixed versions. Reads Cogpit's cache only. Returns {filename, cves, pdf} with pdf the document in base64.",
		Enabled: func(s *engine.Session, p engine.OnCluster) error {
			b, err := connectedOn(s, p.Context)
			if err != nil {
				return err
			}
			if u := s.UIState(); !u.IntegrationOn(b.Context, engine.IntegrationTrivy) {
				return engine.ErrTrivyOff
			}
			return nil
		},
		Run: func(ctx context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			doc, err := s.ImageSecurityPDF(ctx, p.Context)
			if err != nil {
				return nil, err
			}
			return ImageSecurityPDFResult{Filename: doc.Filename, CVEs: doc.CVEs, PDF: doc.PDF}, nil
		},
	})
}

type imageSecurityImageParams struct {
	Image string `json:"image" jsonschema:"the image as the Image Security page and a pod drawer's grade card name it: repository and tag without the registry, e.g. shop/api:2.4.1"`
}

func registerTrivySecurityImage(g *registrar) {
	add(g, engine.Spec[imageSecurityImageParams]{
		ID: "trivy.showImage", Label: "Show Image in Image Security", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Opens the Image Security page's CVEs searched for one image, every severity (image:<image>), as the " +
			"link on a container's grade card in a pod's drawer does: the CVEs of that image, each with the containers that " +
			"run it. A tag ends the match (shop/api:2.4.1 does not take 2.4.10). Needs the Trivy integration on. Returns " +
			"the new UI state.",
		Enabled: func(s *engine.Session, _ imageSecurityImageParams) error {
			b, err := connected(s)
			if err != nil {
				return err
			}
			if u := s.UIState(); !u.IntegrationOn(b.Context, engine.IntegrationTrivy) {
				return engine.ErrTrivyOff
			}
			return nil
		},
		Run: func(_ context.Context, s *engine.Session, p imageSecurityImageParams) (any, error) {
			return s.ShowImageSecurityImage(p.Image)
		},
	})
}
