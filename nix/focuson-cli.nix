# The `focuson` CLI. Unchanged Go, but see internal/cronsetup/cronsetup_linux.go:
# `focuson cron install` deliberately writes the *profile* path into the
# systemd unit rather than the /nix/store path it is running from, so that a
# `nix profile upgrade` doesn't leave the daily backup pointing at a store
# path the next garbage collection deletes.
{
  lib,
  buildGoModule,
  go_1_27,
  git,
}:
let
  # cli/go.mod requires Go 1.27. nixpkgs' default `go` still lags that on
  # several channels, and inside the build sandbox there is no network for
  # GOTOOLCHAIN=auto to fetch one, so the version is pinned explicitly rather
  # than left to fail with Go's "go.mod requires go >= 1.27.0".
  goModule = buildGoModule.override { go = go_1_27; };
in
goModule (finalAttrs: {
  pname = "focuson";
  version = "1.0.0";

  # The Go module is cli/, not the repo root — the repo also holds a macOS
  # Xcode project and the GNOME extension.
  src = ../cli;

  vendorHash = "sha256-mpYlZvwE8lDp7bEq2f5riY0VbHEm7mg5n5X3IaDU/8s=";

  ldflags = [
    "-s"
    "-w"
  ];

  # The suite covers CSV integrity, invoice numbering and PDF rendering, all
  # of which are pure and hermetic. gitsync's tests create throwaway repos in
  # $TMPDIR and need an identity to commit with.
  preCheck = ''
    export HOME=$TMPDIR
    git config --global user.email "focuson@example.invalid"
    git config --global user.name "focuson build"
    git config --global init.defaultBranch main
  '';

  # internal/gitsync shells out to git — the tests build real repositories
  # rather than mocking the one thing that has to actually work.
  nativeCheckInputs = [ git ];

  # The main package sits in cli/, so the Go toolchain names the binary
  # "cli". install.sh has always built it as `focuson`, and that is the name
  # the LaunchAgent, the systemd unit and every README line use.
  postInstall = ''
    mv "$out/bin/cli" "$out/bin/focuson"
  '';

  meta = {
    description = "Local-first personal time tracking and invoicing";
    longDescription = ''
      Turns time logged by the FocusOn widget into invoices: clients,
      projects, rates, PDF generation and git-backed durability. No accounts,
      no cloud — everything lives in plain CSV and TOML files in a git repo
      you control.
    '';
    homepage = "https://github.com/ckritzinger/focus_on";
    license = lib.licenses.mit;
    mainProgram = "focuson";
    platforms = lib.platforms.unix;
  };
})
