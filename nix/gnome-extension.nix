# The GNOME Shell extension — this repo's port of FocusOn.app.
#
# Packaged the way nixpkgs packages every other Shell extension: the whole
# extension directory under share/gnome-shell/extensions/<uuid>, with its
# GSettings schema compiled in place. `passthru.extensionUuid` is the piece
# the NixOS and Home Manager modules read to switch the extension on.
{
  lib,
  stdenvNoCC,
  glib,
  nodejs,
}:
let
  uuid = "focuson@ckritzinger.github.io";
in
stdenvNoCC.mkDerivation {
  pname = "gnome-shell-extension-focuson";
  version = "1.0.0";

  # The whole gnome/ directory, not just the extension subdirectory: Nix
  # path syntax has no room for the "@" every extension uuid contains, so
  # the subdirectory is picked out in installPhase instead. It also puts the
  # test suite next door, where checkPhase can reach it.
  src = ../gnome;

  nativeBuildInputs = [
    glib
    nodejs
  ];

  dontConfigure = true;
  dontBuild = true;

  # lib/format.js holds everything the extension and the Go CLI have to agree
  # on — the CSV row format and the RFC 3339 timestamps. It is free of gi://
  # imports precisely so it can be checked here, without a GNOME session.
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    node --test tests/format.test.js
    runHook postCheck
  '';

  installPhase = ''
    runHook preInstall

    install -d "$out/share/gnome-shell/extensions/${uuid}"
    cp -r "${uuid}"/. "$out/share/gnome-shell/extensions/${uuid}/"

    glib-compile-schemas --strict \
      "$out/share/gnome-shell/extensions/${uuid}/schemas"

    runHook postInstall
  '';

  passthru.extensionUuid = uuid;

  meta = {
    description = "Floating task widget for GNOME that logs work sessions for the focuson CLI";
    longDescription = ''
      The GNOME port of FocusOn.app. A draggable widget that floats above
      every window, including fullscreen ones, and stays put across
      workspaces, plus a top-bar indicator for completing, pausing and
      switching tasks. Sessions are appended to the same CSV files the
      focuson CLI invoices from.

      This is a Shell extension rather than a GTK application because
      nothing else on GNOME can be an always-on-top, user-positioned
      window: Wayland does not let a client place or raise its own surface,
      and Mutter does not implement wlr-layer-shell.
    '';
    homepage = "https://github.com/ckritzinger/focus_on";
    license = lib.licenses.mit;
    platforms = lib.platforms.linux;
  };
}
