let
  lib = {
    mkEnableOption = name: { type = "bool"; inherit name; };
    mkOption = attrs: attrs;
    mkIf = condition: value: if condition then value else {};
    types = { ints.positive = "ints.positive"; };
  };
  pkgs = {};
  config = {
    home.homeDirectory = "/Users/test";
    services.tgrag = {
      enable = true;
      autoSync = { enable = true; intervalMinutes = 15; };
      skill.enable = true;
    };
  };
  module = import ./home-manager-module.nix {
    tgragPackage = pkgs: "/test/tgrag";
    skillDirectory = "/test/skill";
  } { inherit config lib pkgs; };
  agent = module.config.launchd.agents.tgrag-sync.config;
in
assert module.config.home.packages == [ "/test/tgrag" ];
assert module.config.home.file.".claude/skills/telegram-search".source == "/test/skill";
assert agent.Label == "org.nix-community.home.tgrag-sync";
assert agent.ProgramArguments == [ "/test/tgrag/bin/tgrag" "sync" ];
assert agent.StartInterval == 900;
assert agent.RunAtLoad;
assert agent.StandardOutPath == "/Users/test/Library/Logs/tgrag-sync.log";
assert agent.StandardErrorPath == agent.StandardOutPath;
assert module.options.services.tgrag.autoSync.intervalMinutes.default == 30;
true
