//go:build nothreadcheck

package buildinfo

// FyneThreadChecks is whether Fyne's thread check runs (D-103): false, since the build
// passed Cogpit's tag nothreadcheck (the Makefile's FYNE_THREAD_CHECKS=off, a release's default),
// with which the app declares itself migrated to fyne.Do.
const FyneThreadChecks = false
