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
      packages = forAllSystems (pkgs: {
        # The CLI builds everywhere; the GNOME widget is Linux-only, and on
        # macOS the widget you want is the Xcode project in FocusOn/.
        focuson = pkgs.callPackage ./nix/focuson-cli.nix { };
        gnome-shell-extension-focuson = pkgs.callPackage ./nix/gnome-extension.nix { };
        default = pkgs.callPackage ./nix/focuson-cli.nix { };
      });

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
