let
  pins = import ../../npins;

  nilla = import pins.nilla;
in
nilla.create (
  { config }:
  {
    includes = [
      ../../modules
    ];

    config = {
      inputs = {
        nixpkgs.src = builtins.fetchTarball {
          url = "https://releases.nixos.org/nixos/24.11/nixos-24.11.716868.60e405b241ed/nixexprs.tar.xz";
          sha256 = "111zrdbnd2b7w64q07773ksf4gfbm4gq7riggxld1gmxpimprj0j";
        };
        home-manager.src = builtins.fetchTarball {
          url = "https://github.com/nix-community/home-manager/archive/b4e98224ad1336751a2ac7493967a4c9f6d9cb3f.tar.gz";
          sha256 = "0qk1qn04willw5qrzfjs9b7815np8mr6ci68a2787g3q7444bdxp";
        };
      };

      generators.nixos.folder = ./hosts;

      generators.home = {
        folder = ./hosts;
        # Build `hosts/desktop/home/adam.nix` into `systems.nixos.desktop`
        # instead of creating `systems.home."adam@desktop"`.
        # Use `true` for all users, or "desktop" for every user on that host.
        nixos = [ "adam@desktop" ];
      };

      modules.home.common = {
        programs.git.enable = true;
      };
    };
  }
)
