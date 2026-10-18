package fyneui

// The area under a chart's series (G12, ui-parity › Cluster overview and charts, night run
// 2026-10-16/17): Lens's metrics charts fill it (captured with the drawer's charts, "the chart's
// fill"); Cogpit's drew lines only. Each run of a series' points (a line between gaps,
// chartgap.go) is filled down to zero in the series' colour at chartFillAlpha, under the grid's
// reference lines, the event markers and the lines themselves, so those stay readable. The
// alpha and the fill of every series (the network's receive and transmit both) are Cogpit's: the
// lead's decisions for the owner to confirm in a real window (architecture §9).
//
// The fill is a path of the run's points closed along zero, rasterised antialiased in the chart's
// fill layer (chartpath.go, D-89): its top edge is the line's own path, so the line drawn over it
// covers the edge exactly, with no stair of pixels between the two.

// chartFillAlpha is the fill's opacity: a fifth of the line's colour.
const chartFillAlpha = 0x33
