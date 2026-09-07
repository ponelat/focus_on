# NixOS module: installs FocusOn system-wide and, optionally, switches the
# GNOME Shell extension on for every user by seeding the dconf default.
#
# Enabling an extension is per-user state, so the system-level knob writes a
# dconf *default* rather than a user setting: users can still turn FocusOn
# off, and turning it off sticks.
{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.focuson;
  inherit (lib)
    mkEnableOption
    mkIf
    mkOption
    types
    ;
in
{
  options.programs.focuson = {
    enable = mkEnableOption "FocusOn time tracking and invoicing";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.focuson;
      defaultText = "focuson from the FocusOn flake";
      description = "The focuson CLI package.";
    };

    gnomeExtension = {
      enable = mkOption {
        type = types.bool;
        default = config.services.desktopManager.gnome.enable or false;
        defaultText = "config.services.desktopManager.gnome.enable";
        description = ''
          Install the GNOME Shell widget. Defaults to on wherever GNOME is,
          since the widget is the half of FocusOn that logs the time the CLI
          invoices.
        '';
      };

      package = mkOption {
        type = types.package;
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.gnome-shell-extension-focuson;
        defaultText = "gnome-shell-extension-focuson from the FocusOn flake";
        description = "The GNOME Shell extension package.";
      };

      enableByDefault = mkOption {
        type = types.bool;
        default = false;
        description = ''
          Turn the extension on by default for every user, via a dconf
          default. Off by default because installing an extension and
          enabling it are separate decisions — with this off, users switch
          FocusOn on themselves in the Extensions app.
        '';
      };
    };
  };

  config = mkIf cfg.enable {
    environment.systemPackages = [
      cfg.package
    ]
    ++ lib.optional cfg.gnomeExtension.enable cfg.gnomeExtension.package;

    programs.dconf = mkIf (cfg.gnomeExtension.enable && cfg.gnomeExtension.enableByDefault) {
      enable = true;
      profiles.user.databases = [
        {
          settings."org/gnome/shell" = {
            enabled-extensions = [ cfg.gnomeExtension.package.passthru.extensionUuid ];
          };
        }
      ];
    };
  };
}
