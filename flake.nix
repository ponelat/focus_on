{
  description = "FocusOn — local-first time tracking and invoicing, for NixOS and GNOME";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (
        pkgs:
        let
          # The CLI builds everywhere; the GNOME widget is Linux-only, and on
          # macOS the widget you want is the Xcode project in FocusOn/.
          cli = pkgs.callPackage ./nix/focuson-cli.nix { };
          extension = pkgs.callPackage ./nix/gnome-extension.nix { };
        in
        {
          focuson = cli;
          gnome-shell-extension-focuson = extension;

          # Both halves of FocusOn for this platform, so that a plain
          #   nix profile install github:ckritzinger/focus_on
          # is the whole install rather than the first of two commands. The
          # extension lands in share/gnome-shell/extensions/<uuid>, which
          # GNOME finds through XDG_DATA_DIRS once the profile is on it.
          default = pkgs.symlinkJoin {
            name = "focuson-${cli.version}";
            paths = [ cli ] ++ pkgs.lib.optional pkgs.stdenv.hostPlatform.isLinux extension;
            meta = cli.meta // {
              description =
                "FocusOn: the focuson CLI"
                + pkgs.lib.optionalString pkgs.stdenv.hostPlatform.isLinux " and the GNOME Shell widget";
            };
          };
        }
      );

      overlays.default = final: _prev: {
        focuson = final.callPackage ./nix/focuson-cli.nix { };
        gnome-shell-extension-focuson = final.callPackage ./nix/gnome-extension.nix { };
      };

      nixosModules.default = import ./nix/nixos-module.nix { inherit self; };
      homeManagerModules.default = import ./nix/home-manager-module.nix { inherit self; };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          # go for the CLI; nodejs runs the extension's format tests and
          # eslint; glib provides glib-compile-schemas, which the extension
          # needs before GNOME will read its settings.
          packages = [
            pkgs.go_1_27
            pkgs.gopls
            pkgs.nodejs
            pkgs.glib
            pkgs.git
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
