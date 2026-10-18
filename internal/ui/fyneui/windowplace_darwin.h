// The window's place on macOS: the AppKit side of windowplace_darwin.go (D-95, windowplace.go).
// Windows are passed as the NSWindow pointer Fyne hands out (driver.MacWindowContext), as an
// integer. Rectangles are in AppKit's screen coordinates: points, origin at the bottom left of the
// primary display, y up.

#include <stdint.h>

typedef struct {
	double x, y, width, height;
} cogpitPlaceRect;

// cogpitPlaceScreen is one display: its id (NSScreenNumber), its name, its frame and its visible
// frame (without the menu bar and the Dock). id 0: none.
typedef struct {
	uint32_t id;
	char name[128];
	cogpitPlaceRect frame;
	cogpitPlaceRect visible;
} cogpitPlaceScreen;

// cogpitPlaceState is the window's place as reported: its frame outside full screen (in full
// screen the one before it), whether it is in full screen, and the display it is on.
typedef struct {
	cogpitPlaceRect frame;
	int fullScreen;
	cogpitPlaceScreen display;
} cogpitPlaceState;

// cogpitWindowPlaceScreens describes up to max displays into screens, the primary display first,
// and returns how many there are.
int cogpitWindowPlaceScreens(cogpitPlaceScreen *screens, int max);
// cogpitWindowPlaceTitleBarHeight is the title bar's height of the window GLFW creates (titled,
// closable, miniaturizable, resizable): its frame less its content.
double cogpitWindowPlaceTitleBarHeight(void);
// cogpitWindowPlaceFollow gives the window the frame (width 0: leaves it), then reports every
// change of its place to the Go function cogpitWindowPlaceChanged with id, and enters full screen
// when fullScreen is set. Called again, it reports under the new id.
void cogpitWindowPlaceFollow(uintptr_t window, uintptr_t id, cogpitPlaceRect frame, int fullScreen);
// cogpitWindowPlaceState is the place the window would report now.
void cogpitWindowPlaceState(uintptr_t window, cogpitPlaceState *state);
