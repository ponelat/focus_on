#!/usr/bin/env bash
# Installs FocusOn on a GNOME desktop: the Shell extension (the port of
# FocusOn.app) plus the focuson CLI and its daily sync timer.
#
# The counterpart to install.sh, which is macOS-only — it drives xcodebuild.
# NixOS users want neither: see flake.nix and gnome/README.md.
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
UUID="focuson@ckritzinger.github.io"
EXT_SRC="$PROJECT_DIR/gnome/$UUID"
EXT_DEST="${XDG_DATA_HOME:-$HOME/.local/share}/gnome-shell/extensions/$UUID"
BIN_INSTALL_DIR="$HOME/.local/bin"
CLI_BIN="$BIN_INSTALL_DIR/focuson"
CRON_TIME="00:00"

if [ -e /etc/NIXOS ]; then
  echo "This looks like NixOS, where copying files into place is the wrong move." >&2
  echo "Use the flake instead — see gnome/README.md." >&2
  exit 1
fi

# --- Extension -------------------------------------------------------------

if ! command -v glib-compile-schemas > /dev/null; then
  echo "Error: glib-compile-schemas not found. Install glib's development tools" >&2
  echo "(glib2-devel, libglib2.0-dev, or similar) and run this again." >&2
  exit 1
fi

echo "Installing the GNOME Shell extension to $EXT_DEST..."
rm -rf "$EXT_DEST"
mkdir -p "$EXT_DEST"
cp -R "$EXT_SRC/." "$EXT_DEST/"

# Without this the extension loads but every setting reads as its default,
# including the widget position and whatever task was being tracked.
glib-compile-schemas --strict "$EXT_DEST/schemas"

if command -v gnome-extensions > /dev/null; then
  # Enabling before the shell has seen the files is fine — GNOME records the
  # uuid and picks it up on the next load.
  gnome-extensions enable "$UUID" || \
    echo "Note: couldn't enable it automatically — turn FocusOn on in the Extensions app."
else
  echo "Note: gnome-extensions not found — turn FocusOn on in the Extensions app."
fi

# --- CLI -------------------------------------------------------------------

if ! command -v go > /dev/null; then
  echo "Warning: go not found — skipping the focuson CLI and its sync timer." >&2
  echo "Done — extension installed. Log out and back in to load it." >&2
  exit 0
fi

echo "Building the focuson CLI..."
mkdir -p "$BIN_INSTALL_DIR"
( cd "$PROJECT_DIR/cli" && go build -o "$CLI_BIN" . )
echo "Installed focuson to $CLI_BIN"

case ":$PATH:" in
  *":$BIN_INSTALL_DIR:"*) ;;
  *) echo "Note: $BIN_INSTALL_DIR isn't on your PATH — add it to your shell profile to run 'focuson' directly." ;;
esac

# --- Daily sync ------------------------------------------------------------

echo "Installing the daily sync timer (runs at $CRON_TIME)..."
"$CLI_BIN" cron install --time "$CRON_TIME"

# --- Finish ----------------------------------------------------------------

echo
echo "Done."
if [ "${XDG_SESSION_TYPE:-}" = "wayland" ]; then
  # Under Wayland the shell cannot be restarted in place, so there is no
  # equivalent of Alt+F2 r and the extension genuinely cannot load until the
  # session does.
  echo "You're on Wayland: log out and back in to load the widget."
else
  echo "Restart GNOME Shell (Alt+F2, type 'r', Enter) to load the widget."
fi
