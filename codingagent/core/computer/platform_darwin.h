#pragma once

void OverlayEnsure(void);
void OverlaySet(double x, double y, int pressed, const char *status);
void OverlayVisible(int visible);
void OverlayQuit(void);
void ChatPost(const char *role, const char *text);
void ScreenSize(double *w, double *h);
void CursorPos(double *x, double *y);
void WarpCursor(double x, double y);
void MouseMove(double x, double y, int dragButton);
void MouseButton(double x, double y, int button, int down, int clicks);
void Scroll(int dy, int dx);
void KeyEvent(int keycode, int down, unsigned long long flags);
void TypeUnicode(const unsigned short *chars, int n);
int AccessibilityTrusted(int prompt);
