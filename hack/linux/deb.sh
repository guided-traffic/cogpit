#!/usr/bin/env bash
# The Linux download of a release (D-108; docs/developer/releasing.md#the-release-pipeline): the
# Debian package cogpit_<version>_<arch>.deb, made from what `make dist-linux` built. The release
# pipeline's job "Cogpit.deb (Linux <arch>)" runs it through make; any Debian or Ubuntu machine with
# dpkg-dev runs the same.
#
#   hack/linux/deb.sh <version> <arch> <bin dir> <icon dir> <out dir>
#
# <bin dir> holds cogpit and cogpit-cli for <arch> (dpkg's name: amd64, arm64); <icon dir> holds the
# app icon as <size>.png for each size of the hicolor theme it fills. The package holds:
#
#   /usr/bin/cogpit, /usr/bin/cogpit-cli        the app and the command-line tool (D-16: the
#                                               complete product)
#   /usr/share/applications/…desktop            the launcher (packaging/linux), and the icons
#   /usr/share/metainfo/…metainfo.xml           the AppStream entry software centres show
#   /usr/libexec/cogpit/update                  the update helper the Update button runs through
#   /usr/share/polkit-1/actions/…policy         pkexec, and its polkit action (internal/update)
#   /etc/apt/sources.list.d/cogpit.sources      Cogpit's APT repository with its signing key inline
#                                               (Signed-By), a conffile: a user who deletes it keeps
#                                               it deleted through upgrades, a purge removes it
#
# Its dependencies are the shared libraries the app links (dpkg-shlibdeps) and the ones its window
# loads at run time, libdecor for its title bar included, and pkexec is recommended: without it Cogpit's Update button says so and apt or
# the Software Updater still update it. No maintainer scripts: dpkg's triggers refresh the desktop and icon caches. The
# Maintainer field is DEB_MAINTAINER (the Makefile's default).
set -euo pipefail
umask 022

die() { echo "deb.sh: $*" >&2; exit 1; }

[ "$#" -eq 5 ] || die "usage: $0 <version> <arch> <bin dir> <icon dir> <out dir>"
version=$1 arch=$2 bins=$3 icons=$4 out=$5
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "version $version: x.y.z expected"
case "$arch" in amd64 | arm64) ;; *) die "arch $arch: amd64 or arm64" ;; esac
: "${DEB_MAINTAINER:?DEB_MAINTAINER is not set}"
for b in cogpit cogpit-cli; do [ -x "$bins/$b" ] || die "no $bins/$b"; done
for t in dpkg-deb dpkg-shlibdeps gzip; do command -v "$t" >/dev/null || die "$t is missing (apt install dpkg-dev)"; done

here=$(cd "$(dirname "$0")" && pwd)
pkg=$(cd "$here/../../packaging/linux" && pwd)
id=io.guided-traffic.cogpit
repo_url=https://guided-traffic.github.io/cogpit/apt/
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
root=$work/root
mkdir -p "$out"

# The files, owned by root in the package (--root-owner-group), modes set here.
install -D -m 0755 "$bins/cogpit" "$root/usr/bin/cogpit"
install -D -m 0755 "$bins/cogpit-cli" "$root/usr/bin/cogpit-cli"
install -D -m 0644 "$pkg/$id.desktop" "$root/usr/share/applications/$id.desktop"
sed -e "s/@VERSION@/$version/g" -e "s/@DATE@/$(date -u +%Y-%m-%d)/g" "$pkg/$id.metainfo.xml" |
	install -D -m 0644 /dev/stdin "$root/usr/share/metainfo/$id.metainfo.xml"
install -D -m 0755 "$pkg/update" "$root/usr/libexec/cogpit/update"
install -D -m 0644 "$pkg/$id.update.policy" "$root/usr/share/polkit-1/actions/$id.update.policy"
n=0
for icon in "$icons"/*.png; do
	[ -e "$icon" ] || continue
	size=$(basename "$icon" .png)
	[[ "$size" =~ ^[0-9]+$ ]] || die "icon $icon: <size>.png expected"
	install -D -m 0644 "$icon" "$root/usr/share/icons/hicolor/${size}x${size}/apps/$id.png"
	n=$((n + 1))
done
[ "$n" -gt 0 ] || die "no icons in $icons"

# The repository's sources file: deb822, the public key inline (apt 2.4 and later read it there),
# its blank line written as " ." as deb822 continuation lines need.
{
	echo "# Cogpit's APT repository (https://github.com/guided-traffic/cogpit), set up by the package"
	echo "# cogpit so that apt and the Software Updater offer Cogpit's new releases. Delete this file"
	echo "# to stop that: an upgrade of the package does not bring it back; purging the package removes it."
	echo "Types: deb"
	echo "URIs: $repo_url"
	echo "Suites: stable"
	echo "Components: main"
	echo "Architectures: $arch"
	echo "Signed-By:"
	sed -e 's/^$/./' -e 's/^/ /' "$pkg/cogpit-archive-keyring.asc"
} | install -D -m 0644 /dev/stdin "$root/etc/apt/sources.list.d/cogpit.sources"

# The documentation Debian asks every package for: the copyright and a changelog.
doc=$root/usr/share/doc/cogpit
install -d -m 0755 "$doc"
cat >"$doc/copyright" <<'EOF'
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: Cogpit
Upstream-Contact: https://github.com/guided-traffic/cogpit/issues
Source: https://github.com/guided-traffic/cogpit

Files: *
Copyright: 2026 The Cogpit authors
License: Apache-2.0
 Licensed under the Apache License, Version 2.0. On Debian systems, the full text of the license
 is in /usr/share/common-licenses/Apache-2.0. The third-party code the binaries contain and its
 licences: https://github.com/guided-traffic/cogpit/blob/main/ATTRIBUTION.md and the modules
 go.mod names.
EOF
chmod 0644 "$doc/copyright"
{
	echo "cogpit ($version) stable; urgency=medium"
	echo
	echo "  * Cogpit $version: https://github.com/guided-traffic/cogpit/releases/tag/v$version"
	echo
	echo " -- $DEB_MAINTAINER  $(date -u -R)"
} | gzip -9n >"$doc/changelog.gz"
chmod 0644 "$doc/changelog.gz"

# lintian's accepted findings, each with its reason.
install -D -m 0644 /dev/stdin "$root/usr/share/lintian/overrides/cogpit" <<'EOF'
# Go binaries: neither has a manual page, both have --help.
cogpit: no-manual-page [usr/bin/cogpit]
cogpit: no-manual-page [usr/bin/cogpit-cli]
# cogpit-cli is pure Go built position-independent: it needs the dynamic loader and no library.
cogpit: shared-library-lacks-prerequisites [usr/bin/cogpit-cli]
# The repository's sources file is the package's purpose for updates (D-108), a conffile the user may delete.
cogpit: package-installs-apt-sources [etc/apt/sources.list.d/cogpit.sources]
EOF
# Not overridden, since an older lintian (Ubuntu 24.04's, CI's) calls an override of a tag it does
# not know an error: newer lintian warns appstream-metadata-validation-failed, because appstreamcli
# warns of the hyphen in io.guided-traffic.cogpit; it is the app's id everywhere (its Wayland
# app_id, the macOS bundle's), and the desktop file and the AppStream id must match it.

# The shared libraries the app links, as dpkg-shlibdeps names their packages; it wants a source
# package's debian/control around it.
mkdir -p "$work/src/debian"
printf 'Source: cogpit\n\nPackage: cogpit\nArchitecture: any\n' >"$work/src/debian/control"
depends=$(cd "$work/src" && dpkg-shlibdeps -O -e"$root/usr/bin/cogpit" 2>/dev/null | sed -n 's/^shlibs:Depends=//p')
[ -n "$depends" ] || die "dpkg-shlibdeps found no dependencies of cogpit"
# GLFW (Fyne's window) loads the X11 and Wayland client libraries at run time (dlopen), which
# dpkg-shlibdeps cannot see: the ones it needs for a window on either, by their packages' names
# on Debian 13 and Ubuntu 24.04 and later. libdecor and a plugin of it draw the window's title bar
# with its minimize, maximize and close buttons where the compositor draws none and there is no
# XWayland (GNOME without it; with it, Fyne takes X11 and GNOME draws them): required, so that the
# window always has its buttons, also when the package is installed without its recommendations.
depends="$depends, libegl1, libwayland-cursor0, libwayland-egl1, libxkbcommon0, libx11-6, libx11-xcb1"
depends="$depends, libxcursor1, libxext6, libxi6, libxinerama1, libxrandr2, libxrender1, libxxf86vm1"
depends="$depends, libdecor-0-0, libdecor-0-plugin-1-gtk | libdecor-0-plugin-1"

mkdir -p "$root/DEBIAN"
echo /etc/apt/sources.list.d/cogpit.sources >"$root/DEBIAN/conffiles"
cat >"$root/DEBIAN/control" <<EOF
Package: cogpit
Version: $version
Architecture: $arch
Maintainer: $DEB_MAINTAINER
Installed-Size: $(du -sk --exclude=DEBIAN "$root" | cut -f1)
Depends: $depends
Recommends: pkexec
Section: devel
Priority: optional
Homepage: https://github.com/guided-traffic/cogpit
Description: Native Kubernetes IDE
 Cogpit shows your Kubernetes clusters from a live cache, asks before it
 changes anything, and lets agents drive it over MCP. It browses every
 resource of the clusters in your kubeconfigs, shows logs and metrics,
 opens shells in pods and nodes, and manages Helm releases.
 .
 The package contains the desktop app (cogpit) and the command-line tool
 (cogpit-cli), and sets up Cogpit's APT repository for updates.
EOF

deb=$out/cogpit_${version}_${arch}.deb
dpkg-deb --root-owner-group -Zxz --build "$root" "$deb" >/dev/null
echo "$deb: $(du -h "$deb" | cut -f1)"
dpkg-deb --field "$deb" Package Version Architecture Depends
