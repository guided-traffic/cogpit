// A native check of clipboard_darwin.m, built and run by TestConcealedClipboardMarkedForThisMacOnly
// in a process of its own. It puts a pasteboard of its own in the general pasteboard's place
// (+generalPasteboard, and +pasteboardWithName: for the general name), so the user's clipboard is
// neither read nor changed, records the options the copy prepares that pasteboard with (AppKit
// reads no pasteboard's options back), copies its argument with cogpitSetConcealedString and
// prints what a clipboard manager and a paste read, one line per fact:
//
//	general <times the copy asked for the general pasteboard>
//	set <the copy's result: 1 or 0>
//	prepared <options> <NSPasteboardContentsCurrentHostOnly> <on the stand-in: 1 or 0> <change count it began>
//	count <the pasteboard's change count after the copy>
//	type <a type on the pasteboard>, one line each
//	string <the string read back, as hex>
//
// "prepared" is missing when the copy never prepared the pasteboard, "string" when it holds none.
// Exit status 3: the stand-in holds no string before the copy (no pasteboard server).

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include "clipboard_darwin.h"

static NSPasteboard *standIn;
static int generalCalls;

static id generalStandIn(id cls, SEL cmd) {
	generalCalls++;
	return standIn;
}

static IMP pasteboardWithNameAppKit;

static id namedStandIn(id cls, SEL cmd, NSString *name) {
	if ([name isEqualToString:NSPasteboardNameGeneral]) {
		generalCalls++;
		return standIn;
	}
	return ((id (*)(id, SEL, NSString *))pasteboardWithNameAppKit)(cls, cmd, name);
}

static IMP prepareAppKit;
static BOOL prepared;
static NSPasteboardContentsOptions preparedOptions;
static BOOL preparedStandIn;
static NSInteger preparedCount;

static NSInteger prepareRecorded(id pb, SEL cmd, NSPasteboardContentsOptions options) {
	NSInteger count = ((NSInteger (*)(id, SEL, NSPasteboardContentsOptions))prepareAppKit)(pb, cmd, options);
	prepared = YES;
	preparedOptions = options;
	preparedStandIn = pb == standIn;
	preparedCount = count;
	return count;
}

int main(int argc, char **argv) {
	if (argc != 2) {
		fprintf(stderr, "usage: clipboard_harness <value>\n");
		return 2;
	}
	int status = 0;
	@autoreleasepool {
		standIn = [[NSPasteboard pasteboardWithUniqueName] retain];
		[standIn clearContents];
		if (![standIn setString:@"an earlier copy" forType:NSPasteboardTypeString] ||
		    ![[standIn stringForType:NSPasteboardTypeString] isEqualToString:@"an earlier copy"]) {
			status = 3;
		} else {
			Method general = class_getClassMethod([NSPasteboard class], @selector(generalPasteboard));
			Method named = class_getClassMethod([NSPasteboard class], @selector(pasteboardWithName:));
			Method prepare = class_getInstanceMethod([NSPasteboard class], @selector(prepareForNewContentsWithOptions:));
			IMP generalAppKit = method_setImplementation(general, (IMP)generalStandIn);
			pasteboardWithNameAppKit = method_setImplementation(named, (IMP)namedStandIn);
			prepareAppKit = method_setImplementation(prepare, (IMP)prepareRecorded);
			bool ok = cogpitSetConcealedString(argv[1]);
			method_setImplementation(prepare, prepareAppKit);
			method_setImplementation(named, pasteboardWithNameAppKit);
			method_setImplementation(general, generalAppKit);

			printf("general %d\n", generalCalls);
			printf("set %d\n", ok);
			if (prepared) {
				printf("prepared %lu %lu %d %ld\n", (unsigned long)preparedOptions,
				       (unsigned long)NSPasteboardContentsCurrentHostOnly, preparedStandIn, (long)preparedCount);
			}
			printf("count %ld\n", (long)[standIn changeCount]);
			for (NSString *type in [standIn types]) {
				printf("type %s\n", [type UTF8String]);
			}
			NSString *s = [standIn stringForType:NSPasteboardTypeString];
			if (s != nil) {
				printf("string ");
				for (const unsigned char *c = (const unsigned char *)[s UTF8String]; *c != 0; c++) {
					printf("%02x", *c);
				}
				printf("\n");
			}
		}
		[standIn releaseGlobally];
		[standIn release];
	}
	return status;
}
