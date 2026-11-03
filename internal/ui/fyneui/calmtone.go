package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/model"
)

// calmStatusColor is a status's colour on the integrations' pages (D-calm): an error's and a
// warning's own colour, every other status the muted text colour, so that colour marks only what
// needs action (red: act now, amber: check).
func calmStatusColor(s model.Status) fyne.ThemeColorName {
	switch s {
	case model.StatusError, model.StatusWarning:
		return statusColorName(s)
	}
	return colorMuted
}
