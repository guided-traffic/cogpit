# dmgbuild's settings for the template of Cogpit's disk image (hack/macos/dmg-template.sh, D-70):
# the window the owner chose in the DMG lab, read from packaging/dmg/lab.json (its "settings", in
# points, as the lab draws them): the window's size, the icons' size and centres, the label size.
# The background (packaging/dmg/background.png and @2x, joined into one TIFF) is the lab's drawing
# of the same settings, without the icons and labels, which Finder draws. The template holds no
# app: the signing job copies the signed Cogpit.app in with Apple's tools only (release.sh).
#
# dmgbuild -s hack/macos/dmg_settings.py -D lab=packaging/dmg/lab.json \
#     -D background=<background.tiff> -D volicon=<the app's .icns> "Cogpit <version>" <out.dmg>
import json

_lab = json.load(open(defines["lab"]))["settings"]  # noqa: F821 (dmgbuild's -D values)
_name = "Cogpit.app"  # placed by the signing job; its place and view settings are recorded here
_w, _h = _lab["W"], _lab["H"]
_y = round(_lab["iconY"] * _h)

format = "UDZO"  # dmgbuild 2 writes no writable image: the signing job converts it to UDRW, adds the app, back to UDZO
filesystem = "HFS+"
files = []
symlinks = {"Applications": "/Applications"}
icon = defines["volicon"]  # noqa: F821
background = defines["background"]  # noqa: F821

window_rect = ((200, 120), (_w, _h))
default_view = "icon-view"
show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
show_icon_preview = False
include_icon_view_settings = True
arrange_by = None
label_pos = "bottom"
text_size = _lab["textSize"]
icon_size = _lab["iconSize"]
icon_locations = {
    _name: (round(_lab["leftX"] * _w), _y),
    "Applications": (round(_lab["rightX"] * _w), _y),
}
