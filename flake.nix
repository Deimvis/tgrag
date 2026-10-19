{
  description = "Local search over personal Telegram chats, usable as CLI and as Claude Code skill";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      mkPackage = pkgs: pkgs.buildGoModule {
        pname = "tgrag";
        version = "0.1.0";
        src = ./.;
        vendorHash = "sha256-NyQJ1qUAE3rN77l4FbDKK+b4rqPtdZnv4XDgYvPHDOA=";
        subPackages = [ "cmd/tgrag" ];
        env.CGO_ENABLED = 0;
        meta.mainProgram = "tgrag";
      };
    in
    {
      packages = forAllSystems (pkgs: rec {
        tgrag = mkPackage pkgs;
        default = tgrag;
      });

      overlays.default = final: prev: {
        tgrag = mkPackage final;
      };

      homeManagerModules.default = import ./nix/home-manager-module.nix { tgragPackage = pkgs: mkPackage pkgs; skillDirectory = ./skill/tgrag; };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.gotestsum ];
        };
      });

      checks = forAllSystems (pkgs: {
        unit = (mkPackage pkgs).overrideAttrs (old: {
          doCheck = true;
          checkFlags = [ "-timeout" "120s" ];
        });
      });
    };
}
