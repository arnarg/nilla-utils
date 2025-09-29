let
  pins = import ./npins;

  nilla = import pins.nilla;

  systems = [
    "x86_64-linux"
    "aarch64-linux"
  ];
in
nilla.create (
  { config, ... }:
  {
    config = {
      inputs = {
        nixpkgs = {
          src = pins.nixpkgs;

          settings.overlays = [
            (
              final: prev:
              let
                callPackage = final.callPackage;
              in
              {
                inherit (callPackage "${pins.gomod2nix}/builder" { }) buildGoApplication mkGoEnv mkVendorEnv;
                gomod2nix = callPackage "${pins.gomod2nix}/default.nix" { };
              }
            )
          ];
        };
      };

      packages.default = config.packages.nilla-utils-plugins;
      packages.nilla-utils-plugins = {
        inherit systems;

        builder = "nixpkgs";
        settings.pkgs = config.inputs.nixpkgs.result;

        package = import ./package.nix;
      };

      shells.default = {
        inherit systems;

        builder = "nixpkgs";
        settings.pkgs = config.inputs.nixpkgs.result;

        shell =
          {
            mkShellNoCC,
            npins,
            gomod2nix,
            nvd,
            ...
          }:
          mkShellNoCC {
            packages = [
              npins
              gomod2nix
              nvd
            ];
          };
      };
    };
  }
)
