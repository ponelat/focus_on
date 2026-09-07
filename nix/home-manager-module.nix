# Home Manager module: the per-user half, which is where FocusOn actually
# belongs — one person's tasks, one person's data directory, one person's
# backup schedule.
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
  uuid = cfg.gnomeExtension.package.passthru.extensionUuid;
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

    dataDir = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "/home/you/focuson-data";
      description = ''
        Where logged time and invoices live. Written to the config.toml that
        the CLI and the GNOME widget share, so setting it here settles it for
        both.

        Left null, whichever side you run first asks you — the same first-run
        flow as on macOS.
      '';
    };

    gnomeExtension = {
      enable = mkOption {
        type = types.bool;
        default = pkgs.stdenv.hostPlatform.isLinux;
        defaultText = "true on Linux";
        description = "Install and enable the GNOME Shell widget.";
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
          Also switch the extension on, by writing GNOME's
          enabled-extensions list.

          Off by default because that list is a single dconf value: setting
          it here makes this module the sole authority on which extensions
          you run, and anything you enabled in the Extensions app gets
          turned off on the next rebuild. Leave it off and enable FocusOn
          once, by hand, in the Extensions app — or, if you already declare
          your extensions in Home Manager, add
          "focuson@ckritzinger.github.io" to that list yourself.
        '';
      };
    };

    sync = {
      enable = mkEnableOption ''
        a daily systemd user timer that commits your data directory to git and
        pushes it. The declarative equivalent of `focuson cron install` — use
        one or the other, not both
      '';

      time = mkOption {
        type = types.str;
        default = "00:00";
        example = "18:00";
        description = "Local 24-hour time to run the daily sync.";
      };
    };
  };

  config = mkIf cfg.enable {
    home.packages = [
      cfg.package
    ]
    ++ lib.optional cfg.gnomeExtension.enable cfg.gnomeExtension.package;

    # The one setting the CLI and the widget genuinely share. Written as a
    # managed file so a rebuild puts it back if something else moved it.
    xdg.configFile."focuson/config.toml" = mkIf (cfg.dataDir != null) {
      text = ''
        data_dir = "${cfg.dataDir}"
      '';
    };

    dconf.settings = mkIf (cfg.gnomeExtension.enable && cfg.gnomeExtension.enableByDefault) {
      "org/gnome/shell".enabled-extensions = [ uuid ];
    };

    # Deliberately mirrors what `focuson cron install` writes, including
    # Persistent=true: a laptop that was closed at midnight still syncs when
    # it comes back, which is the whole point of scheduling a backup.
    systemd.user.services.focuson-sync = mkIf cfg.sync.enable {
      Unit = {
        Description = "FocusOn daily sync";
        Documentation = "https://github.com/ckritzinger/focus_on";
      };
      Service = {
        Type = "oneshot";
        ExecStart = "${lib.getExe cfg.package} sync";
      };
    };

    systemd.user.timers.focuson-sync = mkIf cfg.sync.enable {
      Unit.Description = "FocusOn daily sync";
      Timer = {
        OnCalendar = "*-*-* ${cfg.sync.time}:00";
        Persistent = true;
      };
      Install.WantedBy = [ "timers.target" ];
    };
  };
}
