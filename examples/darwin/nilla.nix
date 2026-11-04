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
      # nix-darwin's branch must match the Nixpkgs release:
      # `master` with `nixpkgs-unstable`, `nix-darwin-YY.MM` with `nixpkgs-YY.MM-darwin`.
      inputs.nixpkgs.src = pins.nixpkgs;
      inputs.nix-darwin.src = builtins.fetchTarball {
        url = "https://github.com/nix-darwin/nix-darwin/archive/ff0e21a414d4547183396da1b44a66f2577b65e9.tar.gz";
        sha256 = "1d5jqlj3rl8wfh44gr3pdxdd09myjga912wqz6csmx31qyam66pr";
      };

      systems.darwin.mymac = {
        system = "aarch64-darwin";

        modules = [
          (
            { darwinModules, pkgs, ... }:
            {
              imports = [ darwinModules.common ];

              environment.systemPackages = [ pkgs.git ];
              system.stateVersion = 6;
            }
          )
        ];
      };

      modules.darwin.common = {
        system.primaryUser = "myuser";
        users.users.myuser.home = "/Users/myuser";
      };
    };
  }
)
