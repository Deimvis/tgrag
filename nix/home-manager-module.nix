{ tgragPackage, skillDirectory }:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.tgrag;
  package = tgragPackage pkgs;
  logFilePath = "${config.home.homeDirectory}/Library/Logs/tgrag-sync.log";
in
{
  options.services.tgrag = {
    enable = lib.mkEnableOption "tgrag, local search over personal Telegram chats";

    autoSync = {
      enable = lib.mkEnableOption "periodic `tgrag sync` launchd agent (requires a completed `tgrag login`)";
      intervalMinutes = lib.mkOption {
        type = lib.types.ints.positive;
        default = 30;
        description = "How often to run incremental sync.";
      };
    };

    skill.enable = lib.mkEnableOption "installing the telegram-search Claude Code skill into ~/.claude/skills (do not enable when ~/.claude/skills is managed as a whole directory elsewhere; copy ${skillDirectory} there instead)";
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ package ];

    home.file.".claude/skills/telegram-search" = lib.mkIf cfg.skill.enable {
      source = skillDirectory;
      recursive = true;
    };

    launchd.agents.tgrag-sync = lib.mkIf cfg.autoSync.enable {
      enable = true;
      config = {
        Label = "org.nix-community.home.tgrag-sync";
        ProgramArguments = [ "${package}/bin/tgrag" "sync" ];
        StartInterval = cfg.autoSync.intervalMinutes * 60;
        RunAtLoad = true;
        ProcessType = "Background";
        StandardOutPath = logFilePath;
        StandardErrorPath = logFilePath;
      };
    };
  };
}
