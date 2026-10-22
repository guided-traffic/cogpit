package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A container's image grade in a pod's drawer (D-new; the owner, 2026-10-22: no section of its own,
// in each container's section, "als Symbol und als Detailansicht"; his pick in the Image Rating Lab:
// the shield after the container's name, and Docker Scout's card under its items). The card: the
// shield, the grade and the count that decides it, the rule it stands for, the counts as a severity
// bar (gradebadge.go), and a line with the image's OS (its end of life in the warning colour), when
// it was scanned and the report, a link to its object. N/A: the shield, its text and its rule.

// gradeMetaSep separates the parts of a grade card's last line.
const gradeMetaSep = " · "

// The shields' sides in the drawer, in points: after a container's name and on its card.
const (
	gradeTitleSide = 16
	gradeCardSide  = 32
)

// titleGrade is the shield after a container's name; its tooltip is the grade's text.
func (d *drawer) titleGrade(card *model.GradeCard) fyne.CanvasObject {
	return container.NewCenter(newGradeBadge(gradeTitleSide, card.Grade))
}

// gradeCard is a container's grade card. Its texts are rich texts, which keep an inner padding of
// their own: the bar takes the same padding, so that the card's lines start at one edge.
func (d *drawer) gradeCard(ref model.ObjectRef, card *model.GradeCard) fyne.CanvasObject {
	head := widget.NewRichText(
		&widget.TextSegment{Text: card.Text, Style: widget.RichTextStyle{ColorName: theme.ColorNameForeground, TextStyle: fyne.TextStyle{Bold: true}}},
		&widget.TextSegment{Text: card.Reason, Style: widget.RichTextStyle{ColorName: colorMuted, SizeName: theme.SizeNameCaptionText}},
	)
	head.Wrapping = fyne.TextWrapWord
	body := []fyne.CanvasObject{head}
	if card.Counts != nil {
		pad := d.sh.app.Settings().Theme().Size(theme.SizeNameInnerPadding)
		body = append(body, container.New(layout.NewCustomPaddedLayout(0, 0, pad, pad), newSeverityBar(*card.Counts)),
			d.gradeMeta(ref, card))
	}
	return container.NewBorder(nil, nil, container.NewCenter(newGradeBadge(gradeCardSide, card.Grade)), nil,
		container.NewVBox(body...))
}

// gradeMeta is the card's last line, wrapped as the drawer is wide: "OS alpine 3.20.10 · end of
// life · Scanned 3h ago (…) · " and the report's name, a link to its object where the cluster serves
// its kind.
func (d *drawer) gradeMeta(ref model.ObjectRef, card *model.GradeCard) fyne.CanvasObject {
	muted := widget.RichTextStyle{ColorName: colorMuted, Inline: true}
	var segs []widget.RichTextSegment
	add := func(text string, style widget.RichTextStyle) {
		if len(segs) > 0 {
			segs = append(segs, &widget.TextSegment{Text: gradeMetaSep, Style: muted})
		}
		segs = append(segs, &widget.TextSegment{Text: text, Style: style})
	}
	if card.OS != "" {
		add("OS "+card.OS, muted)
	}
	if card.EOSL {
		add("end of life", widget.RichTextStyle{ColorName: theme.ColorNameWarning, Inline: true})
	}
	if card.Scanned != "" {
		add("Scanned "+card.Scanned, muted)
	}
	if r := card.Report; r != nil {
		if d.sh.view.serves(r.Kind) {
			link := *r
			if len(segs) > 0 {
				segs = append(segs, &widget.TextSegment{Text: gradeMetaSep, Style: muted})
			}
			segs = append(segs, &widget.HyperlinkSegment{Text: r.Name, OnTapped: func() { d.sh.followLink(link, ref.Context, false) }})
		} else {
			add(r.Name, muted)
		}
	}
	meta := widget.NewRichText(segs...)
	meta.Wrapping = fyne.TextWrapWord
	return meta
}
