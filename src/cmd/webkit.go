package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/server"
	"github.com/ebitengine/purego"
)

const (
	gtk3SONAME = "libgtk-3.so.0"
	gdk3SONAME = "libgdk-3.so.0"
	gtk4SONAME = "libgtk-4.so.1"
)

type webkitStack int

const (
	stackGTK3 webkitStack = iota
	stackGTK4
)

// Tried when ldconfig names nothing supported. 6.0 is GTK 4. Current builds
// use the .so.4 soname; older builds used .so.0. 4.1 is the GTK 3 library
// shipped since 2021. The removed libsoup 2 library (4.0) is not tried.
var webkitFallbackSONAMEs = []string{
	"libwebkitgtk-6.0.so.4",
	"libwebkitgtk-6.0.so.0",
	"libwebkit2gtk-4.1.so.0",
}

var (
	uiStack webkitStack

	gtkInitCheck                        func(uintptr, uintptr) int32
	gtkWindowNew                        func(int32) uintptr
	gtkWindowSetTitle                   func(uintptr, string)
	gtkWindowSetDefaultSize             func(uintptr, int32, int32)
	gtkWindowFullscreen                 func(uintptr)
	gtkWindowUnfullscreen               func(uintptr)
	gtkWindowIsFullscreen               func(uintptr) int32
	gtkWindowMaximize                   func(uintptr)
	gtkWindowUnmaximize                 func(uintptr)
	gtkWindowIsMaximized                func(uintptr) int32
	gtkWindowMinimize                   func(uintptr)
	gtkWindowIconify                    func(uintptr)
	gtkWindowClose                      func(uintptr)
	gtkWindowSetDecorated               func(uintptr, int32)
	gtkBoxNew                           func(int32, int32) uintptr
	gtkBoxPackStart                     func(uintptr, uintptr, int32, int32, uint32)
	gtkBoxAppend                        func(uintptr, uintptr)
	gtkWidgetSetVexpand                 func(uintptr, int32)
	gtkWidgetSetVisible                 func(uintptr, int32)
	gtkWidgetSetTooltipText             func(uintptr, string)
	gtk3ButtonNewFromIconName           func(string, int32) uintptr
	gtk4ButtonNewFromIconName           func(string) uintptr
	gtkHeaderBarPackEnd                 func(uintptr, uintptr)
	gtkContainerAdd                     func(uintptr, uintptr)
	gtkWidgetShowAll                    func(uintptr)
	gtkMain                             func()
	gtkMainQuit                         func()
	webkitWebViewNew                    func() uintptr
	webkitWebViewLoadURI                func(uintptr, string)
	webkitWebViewRunJavaScript          func(uintptr, string, uintptr, uintptr, uintptr)
	webkitWebViewEvaluateJavaScript     func(uintptr, string, int64, uintptr, uintptr, uintptr, uintptr, uintptr)
	webkitNavigationPolicyGetAction     func(uintptr) uintptr
	webkitNavigationActionGetRequest    func(uintptr) uintptr
	webkitURIRequestGetURI              func(uintptr) uintptr
	webkitPolicyDecisionIgnore          func(uintptr)
	gSignalConnectData                  func(uintptr, string, uintptr, uintptr, uintptr, int32) uintptr
	gIdleAdd                            func(uintptr, uintptr) uint32
	gtkHeaderBarNew                     func() uintptr
	gtkHeaderBarSetTitle                func(uintptr, string)
	gtkHeaderBarSetShowCloseButton      func(uintptr, int32)
	gtkCSSProviderNew                   func() uintptr
	gtkCSSProviderLoadFromData          func(uintptr, string, int, uintptr) int32
	gtkStyleContextAddProviderForScreen func(uintptr, uintptr, uint32)
	gdkScreenGetDefault                 func() uintptr

	gtk4InitCheck                    func() int32
	gtk4WindowNew                    func() uintptr
	gtk4WindowSetChild               func(uintptr, uintptr)
	gtk4WindowPresent                func(uintptr)
	gtk4HeaderBarSetShowTitleButtons func(uintptr, int32)
	gtk4CSSLoadFromData              func(uintptr, string, int)
	gtk4CSSLoadFromString            func(uintptr, string)
	gtk4StyleAddProviderForDisplay   func(uintptr, uintptr, uint32)
	gdk4DisplayGetDefault            func() uintptr
	gMainLoopNew                     func(uintptr, int32) uintptr
	gMainLoopRun                     func(uintptr)
	gMainLoopQuit                    func(uintptr)
	mainLoop                         uintptr

	destroyCB    uintptr
	closeCB      uintptr
	idleCB       uintptr
	decideCB     uintptr
	minimizeCB   uintptr
	maximizeCB   uintptr
	closeClickCB uintptr
	bigPictureCB uintptr
	quitReq      atomic.Bool

	// webkitAppURL is the page loaded in the window. The decide-policy
	// callback reads it on the GTK thread that set it.
	webkitAppURL     string
	webkitWindow     uintptr
	webkitHeader     uintptr
	webkitView       uintptr
	webkitFullscreen atomic.Bool
)

const (
	webkitPolicyNavigation uintptr = 0
	webkitPolicyNewWindow  uintptr = 1
)

func onWindowDestroy(uintptr, uintptr) uintptr {
	quitUI()
	return 0
}

func onWindowCloseRequest(uintptr, uintptr) uintptr {
	quitUI()
	return 0
}

func onIdleQuit(uintptr) uintptr {
	quitUI()
	return 0
}

func quitUI() {
	if uiStack == stackGTK4 {
		if gMainLoopQuit != nil && mainLoop != 0 {
			gMainLoopQuit(mainLoop)
		}
		return
	}
	if gtkMainQuit != nil {
		gtkMainQuit()
	}
}

func requestQuit() {
	quitReq.Store(true)
	if gIdleAdd != nil && idleCB != 0 {
		gIdleAdd(idleCB, 0)
	}
}

func openShared(name string) (uintptr, error) {
	handle, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return handle, nil
}

func registerLibFunc(fptr any, handle uintptr, name string) error {
	if _, err := purego.Dlsym(handle, name); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	purego.RegisterLibFunc(fptr, handle, name)
	return nil
}

func linkerArchTag() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86-64"
	case "arm64":
		return "AArch64"
	default:
		return ""
	}
}

func webkitCandidates() []string {
	out, err := exec.Command("ldconfig", "-p").Output()
	if err != nil {
		return append([]string(nil), webkitFallbackSONAMEs...)
	}
	names := webkitSONAMEs(string(out), linkerArchTag())
	if len(names) == 0 {
		return append([]string(nil), webkitFallbackSONAMEs...)
	}
	return names
}

func webkitSONAMEs(ldconfigText, archTag string) []string {
	if archTag == "" {
		return nil
	}
	type candidate struct {
		name string
		ver  []int
	}
	seen := map[string]struct{}{}
	var found []candidate
	for _, line := range strings.Split(ldconfigText, "\n") {
		if !linkerLineArch(line, archTag) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		ver, _, ok := parseWebKitSONAME(name)
		if !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		found = append(found, candidate{name: name, ver: ver})
	}
	sort.Slice(found, func(i, j int) bool {
		return versionHigher(found[i].ver, found[j].ver)
	})
	names := make([]string, len(found))
	for i, c := range found {
		names[i] = c.name
	}
	return names
}

func linkerLineArch(line, archTag string) bool {
	open := strings.Index(line, "(")
	close := strings.Index(line, ")")
	if open < 0 || close < open {
		return false
	}
	return strings.Contains(line[open:close], archTag)
}

// parseWebKitSONAME accepts libwebkitgtk-6+ (GTK 4) and libwebkit2gtk-4.1+
// (GTK 3). The suffix after .so. is the ELF soname (4 on current builds, 0 on
// older ones) and is only used to order two copies of the same API.
// libwebkit2gtk-4.0 is the removed libsoup 2 build.
func parseWebKitSONAME(name string) (ver []int, gtk4 bool, ok bool) {
	base, soVer, ok := splitSOName(name)
	if !ok {
		return nil, false, false
	}
	var raw string
	switch {
	case strings.HasPrefix(base, "libwebkit2gtk-"):
		raw = strings.TrimPrefix(base, "libwebkit2gtk-")
	case strings.HasPrefix(base, "libwebkitgtk-"):
		raw = strings.TrimPrefix(base, "libwebkitgtk-")
		gtk4 = true
	default:
		return nil, false, false
	}
	if raw == "" {
		return nil, false, false
	}
	parts := strings.Split(raw, ".")
	ver = make([]int, 0, len(parts)+1)
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false, false
		}
		ver = append(ver, n)
	}
	n, err := strconv.Atoi(soVer)
	if err != nil || n < 0 {
		return nil, false, false
	}
	ver = append(ver, n)
	if gtk4 {
		if ver[0] < 6 {
			return nil, false, false
		}
		return ver, true, true
	}
	if !webkit2GTK3Supported(ver) {
		return nil, false, false
	}
	return ver, false, true
}

func splitSOName(name string) (base, soVer string, ok bool) {
	i := strings.LastIndex(name, ".so.")
	if i <= 0 || i+4 >= len(name) {
		return "", "", false
	}
	soVer = name[i+4:]
	for _, c := range soVer {
		if c < '0' || c > '9' {
			return "", "", false
		}
	}
	return name[:i], soVer, true
}

func webkit2GTK3Supported(ver []int) bool {
	if len(ver) == 0 || ver[0] < 4 {
		return false
	}
	if ver[0] > 4 {
		return true
	}
	minor := 0
	if len(ver) > 1 {
		minor = ver[1]
	}
	return minor >= 1
}

func webkitUsesGTK4(name string) bool {
	_, gtk4, ok := parseWebKitSONAME(name)
	return ok && gtk4
}

func versionHigher(a, b []int) bool {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

// flatpakProgramName is the program name WebKit should adopt inside Flatpak.
// An empty result means the environment is not a Flatpak app, or FLATPAK_ID
// would not pass g_application_id_is_valid and WebKit would ignore it.
func flatpakProgramName(flatpakID string) string {
	id := strings.TrimSpace(flatpakID)
	if !applicationIDValid(id) {
		return ""
	}
	return id
}

// applicationIDValid matches g_application_id_is_valid: a D-Bus well-known
// name, not a unique name. WebKit uses that check before accepting g_get_prgname.
func applicationIDValid(id string) bool {
	if id == "" || len(id) > 255 || id[0] == ':' || id[0] == '.' || (id[0] >= '0' && id[0] <= '9') {
		return false
	}
	hasDot := false
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c == '.' {
			if i+1 >= len(id) || id[i+1] == '.' || (id[i+1] >= '0' && id[i+1] <= '9') {
				return false
			}
			hasDot = true
			continue
		}
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return hasDot
}

func setFlatpakProgramName(glib uintptr) error {
	id := flatpakProgramName(os.Getenv("FLATPAK_ID"))
	if id == "" {
		if strings.TrimSpace(os.Getenv("FLATPAK_ID")) != "" {
			log.Printf("webkit: FLATPAK_ID %q is not a valid application id", os.Getenv("FLATPAK_ID"))
		}
		return nil
	}
	var setPrgname func(string)
	if err := registerLibFunc(&setPrgname, glib, "g_set_prgname"); err != nil {
		return err
	}
	setPrgname(id)
	return nil
}

func loadWebKit() error {
	glib, err := openShared("libglib-2.0.so.0")
	if err != nil {
		return err
	}
	// WebKit caches its application id on first use. Inside Flatpak that id
	// becomes the accessibility bus name of the sandboxed web process, and
	// the portal aborts the UI process unless the name starts with FLATPAK_ID.
	// The default program name, "servemedia", is not a valid application id,
	// so WebKit would otherwise fall back to org.webkit.app-<hash>.
	if err := setFlatpakProgramName(glib); err != nil {
		return err
	}
	gobject, err := openShared("libgobject-2.0.so.0")
	if err != nil {
		return err
	}
	var errs []string
	for _, name := range webkitCandidates() {
		webkit, err := openShared(name)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if webkitUsesGTK4(name) {
			return bindGTK4(glib, gobject, webkit)
		}
		return bindGTK3(glib, gobject, webkit)
	}
	if len(errs) == 0 {
		return fmt.Errorf("webkit library not found")
	}
	return fmt.Errorf("%s", strings.Join(errs, "; "))
}

func bindGTK3(glib, gobject, webkit uintptr) error {
	gtk, err := openShared(gtk3SONAME)
	if err != nil {
		return err
	}
	gdk, err := openShared(gdk3SONAME)
	if err != nil {
		return err
	}
	binds := []struct {
		fptr   any
		handle uintptr
		name   string
	}{
		{&gIdleAdd, glib, "g_idle_add"},
		{&gSignalConnectData, gobject, "g_signal_connect_data"},
		{&gtkInitCheck, gtk, "gtk_init_check"},
		{&gtkWindowNew, gtk, "gtk_window_new"},
		{&gtkWindowSetTitle, gtk, "gtk_window_set_title"},
		{&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size"},
		{&gtkWindowFullscreen, gtk, "gtk_window_fullscreen"},
		{&gtkWindowUnfullscreen, gtk, "gtk_window_unfullscreen"},
		{&gtkWindowMaximize, gtk, "gtk_window_maximize"},
		{&gtkWindowUnmaximize, gtk, "gtk_window_unmaximize"},
		{&gtkWindowIsMaximized, gtk, "gtk_window_is_maximized"},
		{&gtkWindowIconify, gtk, "gtk_window_iconify"},
		{&gtkWindowClose, gtk, "gtk_window_close"},
		{&gtkWindowSetDecorated, gtk, "gtk_window_set_decorated"},
		{&gtkBoxNew, gtk, "gtk_box_new"},
		{&gtkBoxPackStart, gtk, "gtk_box_pack_start"},
		{&gtkWidgetSetVexpand, gtk, "gtk_widget_set_vexpand"},
		{&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible"},
		{&gtkWidgetSetTooltipText, gtk, "gtk_widget_set_tooltip_text"},
		{&gtk3ButtonNewFromIconName, gtk, "gtk_button_new_from_icon_name"},
		{&gtkHeaderBarPackEnd, gtk, "gtk_header_bar_pack_end"},
		{&gtkContainerAdd, gtk, "gtk_container_add"},
		{&gtkWidgetShowAll, gtk, "gtk_widget_show_all"},
		{&gtkMain, gtk, "gtk_main"},
		{&gtkMainQuit, gtk, "gtk_main_quit"},
		{&gtkHeaderBarNew, gtk, "gtk_header_bar_new"},
		{&gtkHeaderBarSetTitle, gtk, "gtk_header_bar_set_title"},
		{&gtkHeaderBarSetShowCloseButton, gtk, "gtk_header_bar_set_show_close_button"},
		{&gtkCSSProviderNew, gtk, "gtk_css_provider_new"},
		{&gtkCSSProviderLoadFromData, gtk, "gtk_css_provider_load_from_data"},
		{&gtkStyleContextAddProviderForScreen, gtk, "gtk_style_context_add_provider_for_screen"},
		{&gdkScreenGetDefault, gdk, "gdk_screen_get_default"},
		{&webkitWebViewNew, webkit, "webkit_web_view_new"},
		{&webkitWebViewLoadURI, webkit, "webkit_web_view_load_uri"},
		{&webkitNavigationPolicyGetAction, webkit, "webkit_navigation_policy_decision_get_navigation_action"},
		{&webkitNavigationActionGetRequest, webkit, "webkit_navigation_action_get_request"},
		{&webkitURIRequestGetURI, webkit, "webkit_uri_request_get_uri"},
		{&webkitPolicyDecisionIgnore, webkit, "webkit_policy_decision_ignore"},
	}
	for _, b := range binds {
		if err := registerLibFunc(b.fptr, b.handle, b.name); err != nil {
			return err
		}
	}
	bindWebKitScript(webkit)
	uiStack = stackGTK3
	ensureCallbacks()
	return nil
}

func bindGTK4(glib, gobject, webkit uintptr) error {
	gtk, err := openShared(gtk4SONAME)
	if err != nil {
		return err
	}
	binds := []struct {
		fptr   any
		handle uintptr
		name   string
	}{
		{&gIdleAdd, glib, "g_idle_add"},
		{&gSignalConnectData, gobject, "g_signal_connect_data"},
		{&gMainLoopNew, glib, "g_main_loop_new"},
		{&gMainLoopRun, glib, "g_main_loop_run"},
		{&gMainLoopQuit, glib, "g_main_loop_quit"},
		{&gtk4InitCheck, gtk, "gtk_init_check"},
		{&gtk4WindowNew, gtk, "gtk_window_new"},
		{&gtkWindowSetTitle, gtk, "gtk_window_set_title"},
		{&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size"},
		{&gtkWindowFullscreen, gtk, "gtk_window_fullscreen"},
		{&gtkWindowUnfullscreen, gtk, "gtk_window_unfullscreen"},
		{&gtkWindowIsFullscreen, gtk, "gtk_window_is_fullscreen"},
		{&gtkWindowMaximize, gtk, "gtk_window_maximize"},
		{&gtkWindowUnmaximize, gtk, "gtk_window_unmaximize"},
		{&gtkWindowIsMaximized, gtk, "gtk_window_is_maximized"},
		{&gtkWindowMinimize, gtk, "gtk_window_minimize"},
		{&gtkWindowClose, gtk, "gtk_window_close"},
		{&gtkWindowSetDecorated, gtk, "gtk_window_set_decorated"},
		{&gtkBoxNew, gtk, "gtk_box_new"},
		{&gtkBoxAppend, gtk, "gtk_box_append"},
		{&gtkWidgetSetVexpand, gtk, "gtk_widget_set_vexpand"},
		{&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible"},
		{&gtkWidgetSetTooltipText, gtk, "gtk_widget_set_tooltip_text"},
		{&gtk4ButtonNewFromIconName, gtk, "gtk_button_new_from_icon_name"},
		{&gtkHeaderBarPackEnd, gtk, "gtk_header_bar_pack_end"},
		{&gtk4WindowSetChild, gtk, "gtk_window_set_child"},
		{&gtk4WindowPresent, gtk, "gtk_window_present"},
		{&gtkHeaderBarNew, gtk, "gtk_header_bar_new"},
		{&gtk4HeaderBarSetShowTitleButtons, gtk, "gtk_header_bar_set_show_title_buttons"},
		{&gtkCSSProviderNew, gtk, "gtk_css_provider_new"},
		{&gtk4StyleAddProviderForDisplay, gtk, "gtk_style_context_add_provider_for_display"},
		{&gdk4DisplayGetDefault, gtk, "gdk_display_get_default"},
		{&webkitWebViewNew, webkit, "webkit_web_view_new"},
		{&webkitWebViewLoadURI, webkit, "webkit_web_view_load_uri"},
		{&webkitNavigationPolicyGetAction, webkit, "webkit_navigation_policy_decision_get_navigation_action"},
		{&webkitNavigationActionGetRequest, webkit, "webkit_navigation_action_get_request"},
		{&webkitURIRequestGetURI, webkit, "webkit_uri_request_get_uri"},
		{&webkitPolicyDecisionIgnore, webkit, "webkit_policy_decision_ignore"},
	}
	for _, b := range binds {
		if err := registerLibFunc(b.fptr, b.handle, b.name); err != nil {
			return err
		}
	}
	if err := bindGTK4CSS(gtk); err != nil {
		return err
	}
	bindWebKitScript(webkit)
	uiStack = stackGTK4
	ensureCallbacks()
	return nil
}

// bindWebKitScript binds a way to run a script in the open page. GTK 4
// WebKit dropped webkit_web_view_run_javascript, so the newer evaluate call
// is the fallback. Neither symbol is required to open the window.
func bindWebKitScript(webkit uintptr) {
	if _, err := purego.Dlsym(webkit, "webkit_web_view_run_javascript"); err == nil {
		purego.RegisterLibFunc(&webkitWebViewRunJavaScript, webkit, "webkit_web_view_run_javascript")
		return
	}
	if _, err := purego.Dlsym(webkit, "webkit_web_view_evaluate_javascript"); err == nil {
		purego.RegisterLibFunc(&webkitWebViewEvaluateJavaScript, webkit, "webkit_web_view_evaluate_javascript")
	}
}

func bindGTK4CSS(gtk uintptr) error {
	if _, err := purego.Dlsym(gtk, "gtk_css_provider_load_from_data"); err == nil {
		purego.RegisterLibFunc(&gtk4CSSLoadFromData, gtk, "gtk_css_provider_load_from_data")
		return nil
	}
	if _, err := purego.Dlsym(gtk, "gtk_css_provider_load_from_string"); err == nil {
		purego.RegisterLibFunc(&gtk4CSSLoadFromString, gtk, "gtk_css_provider_load_from_string")
		return nil
	}
	return fmt.Errorf("gtk_css_provider_load_from_data missing")
}

func ensureCallbacks() {
	if destroyCB == 0 {
		destroyCB = purego.NewCallback(onWindowDestroy)
	}
	if closeCB == 0 {
		closeCB = purego.NewCallback(onWindowCloseRequest)
	}
	if idleCB == 0 {
		idleCB = purego.NewCallback(onIdleQuit)
	}
	if decideCB == 0 {
		decideCB = purego.NewCallback(onDecidePolicy)
	}
	if minimizeCB == 0 {
		minimizeCB = purego.NewCallback(onMinimizeClicked)
	}
	if maximizeCB == 0 {
		maximizeCB = purego.NewCallback(onMaximizeClicked)
	}
	if closeClickCB == 0 {
		closeClickCB = purego.NewCallback(onCloseClicked)
	}
	if bigPictureCB == 0 {
		bigPictureCB = purego.NewCallback(onBigPictureClicked)
	}
}

// onDecidePolicy keeps same-origin app navigations in the window. A new
// window, another origin, or a non-http scheme is ignored here and opened
// with the system handler. Response decisions stay unhandled so subresources
// still load.
func onDecidePolicy(_, decision, kind, _ uintptr) uintptr {
	if kind != webkitPolicyNavigation && kind != webkitPolicyNewWindow {
		return 0
	}
	if decision == 0 || webkitNavigationPolicyGetAction == nil || webkitPolicyDecisionIgnore == nil {
		return 0
	}
	action := webkitNavigationPolicyGetAction(decision)
	if action == 0 || webkitNavigationActionGetRequest == nil || webkitURIRequestGetURI == nil {
		return 0
	}
	request := webkitNavigationActionGetRequest(action)
	if request == 0 {
		return 0
	}
	uri := cString(webkitURIRequestGetURI(request))
	switch webkitActionFor(webkitAppURL, uri, kind == webkitPolicyNewWindow) {
	case webkitActBigPicture:
		webkitPolicyDecisionIgnore(decision)
		toggleBigPicture()
		return 1
	case webkitActIgnore:
		webkitPolicyDecisionIgnore(decision)
		return 1
	case webkitActExternal:
		webkitPolicyDecisionIgnore(decision)
		launchDefault(uri)
		return 1
	default:
		return 0
	}
}

func cString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var n uintptr
	for n < 65536 && *(*byte)(unsafe.Pointer(ptr + n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n))
}

// webkitHandsOff reports whether uri should leave the WebKit window.
// Same-origin http(s) stays, including the initial load. about, blob, data,
// and javascript stay and are never passed to the system handler. A new
// window, another origin, or another scheme is handed off.
func webkitHandsOff(appURL, uri string, newWindow bool) bool {
	target, err := url.Parse(uri)
	if err != nil || target.Scheme == "" {
		return false
	}
	switch strings.ToLower(target.Scheme) {
	case "about", "blob", "data", "javascript":
		return false
	}
	if newWindow {
		return true
	}
	if scheme := strings.ToLower(target.Scheme); scheme != "http" && scheme != "https" {
		return true
	}
	app, err := url.Parse(appURL)
	if err != nil || app.Host == "" {
		return false
	}
	return !sameOrigin(app, target)
}

func sameOrigin(a, b *url.URL) bool {
	if !strings.EqualFold(a.Scheme, b.Scheme) {
		return false
	}
	if !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	return originPort(a) == originPort(b)
}

func originPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

const (
	webkitActStay webkitAction = iota
	webkitActExternal
	webkitActIgnore
	webkitActBigPicture
)

type webkitAction int

const webkitBigPictureURL = "servemedia://window/big-picture"

func bigPictureURL(target *url.URL) bool {
	want, err := url.Parse(webkitBigPictureURL)
	if err != nil || target == nil {
		return false
	}
	return strings.EqualFold(target.Scheme, want.Scheme) && strings.EqualFold(target.Host, want.Host) && target.Path == want.Path
}

// webkitActionFor decides what a navigation does. servemedia: stays inside
// the process: the big-picture URL toggles that mode, and any other URI on
// that scheme is ignored. Nothing on that scheme is passed to the system handler.
func webkitActionFor(appURL, uri string, newWindow bool) webkitAction {
	target, err := url.Parse(uri)
	if err == nil && strings.EqualFold(target.Scheme, "servemedia") {
		if bigPictureURL(target) {
			return webkitActBigPicture
		}
		return webkitActIgnore
	}
	if webkitHandsOff(appURL, uri, newWindow) {
		return webkitActExternal
	}
	return webkitActStay
}

func onMinimizeClicked(uintptr, uintptr) uintptr {
	minimizeWindow()
	return 0
}

func onMaximizeClicked(uintptr, uintptr) uintptr {
	toggleWindowMaximize()
	return 0
}

func onCloseClicked(uintptr, uintptr) uintptr {
	if webkitWindow != 0 && gtkWindowClose != nil {
		gtkWindowClose(webkitWindow)
	}
	return 0
}

func onBigPictureClicked(uintptr, uintptr) uintptr {
	toggleBigPicture()
	return 0
}

func minimizeWindow() {
	if webkitWindow == 0 {
		return
	}
	if gtkWindowMinimize != nil {
		gtkWindowMinimize(webkitWindow)
		return
	}
	if gtkWindowIconify != nil {
		gtkWindowIconify(webkitWindow)
	}
}

func fullscreenWindow(window uintptr) {
	if window != 0 && gtkWindowFullscreen != nil {
		gtkWindowFullscreen(window)
	}
	webkitFullscreen.Store(true)
}

func unfullscreenWindow() {
	if webkitWindow != 0 && gtkWindowUnfullscreen != nil {
		gtkWindowUnfullscreen(webkitWindow)
	}
	webkitFullscreen.Store(false)
}

func windowIsMaximized() bool {
	if gtkWindowIsMaximized != nil && webkitWindow != 0 {
		return gtkWindowIsMaximized(webkitWindow) != 0
	}
	return false
}

func maximizeWindow(window uintptr) {
	if window != 0 && gtkWindowMaximize != nil {
		gtkWindowMaximize(window)
	}
}

// applyStartupChrome sizes the window for a saved startup mode.
// Call it after the window is shown so a later show-all does not reveal the header.
func applyStartupChrome(window uintptr, mode string) {
	switch config.NormalizeWindowMode(mode) {
	case config.WindowModeUnmaximized:
		setHeaderVisible(true)
	case config.WindowModeBigPicture:
		fullscreenWindow(window)
		setHeaderVisible(false)
	default:
		setHeaderVisible(true)
		maximizeWindow(window)
	}
}

func setUnmaximizedSize(window uintptr) {
	if window != 0 && gtkWindowSetDefaultSize != nil {
		gtkWindowSetDefaultSize(window, 700, 1120)
	}
}

func toggleWindowMaximize() {
	if webkitWindow == 0 {
		return
	}
	if windowIsMaximized() {
		setUnmaximizedSize(webkitWindow)
		if gtkWindowUnmaximize != nil {
			gtkWindowUnmaximize(webkitWindow)
		}
		return
	}
	maximizeWindow(webkitWindow)
}

func setHeaderVisible(on bool) {
	if webkitHeader == 0 || gtkWidgetSetVisible == nil {
		return
	}
	v := int32(0)
	if on {
		v = 1
	}
	gtkWidgetSetVisible(webkitHeader, v)
}

// toggleBigPicture hides the header bar and fullscreens the window, or shows
// the bar and leaves fullscreen. GTK restores the maximized or restored size
// from before fullscreen. The flag is what later page renders read.
func toggleBigPicture() {
	on := !server.BigPicture()
	server.SetBigPicture(on)
	if on {
		fullscreenWindow(webkitWindow)
		setHeaderVisible(false)
	} else {
		unfullscreenWindow()
		setHeaderVisible(true)
	}
	syncPageBigPicture()
}

func syncPageBigPicture() {
	if webkitView == 0 {
		return
	}
	state := "false"
	if server.BigPicture() {
		state = "true"
	}
	script := `(function(){var b=document.getElementById("big-picture");if(!b)return;var on=` + state + `;b.hidden=!on;b.setAttribute("aria-pressed",on?"true":"false");})();`
	if webkitWebViewRunJavaScript != nil {
		webkitWebViewRunJavaScript(webkitView, script, 0, 0, 0)
		return
	}
	if webkitWebViewEvaluateJavaScript != nil {
		webkitWebViewEvaluateJavaScript(webkitView, script, -1, 0, 0, 0, 0, 0)
	}
}

func mountWindow(window, bar, view uintptr) error {
	if gtkBoxNew == nil || gtkWidgetSetVexpand == nil {
		return fmt.Errorf("gtk box missing")
	}
	const vertical int32 = 1
	box := gtkBoxNew(vertical, 0)
	if box == 0 {
		return fmt.Errorf("gtk_box_new failed")
	}
	gtkWidgetSetVexpand(view, 1)
	if uiStack == stackGTK4 {
		if gtkBoxAppend == nil {
			return fmt.Errorf("gtk_box_append missing")
		}
		gtkBoxAppend(box, bar)
		gtkBoxAppend(box, view)
		gtk4WindowSetChild(window, box)
	} else {
		if gtkBoxPackStart == nil {
			return fmt.Errorf("gtk_box_pack_start missing")
		}
		gtkBoxPackStart(box, bar, 0, 0, 0)
		gtkBoxPackStart(box, view, 1, 1, 0)
		gtkContainerAdd(window, box)
	}
	if gtkWindowSetDecorated != nil {
		gtkWindowSetDecorated(window, 0)
	}
	webkitWindow = window
	webkitHeader = bar
	return nil
}

func windowButton(icon, tip string, clicked uintptr) (uintptr, error) {
	if clicked == 0 {
		return 0, fmt.Errorf("window button callback missing")
	}
	var btn uintptr
	switch uiStack {
	case stackGTK4:
		if gtk4ButtonNewFromIconName == nil {
			return 0, fmt.Errorf("gtk_button_new_from_icon_name missing")
		}
		btn = gtk4ButtonNewFromIconName(icon)
	default:
		if gtk3ButtonNewFromIconName == nil {
			return 0, fmt.Errorf("gtk_button_new_from_icon_name missing")
		}
		const gtkIconSizeButton int32 = 4
		btn = gtk3ButtonNewFromIconName(icon, gtkIconSizeButton)
	}
	if btn == 0 {
		return 0, fmt.Errorf("gtk_button_new_from_icon_name failed")
	}
	if gtkWidgetSetTooltipText != nil {
		gtkWidgetSetTooltipText(btn, tip)
	}
	gSignalConnectData(btn, "clicked", clicked, 0, 0, 0)
	return btn, nil
}

func packWindowButtons(bar uintptr) error {
	if gtkHeaderBarPackEnd == nil {
		return fmt.Errorf("gtk_header_bar_pack_end missing")
	}
	closeBtn, err := windowButton("window-close-symbolic", "Close", closeClickCB)
	if err != nil {
		return err
	}
	maxBtn, err := windowButton("window-maximize-symbolic", "Maximize", maximizeCB)
	if err != nil {
		return err
	}
	minBtn, err := windowButton("window-minimize-symbolic", "Minimize", minimizeCB)
	if err != nil {
		return err
	}
	// pack_end places the first widget at the trailing edge.
	gtkHeaderBarPackEnd(bar, closeBtn)
	gtkHeaderBarPackEnd(bar, maxBtn)
	gtkHeaderBarPackEnd(bar, minBtn)
	pictureBtn, err := windowButton("view-fullscreen-symbolic", "Big picture", bigPictureCB)
	if err != nil {
		return err
	}
	gtkHeaderBarPackEnd(bar, pictureBtn)
	return nil
}

func prepareWebView(view uintptr, rawURL string) {
	webkitView = view
	webkitAppURL = rawURL
	gSignalConnectData(view, "decide-policy", decideCB, 0, 0, 0)
}

// runWebKit opens rawURL and blocks until the window closes.
// afterLoad runs once the libraries are loaded, before the GTK loop, so a
// quit signal can reach the loop.
func runWebKit(rawURL, windowMode string, afterLoad func()) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := loadWebKit(); err != nil {
		return err
	}
	if uiStack == stackGTK4 {
		return runWebKitGTK4(rawURL, windowMode, afterLoad)
	}
	return runWebKitGTK3(rawURL, windowMode, afterLoad)
}

func runWebKitGTK3(rawURL, windowMode string, afterLoad func()) error {
	if gtkInitCheck(0, 0) == 0 {
		return fmt.Errorf("gtk_init_check failed")
	}
	if afterLoad != nil {
		afterLoad()
	}
	if quitReq.Load() {
		return nil
	}
	const gtkWindowToplevel int32 = 0
	window := gtkWindowNew(gtkWindowToplevel)
	if window == 0 {
		return fmt.Errorf("gtk_window_new failed")
	}
	gtkWindowSetTitle(window, "ServeMedia")
	bar, err := installDarkHeader()
	if err != nil {
		return err
	}
	setUnmaximizedSize(window)
	view := webkitWebViewNew()
	if view == 0 {
		return fmt.Errorf("webkit_web_view_new failed")
	}
	if err := mountWindow(window, bar, view); err != nil {
		return err
	}
	prepareWebView(view, rawURL)
	server.SetBigPicture(config.NormalizeWindowMode(windowMode) == config.WindowModeBigPicture)
	webkitWebViewLoadURI(view, rawURL)
	gSignalConnectData(window, "destroy", destroyCB, 0, 0, 0)
	gtkWidgetShowAll(window)
	applyStartupChrome(window, windowMode)
	if quitReq.Load() {
		return nil
	}
	gtkMain()
	return nil
}

func runWebKitGTK4(rawURL, windowMode string, afterLoad func()) error {
	if gtk4InitCheck() == 0 {
		return fmt.Errorf("gtk_init_check failed")
	}
	if afterLoad != nil {
		afterLoad()
	}
	if quitReq.Load() {
		return nil
	}
	window := gtk4WindowNew()
	if window == 0 {
		return fmt.Errorf("gtk_window_new failed")
	}
	gtkWindowSetTitle(window, "ServeMedia")
	bar, err := installDarkHeaderGTK4()
	if err != nil {
		return err
	}
	setUnmaximizedSize(window)
	view := webkitWebViewNew()
	if view == 0 {
		return fmt.Errorf("webkit_web_view_new failed")
	}
	if err := mountWindow(window, bar, view); err != nil {
		return err
	}
	prepareWebView(view, rawURL)
	server.SetBigPicture(config.NormalizeWindowMode(windowMode) == config.WindowModeBigPicture)
	webkitWebViewLoadURI(view, rawURL)
	gSignalConnectData(window, "close-request", closeCB, 0, 0, 0)
	gtk4WindowPresent(window)
	applyStartupChrome(window, windowMode)
	if quitReq.Load() {
		return nil
	}
	mainLoop = gMainLoopNew(0, 0)
	if mainLoop == 0 {
		return fmt.Errorf("g_main_loop_new failed")
	}
	gMainLoopRun(mainLoop)
	return nil
}

const gtkStyleProviderPriorityUser uint32 = 800

func darkHeaderCSS() string {
	return `headerbar {
  background-color: #0f1419;
  background-image: none;
  color: #ffffff;
  border: none;
  box-shadow: none;
}
headerbar .title,
headerbar .subtitle {
  color: #ffffff;
}
headerbar button,
headerbar .titlebutton {
  color: #ffffff;
  background: none;
  background-color: transparent;
  background-image: none;
  border: none;
  box-shadow: none;
  min-width: 0;
  min-height: 0;
  padding: 6px;
}
headerbar button image {
  color: #ffffff;
}
headerbar button:hover,
headerbar .titlebutton:hover {
  background-color: rgba(255, 255, 255, 0.08);
  color: #ffffff;
}
`
}

func darkHeaderCSSGTK4() string {
	return `headerbar {
  background-color: #0f1419;
  background-image: none;
  color: #ffffff;
  border: none;
  box-shadow: none;
}
headerbar .title,
headerbar .subtitle {
  color: #ffffff;
}
headerbar button,
headerbar .titlebutton,
headerbar windowcontrols,
headerbar windowcontrols button {
  color: #ffffff;
  background: none;
  background-color: transparent;
  background-image: none;
  border: none;
  box-shadow: none;
  min-width: 0;
  min-height: 0;
  padding: 6px;
}
headerbar button image {
  color: #ffffff;
}
headerbar button:hover,
headerbar .titlebutton:hover,
headerbar windowcontrols button:hover {
  background-color: rgba(255, 255, 255, 0.08);
  color: #ffffff;
}
`
}

func installDarkHeader() (uintptr, error) {
	bar := gtkHeaderBarNew()
	if bar == 0 {
		return 0, fmt.Errorf("gtk_header_bar_new failed")
	}
	gtkHeaderBarSetTitle(bar, "ServeMedia")
	gtkHeaderBarSetShowCloseButton(bar, 0)
	if err := packWindowButtons(bar); err != nil {
		return 0, err
	}
	screen := gdkScreenGetDefault()
	if screen == 0 {
		return 0, fmt.Errorf("gdk_screen_get_default failed")
	}
	provider := gtkCSSProviderNew()
	if provider == 0 {
		return 0, fmt.Errorf("gtk_css_provider_new failed")
	}
	css := darkHeaderCSS()
	if gtkCSSProviderLoadFromData(provider, css, len(css), 0) == 0 {
		return 0, fmt.Errorf("gtk_css_provider_load_from_data failed")
	}
	gtkStyleContextAddProviderForScreen(screen, provider, gtkStyleProviderPriorityUser)
	return bar, nil
}

func installDarkHeaderGTK4() (uintptr, error) {
	bar := gtkHeaderBarNew()
	if bar == 0 {
		return 0, fmt.Errorf("gtk_header_bar_new failed")
	}
	gtk4HeaderBarSetShowTitleButtons(bar, 0)
	if err := packWindowButtons(bar); err != nil {
		return 0, err
	}
	display := gdk4DisplayGetDefault()
	if display == 0 {
		return 0, fmt.Errorf("gdk_display_get_default failed")
	}
	provider := gtkCSSProviderNew()
	if provider == 0 {
		return 0, fmt.Errorf("gtk_css_provider_new failed")
	}
	css := darkHeaderCSSGTK4()
	switch {
	case gtk4CSSLoadFromData != nil:
		gtk4CSSLoadFromData(provider, css, len(css))
	case gtk4CSSLoadFromString != nil:
		gtk4CSSLoadFromString(provider, css)
	default:
		return 0, fmt.Errorf("gtk_css_provider_load_from_data failed")
	}
	gtk4StyleAddProviderForDisplay(display, provider, gtkStyleProviderPriorityUser)
	return bar, nil
}
