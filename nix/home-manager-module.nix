{ tgragPackage, skillDirectory }:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.tgrag;
  package = tgragPackage pkgs;
  logFilePath = "${config.home.homeDirectory}/Library/Logs/tgrag-sync.log";
  skillFiles = builtins.listToAttrs (map (directory: {
    name = "${directory}/tgrag";
    value = { source = skillDirectory; recursive = true; };
  }) cfg.skill.directories);
in
{
  options.services.tgrag = {
    enable = lib.mkEnableOption "tgrag, local search over personal Telegram chats";

    autoSync = {
      enable = lib.mkEnableOption "periodic `tgrag sync` launchd agent (requires a completed `tgrag setup`)";
      intervalMinutes = lib.mkOption {
        type = lib.types.ints.positive;
        default = 30;
        description = "How often to run incremental sync.";
      };
    };

    skill = {
      enable = lib.mkEnableOption "installing the tgrag agent skill (SKILL.md) into every directory from skill.directories";
      directories = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ".claude/skills" ".codex/skills" ];
        description = "Skill directories of agents (relative to home), the skill is installed as <directory>/tgrag. Remove a directory when it is managed as a whole elsewhere.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ package ];

    home.file = lib.mkIf cfg.skill.enable skillFiles;

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
