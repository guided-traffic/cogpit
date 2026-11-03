package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// trivy.imageSecurity: the Image Security page's read for agents (D-new). Agents read what the
// page shows: the header's counts, the cards and, with findings, the table for a search; ui.openView
// "trivy/image-security" shows the page to the user. It reads Cogpit's cache only.

type imageSecurityParams struct {
	engine.OnCluster
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces to count; omitted: the namespace filter the user has set (every namespace for a cluster the window does not show)"`
	Findings   bool     `json:"findings,omitempty" jsonschema:"true: include the page's findings table, one row per CVE, workload and container"`
	Filter     *string  `json:"filter,omitempty" jsonschema:"the findings' search as the page's search field takes it: words any cell must contain, and the tokens sev>=critical|high|medium|low, sev:<severity>, fixable, ns:<namespace>, image:<text>, pkg:<text>, cve:<id>; omitted: sev>=high, the page's own; empty: every finding"`
	Limit      int      `json:"limit,omitempty" jsonschema:"maximum findings to return; default 100, at most 1000"`
}

// ImageSecurityResult is trivy.imageSecurity's result.
type ImageSecurityResult struct {
	engine.ImageSecurityModel
	// Findings is the page's table: Namespace, Workload, Container, Image, CVE, Severity, Score,
	// Package, Installed, Fixed; its rows cut at the limit. Only with findings: true.
	Findings *engine.TableSnapshot `json:"findings,omitempty"`
	// Matched counts the findings the search keeps, Returned those in Findings.
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
			"namespaces, each with the search token that narrows the table to it; with findings, the findings the search " +
			"keeps, each with its workload (ref: pass it to ui_navigate or object_get), container, image, CVE, severity, " +
			"score, package and its installed and fixed version. Needs the Trivy integration on for the cluster " +
			"(cluster_settings_get lists the integrations). ui_openView with view trivy/image-security shows the page; " +
			"ui_setFilter sets its search.",
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
	m, table, err := s.ImageSecurity(ctx, p.Context, p.Namespaces, p.Filter, p.Findings)
	if err != nil {
		return nil, err
	}
	res := ImageSecurityResult{ImageSecurityModel: *m}
	if table != nil {
		cut := *table
		res.Matched = len(table.Rows)
		cut.Rows = table.Rows[:min(len(table.Rows), limit)]
		res.Findings, res.Returned = &cut, len(cut.Rows)
	}
	return res, nil
}
