#include <stdbool.h>
#include <stddef.h>

// What cogpitSparkleLoad found (sparkle_darwin.go maps them to the package's errors).
enum {
	COGPIT_SPARKLE_OK = 0,
	COGPIT_SPARKLE_NO_BUNDLE = 1,     // the process is not an app bundle's executable
	COGPIT_SPARKLE_NOT_INSTALLED = 2, // the bundle runs from a read-only volume or translocated
	COGPIT_SPARKLE_NO_FRAMEWORK = 3,  // no Contents/Frameworks/Sparkle.framework
	COGPIT_SPARKLE_NO_FEED = 4,       // no SUFeedURL in Info.plist
	COGPIT_SPARKLE_NO_KEY = 5,        // no SUPublicEDKey in Info.plist
	COGPIT_SPARKLE_LOAD_FAILED = 6,   // the framework or its controller class did not load
};

// The updater's states (cogpitSparkleState).
enum {
	COGPIT_SPARKLE_IDLE = 0,
	COGPIT_SPARKLE_STARTING = 1,
	COGPIT_SPARKLE_RUNNING = 2,
	COGPIT_SPARKLE_FAILED = 3,
	COGPIT_SPARKLE_CREATED = 4, // the controller made but not started (the test harness)
};

// The most of an update's version the delegate hands to cogpitSparkleUpdateChanged.
enum { COGPIT_SPARKLE_VERSION_MAX = 64 };

int cogpitSparkleLoad(char *detail, size_t detailSize);
void cogpitSparkleStart(bool startUpdater);
void cogpitSparkleCheck(void);
void cogpitSparkleProbe(void);
int cogpitSparkleState(char *detail, size_t detailSize);
int cogpitSparkleAutomaticChecks(void);
int cogpitSparkleSessionInProgress(void);
