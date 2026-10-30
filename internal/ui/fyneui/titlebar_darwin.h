// Cogpit's title bar on macOS: the AppKit side of titlebar_darwin.go (see titlebar.go). Windows
// are passed as the NSWindow pointer Fyne hands out (driver.MacWindowContext), as an integer.

#include <stdint.h>

// cogpitTitlebarGeom is the title bar as AppKit lays it out, in points from the window's top left.
typedef struct {
	double buttonsMinX; // the close button's left edge
	double buttonsMaxX; // the zoom button's right edge
	double height;      // the title bar's height
	int fullScreen;
	int ok; // 0: the window has no standard buttons
} cogpitTitlebarGeom;

// cogpitWindowKeepContentPlaced keeps the window's last frame anchored at the top left while a
// resize is dragged, instead of scaled to the new size until the next frame is drawn.
void cogpitWindowKeepContentPlaced(uintptr_t window);
// cogpitWindowUseSRGB declares the window's content sRGB, the theme's colour space, so that AppKit
// converts every frame to the profile of the display the window is on.
void cogpitWindowUseSRGB(uintptr_t window);
// cogpitTitlebarInstall lets the window's content fill the title bar's area under a transparent
// title bar without a title, and reports layout changes (full screen, resizes, a new screen) to the
// Go function cogpitTitlebarChanged with id.
void cogpitTitlebarInstall(uintptr_t window, uintptr_t id);
// cogpitTitlebarCentreButtons centres the window buttons vertically in Cogpit's top bar, barHeight
// points high, and keeps them there through AppKit's layout passes; 0 leaves them where AppKit puts
// them.
void cogpitTitlebarCentreButtons(uintptr_t window, double barHeight);
// cogpitTitlebarGeometry reads the title bar's layout.
void cogpitTitlebarGeometry(uintptr_t window, cogpitTitlebarGeom *geom);
// cogpitTitlebarClickCount returns the click count of the event AppKit is dispatching when it is a
// left mouse-down in the window, else 0.
int cogpitTitlebarClickCount(uintptr_t window);
// cogpitTitlebarDrag starts dragging the window with the left mouse-down AppKit is dispatching.
void cogpitTitlebarDrag(uintptr_t window);
// cogpitTitlebarDoubleClickSetting reads "double-click a window's title bar to": the value of
// AppleActionOnDoubleClick into action (empty when unset), and whether the older
// AppleMiniaturizeOnDoubleClick is set and true.
void cogpitTitlebarDoubleClickSetting(char *action, int size, int *legacySet, int *legacyMinimize);
// cogpitTitlebarZoom zooms the window, cogpitTitlebarMiniaturize minimises it.
void cogpitTitlebarZoom(uintptr_t window);
void cogpitTitlebarMiniaturize(uintptr_t window);
