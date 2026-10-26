// Sparkle 2 loaded at run time: the AppKit side of sparkle_darwin.go. Cogpit's binary does not
// link Sparkle, so its headers are not used either: the protocols below declare the few methods of
// SPUStandardUpdaterController, SPUUpdater and SUAppcastItem that Cogpit calls, and the delegate
// implements those of SPUUpdaterDelegate and SPUStandardUserDriverDelegate it needs, as Sparkle
// 2.10's headers declare them, so that the compiler knows their argument and return types.
// Compiled without ARC, like Fyne's Objective-C: the controller is made once and kept for the
// process's life.

#import <Cocoa/Cocoa.h>
#include <stdatomic.h>
#include <string.h>

#include "sparkle_darwin.h"

@protocol CogpitSPUUpdater <NSObject>
@property (nonatomic) BOOL automaticallyChecksForUpdates;
@property (nonatomic, readonly) BOOL canCheckForUpdates;
@property (nonatomic, readonly) BOOL sessionInProgress;
- (BOOL)startUpdater:(NSError **)error;
- (void)checkForUpdateInformation;
@end

// SUAppcastItem, the update a check found: only its version is read.
@protocol CogpitSUAppcastItem <NSObject>
@property (nonatomic, copy, readonly) NSString *displayVersionString;
@end

// SPUUpdateCheckUpdateInformation of Sparkle's SPUUpdateCheck: the probing check's kind.
enum { CogpitUpdateCheckUpdateInformation = 2 };

// The Go side's report of a newer release a check found, or of none (sparkle_darwin.go's export;
// the test harness defines its own).
extern void cogpitSparkleUpdateChanged(bool available, char *version, int length);

static void reportUpdate(bool available, id item) {
	char version[COGPIT_SPARKLE_VERSION_MAX + 1] = {0};
	if (available && [item respondsToSelector:@selector(displayVersionString)]) {
		NSString *v = [(id<CogpitSUAppcastItem>)item displayVersionString];
		if ([v isKindOfClass:[NSString class]] && v.UTF8String != NULL) {
			strlcpy(version, v.UTF8String, sizeof version);
		}
	}
	cogpitSparkleUpdateChanged(available, version, (int)strlen(version));
}

static BOOL probing;      // a probe of cogpitSparkleProbe runs; main thread only
static BOOL checkWaiting; // the user asked for a check while it ran; main thread only
static void checkOnMain(void);

// CogpitSparkleDelegate is the updater's delegate and its standard user driver's, as
// SPUUpdaterDelegate and SPUStandardUserDriverDelegate in Sparkle 2.10's headers declare their
// methods. Sparkle's own scheduled checks are off (startOnMain): one that finds a release holds it
// in an update session until the user acts on it, and while that session lasts Sparkle checks no
// more, neither on its schedule nor when the user asks (checkForUpdates: brings the held release
// back up), so a copy that runs for days offers the first release it found however many followed
// (the owner's report of 2026-10-26: 0.48.2 offered while 0.49.1 was out). Cogpit's scheduled
// checks are Sparkle's probing checks instead (cogpitSparkleProbe, every hour from
// sparkle_darwin.go): each reads the feed, reports what it found here and ends, so the window's
// Update button names the newest release (F7), and the button's click and Help › Check for
// Updates… read the feed again and bring Sparkle's window up with what it offers then. Sparkle
// calls these on the main thread.
@interface CogpitSparkleDelegate : NSObject
@end

@implementation CogpitSparkleDelegate
// A check found a newer release: a probe, or a check the user started.
- (void)updater:(id)updater didFindValidUpdate:(id)item {
	reportUpdate(true, item);
}

// A check found none newer (a probe leaves out the release the user skipped).
- (void)updaterDidNotFindUpdate:(id)updater error:(NSError *)error {
	reportUpdate(false, nil);
}

// A check ended. A probe's end runs the check the user asked for while it ran: Sparkle starts none
// while another runs.
- (void)updater:(id)updater didFinishUpdateCycleForUpdateCheck:(NSInteger)updateCheck error:(NSError *)error {
	if (updateCheck != CogpitUpdateCheckUpdateInformation || !probing) {
		return;
	}
	probing = NO;
	if (checkWaiting) {
		checkWaiting = NO;
		dispatch_async(dispatch_get_main_queue(), ^{
			checkOnMain();
		});
	}
}

// The update session of a check the user started ended: the release installed, skipped, put off
// with "Remind Me Later", or the check failed. The button goes until the next probe finds it again.
- (void)standardUserDriverWillFinishUpdateSession {
	reportUpdate(false, nil);
}
@end

@protocol CogpitSPUStandardUpdaterController <NSObject>
- (instancetype)initWithStartingUpdater:(BOOL)startUpdater updaterDelegate:(id)updaterDelegate userDriverDelegate:(id)userDriverDelegate;
@property (nonatomic, readonly) id<CogpitSPUUpdater> updater;
- (void)checkForUpdates:(id)sender;
@end

static Class controllerClass;                                // set once by cogpitSparkleLoad
static id<CogpitSPUStandardUpdaterController> controller;    // main thread only
static CogpitSparkleDelegate *sparkleDelegate;               // kept: Sparkle holds its delegates weakly
static _Atomic int state = COGPIT_SPARKLE_IDLE;
static char failure[512];                                    // written before state is FAILED

static void copyText(NSString *text, char *buf, size_t size) {
	if (buf == NULL || size == 0) {
		return;
	}
	const char *s = text.UTF8String;
	strlcpy(buf, s != NULL ? s : "", size);
}

// cogpitSparkleLoad checks the running app's bundle and loads Sparkle.framework from it: an app
// bundle, installed (not on a read-only volume such as its disk image, not translocated), with the
// framework in Contents/Frameworks and SUFeedURL and SUPublicEDKey in its Info.plist; only then is
// the framework loaded (NSBundle dlopens it) and SPUStandardUpdaterController looked up. Nothing
// is started. Any thread.
int cogpitSparkleLoad(char *detail, size_t detailSize) {
	@autoreleasepool {
		NSBundle *main = [NSBundle mainBundle];
		NSString *path = main.bundlePath;
		if (![path.pathExtension isEqualToString:@"app"] || main.bundleIdentifier == nil) {
			copyText(path, detail, detailSize);
			return COGPIT_SPARKLE_NO_BUNDLE;
		}
		if ([path rangeOfString:@"/AppTranslocation/"].location != NSNotFound) {
			copyText(path, detail, detailSize);
			return COGPIT_SPARKLE_NOT_INSTALLED;
		}
		NSNumber *readOnly = nil;
		if ([[NSURL fileURLWithPath:path] getResourceValue:&readOnly forKey:NSURLVolumeIsReadOnlyKey error:NULL] &&
			readOnly.boolValue) {
			copyText(path, detail, detailSize);
			return COGPIT_SPARKLE_NOT_INSTALLED;
		}
		NSString *framework = [main.privateFrameworksPath stringByAppendingPathComponent:@"Sparkle.framework"];
		if (framework == nil || ![[NSFileManager defaultManager] fileExistsAtPath:framework]) {
			copyText(framework, detail, detailSize);
			return COGPIT_SPARKLE_NO_FRAMEWORK;
		}
		id feed = [main objectForInfoDictionaryKey:@"SUFeedURL"];
		if (![feed isKindOfClass:[NSString class]] || [(NSString *)feed length] == 0) {
			return COGPIT_SPARKLE_NO_FEED;
		}
		id key = [main objectForInfoDictionaryKey:@"SUPublicEDKey"];
		if (![key isKindOfClass:[NSString class]] || [(NSString *)key length] == 0) {
			return COGPIT_SPARKLE_NO_KEY;
		}
		NSBundle *sparkle = [NSBundle bundleWithPath:framework];
		NSError *err = nil;
		if (sparkle == nil || ![sparkle loadAndReturnError:&err]) {
			copyText(err != nil ? err.localizedDescription : @"not a bundle", detail, detailSize);
			return COGPIT_SPARKLE_LOAD_FAILED;
		}
		Class c = NSClassFromString(@"SPUStandardUpdaterController");
		if (c == Nil) {
			copyText(@"Sparkle.framework has no SPUStandardUpdaterController", detail, detailSize);
			return COGPIT_SPARKLE_LOAD_FAILED;
		}
		controllerClass = c;
		return COGPIT_SPARKLE_OK;
	}
}

static void fail(NSString *why) {
	copyText(why, failure, sizeof failure);
	atomic_store(&state, COGPIT_SPARKLE_FAILED);
}

// startOnMain makes the controller with its updater and Sparkle's standard window, turns Sparkle's
// own scheduled checks off (the bundle's Info.plist has them off; an older Cogpit kept them on in
// the app's user defaults, where Sparkle keeps the value) and starts the updater, whose error it
// keeps instead of the alert SPUStandardUpdaterController's own startUpdater would show. It checks
// nothing: Cogpit's scheduled checks (cogpitSparkleProbe) do, the first right after this. Main
// thread.
static void startOnMain(bool startUpdater) {
	@autoreleasepool {
		if (controllerClass == Nil || controller != nil) {
			return;
		}
		if (sparkleDelegate == nil) {
			sparkleDelegate = [[CogpitSparkleDelegate alloc] init]; // made with alloc: kept
		}
		id<CogpitSPUStandardUpdaterController> c =
			[(id<CogpitSPUStandardUpdaterController>)[controllerClass alloc] initWithStartingUpdater:NO updaterDelegate:sparkleDelegate userDriverDelegate:sparkleDelegate];
		if (c == nil) {
			fail(@"SPUStandardUpdaterController could not be made");
			return;
		}
		controller = c; // made with alloc: kept
		id<CogpitSPUUpdater> updater = c.updater;
		if (!startUpdater) {
			atomic_store(&state, COGPIT_SPARKLE_CREATED);
			return;
		}
		if (updater.automaticallyChecksForUpdates) {
			updater.automaticallyChecksForUpdates = NO;
		}
		NSError *err = nil;
		if (![updater startUpdater:&err]) {
			fail(err != nil ? err.localizedDescription : @"unknown error");
			return;
		}
		atomic_store(&state, COGPIT_SPARKLE_RUNNING);
	}
}

// cogpitSparkleStart queues the start onto the main queue, which the app's event loop drains once
// it runs: Sparkle starts after the app finished launching. startUpdater false makes the
// controller only (the test harness). Any thread.
void cogpitSparkleStart(bool startUpdater) {
	atomic_store(&state, COGPIT_SPARKLE_STARTING);
	dispatch_async(dispatch_get_main_queue(), ^{
		startOnMain(startUpdater);
	});
}

// checkOnMain checks for updates with Sparkle's window, as its "Check for Updates…" menu item does:
// the feed is read again, whatever a probe found before. While a probe runs the check waits for
// its end; while a check the user started or its update is under way, Sparkle's window is up
// already and comes to the front. Main thread.
static void checkOnMain(void) {
	if (controller == nil || atomic_load(&state) != COGPIT_SPARKLE_RUNNING) {
		return;
	}
	if (probing) {
		checkWaiting = YES;
		return;
	}
	if (controller.updater.canCheckForUpdates) {
		[controller checkForUpdates:nil];
	}
}

// cogpitSparkleCheck checks for updates with Sparkle's window (checkOnMain). Any thread.
void cogpitSparkleCheck(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		checkOnMain();
	});
}

// cogpitSparkleProbe runs Sparkle's probing check, Cogpit's scheduled check: it reads the feed,
// tells the delegate what it found (the Update button) and ends, holding nothing; it leaves out the
// release the user skipped. Nothing happens while another check or the user's update session runs:
// that one reads the feed itself. Any thread.
void cogpitSparkleProbe(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (controller == nil || atomic_load(&state) != COGPIT_SPARKLE_RUNNING || controller.updater.sessionInProgress) {
			return;
		}
		probing = YES;
		[controller.updater checkForUpdateInformation];
		if (!controller.updater.sessionInProgress) {
			probing = NO; // Sparkle did not start it
		}
	});
}

// cogpitSparkleState returns the updater's state and, once it failed to start, why. Any thread.
int cogpitSparkleState(char *detail, size_t detailSize) {
	int s = atomic_load(&state);
	if (s == COGPIT_SPARKLE_FAILED && detail != NULL && detailSize > 0) {
		strlcpy(detail, failure, detailSize);
	}
	return s;
}

// cogpitSparkleAutomaticChecks returns Sparkle's automaticallyChecksForUpdates, -1 without a
// controller. Main thread (the test harness).
int cogpitSparkleAutomaticChecks(void) {
	if (controller == nil) {
		return -1;
	}
	return controller.updater.automaticallyChecksForUpdates ? 1 : 0;
}

// cogpitSparkleSessionInProgress returns whether Sparkle runs an update session (a check, or an
// update held for the user), -1 without a running updater. Main thread (the test harness).
int cogpitSparkleSessionInProgress(void) {
	if (controller == nil || atomic_load(&state) != COGPIT_SPARKLE_RUNNING) {
		return -1;
	}
	return controller.updater.sessionInProgress ? 1 : 0;
}
