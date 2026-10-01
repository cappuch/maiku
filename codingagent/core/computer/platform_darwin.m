#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#include "platform_darwin.h"

@interface CrosshairView : NSView
@property CGFloat cx, cy;
@property BOOL pressed;
@property (copy) NSString *status;
@end

@implementation CrosshairView
- (BOOL)isFlipped { return YES; }
- (void)drawRect:(NSRect)dirty {
    [[NSColor clearColor] set];
    NSRectFill(dirty);

    CGFloat x = self.cx, y = self.cy, s = self.pressed ? 0.7 : 0.78, arm = 16;
    NSColor *main = self.pressed ? [NSColor systemGreenColor] : [NSColor systemIndigoColor];

    if (self.pressed) {
        [[main colorWithAlphaComponent:0.35] set];
        [[NSBezierPath bezierPathWithOvalInRect:NSMakeRect(x - 8, y - 8, 16, 16)] fill];
    }

    // Arrow pointer with its tip at (x, y); round joins soften every corner.
    CGFloat pts[][2] = {{0, 0}, {0, 16}, {4.2, 12.3}, {7, 18.6}, {9.6, 17.5}, {6.9, 11.4}, {12.2, 11.4}};
    NSBezierPath *p = [NSBezierPath bezierPath];
    for (int i = 0; i < 7; i++) {
        NSPoint pt = NSMakePoint(x + pts[i][0] * s, y + pts[i][1] * s);
        if (i == 0) [p moveToPoint:pt]; else [p lineToPoint:pt];
    }
    [p closePath];
    p.lineJoinStyle = NSLineJoinStyleRound;
    p.lineCapStyle = NSLineCapStyleRound;

    [NSGraphicsContext saveGraphicsState];
    NSShadow *sh = [[NSShadow alloc] init];
    sh.shadowColor = [NSColor colorWithWhite:0 alpha:0.45];
    sh.shadowBlurRadius = 2.5;
    sh.shadowOffset = NSMakeSize(0, -1);
    [sh set];
    [[NSColor whiteColor] set];
    p.lineWidth = 3; [p stroke];
    [NSGraphicsContext restoreGraphicsState];

    [main set];
    [p fill];
    p.lineWidth = 1.4; [p stroke];

    if (self.status.length) {
        NSDictionary *attrs = @{
            NSFontAttributeName: [NSFont boldSystemFontOfSize:13],
            NSForegroundColorAttributeName: [NSColor whiteColor],
        };
        NSSize sz = [self.status sizeWithAttributes:attrs];
        NSRect box = NSMakeRect(x + arm + 6, y + 8, sz.width + 14, sz.height + 8);
        if (NSMaxX(box) > self.bounds.size.width) box.origin.x = x - arm - 6 - box.size.width;
        if (NSMaxY(box) > self.bounds.size.height) box.origin.y = y - 8 - box.size.height;
        [[NSColor colorWithWhite:0 alpha:0.75] set];
        [[NSBezierPath bezierPathWithRoundedRect:box xRadius:6 yRadius:6] fill];
        [self.status drawAtPoint:NSMakePoint(box.origin.x + 7, box.origin.y + 4) withAttributes:attrs];
    }
}
@end

static NSWindow *gWindow;
static CrosshairView *gView;
static int gInstalled;

@interface ChatPanel : NSObject
@property NSPanel *panel;
@property NSTextView *text;
@property NSButton *toggle;
@property BOOL collapsed;
- (void)appendRole:(NSString *)role text:(NSString *)text;
- (void)toggleCollapsed:(id)sender;
@end

@implementation ChatPanel
- (void)appendRole:(NSString *)role text:(NSString *)text {
    if (!text.length) return;
    NSColor *color = [NSColor colorWithWhite:0.92 alpha:1];
    NSString *prefix = @"";
    BOOL italic = NO;
    if ([role isEqualToString:@"think"]) { color = [NSColor colorWithWhite:0.78 alpha:1]; italic = YES; }
    else if ([role isEqualToString:@"act"]) { prefix = @"→ "; color = [NSColor colorWithCalibratedRed:0.62 green:0.72 blue:1 alpha:1]; }
    else if ([role isEqualToString:@"done"]) { prefix = @"✓ "; color = [NSColor systemGreenColor]; }
    else if ([role isEqualToString:@"task"]) { prefix = @"Task  "; color = [NSColor colorWithWhite:1 alpha:0.95]; }

    NSFont *font = italic ? [NSFont systemFontOfSize:12] : [NSFont systemFontOfSize:12 weight:NSFontWeightMedium];
    if (italic) font = [[NSFontManager sharedFontManager] convertFont:font toHaveTrait:NSItalicFontMask];
    NSDictionary *attrs = @{NSFontAttributeName: font, NSForegroundColorAttributeName: color};
    NSString *line = [NSString stringWithFormat:@"%@%@\n\n", prefix, text];
    [self.text.textStorage appendAttributedString:[[NSAttributedString alloc] initWithString:line attributes:attrs]];
    [self.text scrollRangeToVisible:NSMakeRange(self.text.string.length, 0)];
}

- (void)toggleCollapsed:(id)sender {
    self.collapsed = !self.collapsed;
    NSRect screen = self.panel.screen.frame;
    CGFloat w = 340, margin = 14, header = 34;
    CGFloat h = self.collapsed ? header : 280;
    NSRect f = NSMakeRect(NSMaxX(screen) - w - margin, NSMaxY(screen) - h - margin, w, h);
    self.text.enclosingScrollView.hidden = self.collapsed;
    self.toggle.title = self.collapsed ? @"Agent  ▸" : @"Agent  ▾";
    [self.panel setFrame:f display:YES animate:YES];
}
@end

static ChatPanel *gChat;

static void installChat(void) {
    gChat = [[ChatPanel alloc] init];
    NSRect screen = [[NSScreen mainScreen] frame];
    CGFloat w = 340, h = 280, margin = 14;
    NSRect frame = NSMakeRect(NSMaxX(screen) - w - margin, NSMaxY(screen) - h - margin, w, h);

    NSPanel *panel = [[NSPanel alloc] initWithContentRect:frame
                                                 styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                                                   backing:NSBackingStoreBuffered
                                                     defer:NO];
    panel.opaque = NO;
    panel.backgroundColor = [NSColor colorWithWhite:0.08 alpha:0.88];
    panel.hasShadow = YES;
    panel.level = NSScreenSaverWindowLevel;
    panel.sharingType = NSWindowSharingNone;
    panel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                               NSWindowCollectionBehaviorStationary |
                               NSWindowCollectionBehaviorFullScreenAuxiliary;
    panel.movableByWindowBackground = YES;
    gChat.panel = panel;

    NSView *root = panel.contentView;
    root.wantsLayer = YES;
    root.layer.cornerRadius = 12;
    root.layer.masksToBounds = YES;

    NSButton *toggle = [[NSButton alloc] initWithFrame:NSMakeRect(8, h - 30, w - 16, 22)];
    toggle.autoresizingMask = NSViewMinYMargin | NSViewWidthSizable;
    toggle.bordered = NO;
    toggle.title = @"Agent  ▾";
    toggle.alignment = NSTextAlignmentLeft;
    toggle.font = [NSFont systemFontOfSize:13 weight:NSFontWeightSemibold];
    toggle.contentTintColor = [NSColor whiteColor];
    toggle.target = gChat;
    toggle.action = @selector(toggleCollapsed:);
    gChat.toggle = toggle;
    [root addSubview:toggle];

    NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(8, 8, w - 16, h - 42)];
    scroll.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    scroll.drawsBackground = NO;
    scroll.hasVerticalScroller = YES;
    scroll.autohidesScrollers = YES;

    NSTextView *tv = [[NSTextView alloc] initWithFrame:scroll.bounds];
    tv.editable = NO;
    tv.selectable = YES;
    tv.drawsBackground = NO;
    tv.textContainerInset = NSMakeSize(4, 4);
    tv.textContainer.widthTracksTextView = YES;
    scroll.documentView = tv;
    gChat.text = tv;
    [root addSubview:scroll];

    [panel orderFrontRegardless];
}

void ChatPost(const char *role, const char *text) {
    NSString *r = role ? [NSString stringWithUTF8String:role] : @"";
    NSString *t = text ? [NSString stringWithUTF8String:text] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!gChat) return;
        [gChat appendRole:r text:t];
    });
}

static void installOverlay(void) {
    if (gWindow || ![NSApp isRunning]) return;
    NSRect frame = [[NSScreen mainScreen] frame];
    gWindow = [[NSWindow alloc] initWithContentRect:frame
                                          styleMask:NSWindowStyleMaskBorderless
                                            backing:NSBackingStoreBuffered
                                              defer:NO];
    gWindow.opaque = NO;
    gWindow.backgroundColor = [NSColor clearColor];
    gWindow.hasShadow = NO;
    gWindow.ignoresMouseEvents = YES;
    gWindow.level = NSScreenSaverWindowLevel;
    gWindow.sharingType = NSWindowSharingNone;
    gWindow.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                                 NSWindowCollectionBehaviorStationary |
                                 NSWindowCollectionBehaviorFullScreenAuxiliary;

    gView = [[CrosshairView alloc] initWithFrame:NSMakeRect(0, 0, frame.size.width, frame.size.height)];
    gView.cx = frame.size.width / 2;
    gView.cy = frame.size.height / 2;
    gWindow.contentView = gView;
    [gWindow orderFrontRegardless];
    installChat();
    gInstalled = 1;
}

// Attach the crosshair to the host app's existing Cocoa run loop (the desktop
// app). A CLI process with no running NSApp still controls the pointer; it
// just has no overlay.
void OverlayEnsure(void) {
    if (![NSApp isRunning]) return;
    if ([NSThread isMainThread]) {
        installOverlay();
        return;
    }
    dispatch_sync(dispatch_get_main_queue(), ^{ installOverlay(); });
}

void OverlaySet(double x, double y, int pressed, const char *status) {
    NSString *s = status ? [NSString stringWithUTF8String:status] : nil;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!gView) return;
        gView.cx = x; gView.cy = y; gView.pressed = pressed != 0;
        if (s) gView.status = s;
        gView.needsDisplay = YES;
    });
}

void OverlayVisible(int visible) {
    if (!gInstalled) return;
    dispatch_sync(dispatch_get_main_queue(), ^{
        if (!gWindow) return;
        gWindow.alphaValue = visible ? 1.0 : 0.0;
        [gWindow displayIfNeeded];
    });
}

void OverlayQuit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
}

void ScreenSize(double *w, double *h) {
    CGRect b = CGDisplayBounds(CGMainDisplayID());
    *w = b.size.width; *h = b.size.height;
}

void CursorPos(double *x, double *y) {
    CGEventRef e = CGEventCreate(NULL);
    CGPoint p = CGEventGetLocation(e);
    CFRelease(e);
    *x = p.x; *y = p.y;
}

void WarpCursor(double x, double y) {
    CGWarpMouseCursorPosition(CGPointMake(x, y));
    CGAssociateMouseAndMouseCursorPosition(true);
}

static void post(CGEventRef e) { CGEventPost(kCGHIDEventTap, e); CFRelease(e); }

void MouseMove(double x, double y, int dragButton) {
    CGEventType t = kCGEventMouseMoved;
    CGMouseButton b = kCGMouseButtonLeft;
    if (dragButton == 1) { t = kCGEventLeftMouseDragged; }
    if (dragButton == 2) { t = kCGEventRightMouseDragged; b = kCGMouseButtonRight; }
    post(CGEventCreateMouseEvent(NULL, t, CGPointMake(x, y), b));
}

void MouseButton(double x, double y, int button, int down, int clicks) {
    CGEventType t;
    CGMouseButton b;
    switch (button) {
        case 2: b = kCGMouseButtonRight; t = down ? kCGEventRightMouseDown : kCGEventRightMouseUp; break;
        case 3: b = kCGMouseButtonCenter; t = down ? kCGEventOtherMouseDown : kCGEventOtherMouseUp; break;
        default: b = kCGMouseButtonLeft; t = down ? kCGEventLeftMouseDown : kCGEventLeftMouseUp; break;
    }
    CGEventRef e = CGEventCreateMouseEvent(NULL, t, CGPointMake(x, y), b);
    CGEventSetIntegerValueField(e, kCGMouseEventClickState, clicks);
    post(e);
}

void Scroll(int dy, int dx) {
    post(CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 2, dy, dx));
}

void KeyEvent(int keycode, int down, unsigned long long flags) {
    CGEventRef e = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)keycode, down != 0);
    CGEventSetFlags(e, (CGEventFlags)flags);
    post(e);
}

void TypeUnicode(const unsigned short *chars, int n) {
    for (int down = 1; down >= 0; down--) {
        CGEventRef e = CGEventCreateKeyboardEvent(NULL, 0, down);
        CGEventKeyboardSetUnicodeString(e, n, chars);
        CGEventSetFlags(e, 0);
        post(e);
    }
}

int AccessibilityTrusted(int prompt) {
    NSDictionary *opts = @{(__bridge id)kAXTrustedCheckOptionPrompt: prompt ? @YES : @NO};
    return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts) ? 1 : 0;
}
